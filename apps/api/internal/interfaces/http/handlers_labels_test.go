package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/visionloop/api/internal/domain"
	"github.com/visionloop/api/internal/infrastructure/eventbus"
	httpapi "github.com/visionloop/api/internal/interfaces/http"
)

// ── stub ───────────────────────────────────────────────────────────────────

type stubLabels struct {
	items []domain.Label
	err   error
	calls int
}

func (s *stubLabels) ListByOrg(_ context.Context, _ uuid.UUID) ([]domain.Label, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return s.items, nil
}

func newTestRouterWithLabels(t *testing.T, role domain.Role, lbl *stubLabels) http.Handler {
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
	// Avoid the typed-nil-interface gotcha: only assign LabelsRepo when
	// the caller actually supplied a stub.
	if lbl != nil {
		deps.LabelsRepo = lbl
	}
	return httpapi.NewRouter(deps)
}

func newLabelsReq() *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/v1/labels", nil)
	r.AddCookie(&http.Cookie{Name: "better-auth.session_token", Value: "valid-token"})
	return r
}

// ── tests ──────────────────────────────────────────────────────────────────

func TestListLabels_HappyPath(t *testing.T) {
	t.Parallel()
	id1, id2 := uuid.New(), uuid.New()
	lbl := &stubLabels{
		items: []domain.Label{
			{ID: id1, Name: "car", Color: "#4a8ff5", Description: "vehicle"},
			{ID: id2, Name: "person", Color: "#3ecf8e"},
		},
	}
	router := newTestRouterWithLabels(t, domain.RoleAnnotator, lbl)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, newLabelsReq())

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Items) != 2 {
		t.Fatalf("items: got %d want 2", len(resp.Items))
	}
	if resp.Items[0]["name"] != "car" {
		t.Errorf("first item name: got %v", resp.Items[0]["name"])
	}
	if resp.Items[0]["color"] != "#4a8ff5" {
		t.Errorf("first item color: got %v", resp.Items[0]["color"])
	}
	// archived not exposed in DTO; created_at is.
	if _, has := resp.Items[0]["archived"]; has {
		t.Errorf("archived must NOT leak into DTO")
	}
	if _, has := resp.Items[0]["created_at"]; !has {
		t.Errorf("created_at expected in DTO")
	}
	if lbl.calls != 1 {
		t.Errorf("ListByOrg calls: got %d want 1", lbl.calls)
	}
}

func TestListLabels_AllowsViewerRole(t *testing.T) {
	t.Parallel()
	lbl := &stubLabels{items: []domain.Label{{ID: uuid.New(), Name: "x", Color: "#000000"}}}
	router := newTestRouterWithLabels(t, domain.RoleViewer, lbl)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, newLabelsReq())

	if w.Code != http.StatusOK {
		t.Fatalf("viewers must be allowed to read labels, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestListLabels_RepoError_500(t *testing.T) {
	t.Parallel()
	lbl := &stubLabels{err: errors.New("db down")}
	router := newTestRouterWithLabels(t, domain.RoleAnnotator, lbl)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, newLabelsReq())

	if w.Code < 500 {
		t.Fatalf("want 5xx, got %d", w.Code)
	}
}

func TestListLabels_EmptyResultReturnsEmptyArray(t *testing.T) {
	t.Parallel()
	lbl := &stubLabels{items: nil}
	router := newTestRouterWithLabels(t, domain.RoleAnnotator, lbl)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, newLabelsReq())

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	items, _ := resp["items"].([]any)
	if items == nil || len(items) != 0 {
		t.Errorf("expected empty array, got %v", resp["items"])
	}
}

func TestListLabels_RouteSkippedWhenRepoNil(t *testing.T) {
	t.Parallel()
	// Build a router WITHOUT a labels repo — route shouldn't register.
	router := newTestRouterWithLabels(t, domain.RoleAnnotator, nil)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, newLabelsReq())

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404 (no route), got %d", w.Code)
	}
}
