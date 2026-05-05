package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/visionloop/api/internal/application"
	"github.com/visionloop/api/internal/domain"
	"github.com/visionloop/api/internal/infrastructure/eventbus"
	httpapi "github.com/visionloop/api/internal/interfaces/http"
)

// In-memory implementations of the application ports — sufficient for
// handler-level tests that exercise routing, auth, and serialisation
// without a database.

type stubSessions struct{ userID uuid.UUID }

func (s stubSessions) LookupSession(_ context.Context, token string) (uuid.UUID, time.Time, error) {
	if token == "valid-token" {
		return s.userID, time.Now().Add(time.Hour), nil
	}
	return uuid.Nil, time.Time{}, domain.ErrUnauthorized
}

type stubUsers struct {
	user domain.User
	mem  domain.Membership
}

func (s stubUsers) GetUser(context.Context, uuid.UUID) (domain.User, error) {
	return s.user, nil
}
func (s stubUsers) PrimaryMembership(context.Context, uuid.UUID) (domain.Membership, error) {
	return s.mem, nil
}

type stubImages struct {
	created domain.Image
}

func (s *stubImages) Create(_ context.Context, img domain.Image) (domain.Image, error) {
	img.CreatedAt = time.Now()
	s.created = img
	return img, nil
}
func (s *stubImages) Finalize(context.Context, uuid.UUID, uuid.UUID, string, int32, int32, string) (domain.Image, error) {
	return domain.Image{}, nil
}
func (s *stubImages) Get(context.Context, uuid.UUID, uuid.UUID) (domain.Image, error) {
	return domain.Image{}, domain.ErrNotFound
}
func (s *stubImages) List(context.Context, uuid.UUID, domain.ImageStatus, int32, int32) ([]domain.Image, int64, error) {
	return nil, 0, nil
}

type stubStorage struct{}

func (stubStorage) PresignPut(_ context.Context, key, ct string, sz int64, ttl time.Duration) (application.PresignedURL, error) {
	return application.PresignedURL{
		URL:     "https://r2.example/" + key,
		Method:  "PUT",
		Headers: map[string]string{"Content-Type": ct},
		Expires: time.Now().Add(ttl),
	}, nil
}
func (stubStorage) HeadObject(context.Context, string) (application.ObjectInfo, error) {
	return application.ObjectInfo{}, domain.ErrNotFound
}

type stubAudit struct{}

func (stubAudit) Record(context.Context, application.AuditEntry) error { return nil }

// stubJobs is a tiny in-memory JobRepository used by handlers_jobs_test.go.
type stubJobs struct {
	created       []domain.Job
	failNextWith  error
	dedupHit      *domain.Job // if set, FindActiveByDedupKey returns this regardless of args
	markErroredOK bool
}

func (s *stubJobs) Create(_ context.Context, j domain.Job) (domain.Job, error) {
	if s.failNextWith != nil {
		err := s.failNextWith
		s.failNextWith = nil
		return domain.Job{}, err
	}
	j.State = domain.JobPending
	s.created = append(s.created, j)
	return j, nil
}
func (s *stubJobs) ApplyCallback(_ context.Context, in application.JobCallback) (domain.Job, error) {
	for i, j := range s.created {
		if j.ID != in.ID {
			continue
		}
		// Mirror the SQL guard: refuse to overwrite a terminal state.
		switch j.State {
		case domain.JobSucceeded, domain.JobFailed, domain.JobCancelled:
			return j, domain.ErrConflict
		}
		s.created[i].State = in.State
		s.created[i].Attempt = in.Attempt
		s.created[i].Error = in.Error
		return s.created[i], nil
	}
	return domain.Job{}, domain.ErrNotFound
}
func (s *stubJobs) Get(_ context.Context, id, orgID uuid.UUID) (domain.Job, error) {
	for _, j := range s.created {
		if j.ID == id && j.OrgID == orgID {
			return j, nil
		}
	}
	return domain.Job{}, domain.ErrNotFound
}
func (s *stubJobs) FindActiveByDedupKey(_ context.Context, _ uuid.UUID, _ string, _ time.Time) (domain.Job, error) {
	if s.dedupHit != nil {
		return *s.dedupHit, nil
	}
	return domain.Job{}, domain.ErrNotFound
}
func (s *stubJobs) MarkErrored(_ context.Context, _, _ uuid.UUID, _ string) error {
	s.markErroredOK = true
	return nil
}

// stubAnnotations is an in-memory AnnotationRepository + AnnotationSetRepository for handler tests.
type stubAnnotations struct {
	row     domain.Annotation
	version int64
	notFound bool
	// Used by GetByImage:
	set      domain.AnnotationSet
	setItems []domain.Annotation
	setFound bool
}

func (s *stubAnnotations) GetByImage(_ context.Context, imageID, orgID uuid.UUID) (domain.AnnotationSet, []domain.Annotation, error) {
	if !s.setFound || s.set.ImageID != imageID || s.set.OrgID != orgID {
		return domain.AnnotationSet{}, nil, domain.ErrNotFound
	}
	return s.set, s.setItems, nil
}

func (s *stubAnnotations) Patch(_ context.Context, id, orgID uuid.UUID, ifMatch int64, patch domain.AnnotationPatch) (domain.Annotation, int64, error) {
	if s.notFound {
		return domain.Annotation{}, 0, domain.ErrNotFound
	}
	if id != s.row.ID || orgID != s.row.OrgID {
		return domain.Annotation{}, 0, domain.ErrNotFound
	}
	if ifMatch != s.version {
		return domain.Annotation{}, s.version, domain.ErrConflict
	}
	if patch.Geometry != nil {
		s.row.Geometry = patch.Geometry
	}
	if patch.LabelID != nil {
		l := *patch.LabelID
		s.row.LabelID = &l
	}
	if patch.HumanAccepted != nil {
		v := *patch.HumanAccepted
		s.row.HumanAccepted = &v
	}
	s.version++
	return s.row, s.version, nil
}

// stubPublisher records publishes; failWith makes Publish return an error.
type stubPublisher struct {
	calls   int
	failWith error
}

func (p *stubPublisher) Publish(_ context.Context, _, _ string, _ []byte, _ map[string]any) error {
	p.calls++
	return p.failWith
}

func newTestRouter(t *testing.T, role domain.Role) http.Handler {
	t.Helper()
	uid := uuid.MustParse("00000000-0000-0000-0000-000000000010")
	oid := uuid.MustParse("00000000-0000-0000-0000-000000000020")

	deps := httpapi.RouterDeps{
		Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Pool:   nil, // health endpoints not exercised here
		Sessions: stubSessions{userID: uid},
		Users: stubUsers{
			user: domain.User{ID: uid, Email: "owner@example.com", DisplayName: "Owner", Locale: "en"},
			mem:  domain.Membership{OrgID: oid, UserID: uid, Role: role},
		},
		ImagesRepo:            &stubImages{},
		JobsRepo:              &stubJobs{},
		JobPublisher:          &stubPublisher{},
		JobEvents:             eventbus.NewMemoryHub(),
		AnnotationsRepo:       &stubAnnotations{notFound: true},
		AnnotationSetsRepo:    &stubAnnotations{notFound: true},
		Storage:               stubStorage{},
		Audit:                 stubAudit{},
		WebOrigin:             "http://localhost:3000",
		PresignTTL:            15 * time.Minute,
		StartedAt:             time.Now(),
		JobCallbackToken:      testCallbackToken,
		JobCallbackHMACSecret: testCallbackHMAC,
	}
	return httpapi.NewRouter(deps)
}

const (
	testCallbackToken = "test-callback-token-of-at-least-32-bytes-long-xx"
	testCallbackHMAC  = "test-callback-hmac-secret-of-at-least-32-bytes-x"
)

// newTestRouterWithAnnotations builds a router with the given annotation
// stub. Other deps default. Used by handlers_annotations_test.go.
func newTestRouterWithAnnotations(t *testing.T, role domain.Role, anns *stubAnnotations) http.Handler {
	t.Helper()
	uid := uuid.MustParse("00000000-0000-0000-0000-000000000010")
	oid := uuid.MustParse("00000000-0000-0000-0000-000000000020")
	deps := httpapi.RouterDeps{
		Logger:   slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Sessions: stubSessions{userID: uid},
		Users: stubUsers{
			user: domain.User{ID: uid, Email: "x@example.com", DisplayName: "X", Locale: "en"},
			mem:  domain.Membership{OrgID: oid, UserID: uid, Role: role},
		},
		ImagesRepo:            &stubImages{},
		JobsRepo:              &stubJobs{},
		JobPublisher:          &stubPublisher{},
		JobEvents:             eventbus.NewMemoryHub(),
		AnnotationsRepo:       anns,
		AnnotationSetsRepo:    anns,
		Storage:               stubStorage{},
		Audit:                 stubAudit{},
		WebOrigin:             "http://localhost:3000",
		PresignTTL:            15 * time.Minute,
		StartedAt:             time.Now(),
		JobCallbackToken:      testCallbackToken,
		JobCallbackHMACSecret: testCallbackHMAC,
	}
	return httpapi.NewRouter(deps)
}

// newTestRouterWith lets jobs tests inject specific stubs (image fixture,
// publisher behaviour, dedup hit). Returns the deps so tests can assert
// on the stubs after calling the handler. A fresh in-memory hub is used.
func newTestRouterWith(t *testing.T, role domain.Role, images application.ImageRepository, jobs application.JobRepository, pub application.JobPublisher) http.Handler {
	t.Helper()
	return newTestRouterWithHub(t, role, images, jobs, pub, eventbus.NewMemoryHub())
}

func newTestRouterWithHub(t *testing.T, role domain.Role, images application.ImageRepository, jobs application.JobRepository, pub application.JobPublisher, hub application.JobEventHub) http.Handler {
	t.Helper()
	uid := uuid.MustParse("00000000-0000-0000-0000-000000000010")
	oid := uuid.MustParse("00000000-0000-0000-0000-000000000020")
	deps := httpapi.RouterDeps{
		Logger:   slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Sessions: stubSessions{userID: uid},
		Users: stubUsers{
			user: domain.User{ID: uid, Email: "x@example.com", DisplayName: "X", Locale: "en"},
			mem:  domain.Membership{OrgID: oid, UserID: uid, Role: role},
		},
		ImagesRepo:            images,
		JobsRepo:              jobs,
		JobPublisher:          pub,
		JobEvents:             hub,
		Storage:               stubStorage{},
		Audit:                 stubAudit{},
		WebOrigin:             "http://localhost:3000",
		PresignTTL:            15 * time.Minute,
		StartedAt:             time.Now(),
		JobCallbackToken:      testCallbackToken,
		JobCallbackHMACSecret: testCallbackHMAC,
	}
	return httpapi.NewRouter(deps)
}

func TestPresign_RejectsAnonymous(t *testing.T) {
	t.Parallel()
	router := newTestRouter(t, domain.RoleAnnotator)
	body, _ := json.Marshal(map[string]any{"content_type": "image/png", "byte_size": 1024})

	req := httptest.NewRequest(http.MethodPost, "/v1/images:presign", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestPresign_AnnotatorAllowed(t *testing.T) {
	t.Parallel()
	router := newTestRouter(t, domain.RoleAnnotator)
	body, _ := json.Marshal(map[string]any{"content_type": "image/png", "byte_size": 1024})

	req := httptest.NewRequest(http.MethodPost, "/v1/images:presign", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "better-auth.session_token", Value: "valid-token"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Image  map[string]any `json:"image"`
		Upload map[string]any `json:"upload"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Upload["url"] == "" {
		t.Fatalf("expected presigned URL, got %v", resp.Upload)
	}
	if resp.Image["status"] != "uploading" {
		t.Fatalf("expected status uploading, got %v", resp.Image["status"])
	}
}

func TestPresign_ViewerForbidden(t *testing.T) {
	t.Parallel()
	router := newTestRouter(t, domain.RoleViewer)
	body, _ := json.Marshal(map[string]any{"content_type": "image/png", "byte_size": 1024})

	req := httptest.NewRequest(http.MethodPost, "/v1/images:presign", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "better-auth.session_token", Value: "valid-token"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d", w.Code)
	}
}

func TestPresign_RejectsTooLarge(t *testing.T) {
	t.Parallel()
	router := newTestRouter(t, domain.RoleAnnotator)
	body, _ := json.Marshal(map[string]any{
		"content_type": "image/png",
		"byte_size":    domain.MaxImageBytes + 1,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/images:presign", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "better-auth.session_token", Value: "valid-token"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("want 413, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestRequestIDIsEchoed(t *testing.T) {
	t.Parallel()
	router := newTestRouter(t, domain.RoleAnnotator)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("X-Request-ID", "test-req-123")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if got := w.Header().Get("X-Request-ID"); got != "test-req-123" {
		t.Fatalf("want echoed request id, got %q", got)
	}
}
