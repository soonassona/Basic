package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/visionloop/api/internal/domain"
)

// readyImage stub returns a ready image whose org matches caller's org.
type readyImage struct {
	id    uuid.UUID
	orgID uuid.UUID
}

func (r *readyImage) Get(_ context.Context, id, orgID uuid.UUID) (domain.Image, error) {
	if id != r.id || orgID != r.orgID {
		return domain.Image{}, domain.ErrNotFound
	}
	return domain.Image{
		ID:          r.id,
		OrgID:       r.orgID,
		Status:      domain.ImageReady,
		StorageKey:  "orgs/" + r.orgID.String() + "/images/" + r.id.String() + ".jpg",
		ContentType: "image/jpeg",
		ByteSize:    1024,
	}, nil
}
func (r *readyImage) Create(context.Context, domain.Image) (domain.Image, error) {
	return domain.Image{}, nil
}
func (r *readyImage) Finalize(context.Context, uuid.UUID, uuid.UUID, string, int32, int32, string) (domain.Image, error) {
	return domain.Image{}, nil
}
func (r *readyImage) List(context.Context, uuid.UUID, domain.ImageStatus, int32, int32) ([]domain.Image, int64, error) {
	return nil, 0, nil
}

// callerOrg matches the constant used by newTestRouterWith.
var callerOrg = uuid.MustParse("00000000-0000-0000-0000-000000000020")

func newJobReq(t *testing.T, body any) *http.Request {
	t.Helper()
	b, _ := json.Marshal(body)
	r := httptest.NewRequest(http.MethodPost, "/v1/jobs", bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	r.AddCookie(&http.Cookie{Name: "better-auth.session_token", Value: "valid-token"})
	return r
}

func TestSubmitJob_HappyPath(t *testing.T) {
	t.Parallel()
	imgID := uuid.New()
	images := &readyImage{id: imgID, orgID: callerOrg}
	jobs := &stubJobs{}
	pub := &stubPublisher{}
	router := newTestRouterWith(t, domain.RoleAnnotator, images, jobs, pub)

	req := newJobReq(t, map[string]any{
		"image_id": imgID.String(),
		"type":     "auto",
		"payload":  map[string]any{"score": 0.5},
	})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d body=%s", w.Code, w.Body.String())
	}
	if pub.calls != 1 {
		t.Fatalf("publishes: got %d want 1", pub.calls)
	}
	if got := w.Header().Get("ETag"); got == "" {
		t.Fatal("expected ETag header")
	}
	if got := w.Header().Get("Idempotent-Replayed"); got != "" {
		t.Fatalf("did not expect Idempotent-Replayed on fresh submission, got %q", got)
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["state"] != "pending" {
		t.Fatalf("state: %v", resp["state"])
	}
	if resp["replayed"] != false {
		t.Fatalf("replayed: %v", resp["replayed"])
	}
}

func TestSubmitJob_Replayed_SetsHeader(t *testing.T) {
	t.Parallel()
	imgID := uuid.New()
	existingID := uuid.New()
	images := &readyImage{id: imgID, orgID: callerOrg}
	jobs := &stubJobs{
		dedupHit: &domain.Job{
			ID: existingID, OrgID: callerOrg, ImageID: &imgID,
			Type: domain.JobTypeAuto, State: domain.JobPending,
			DedupKey: "x", CreatedAt: time.Now().UTC().Format(time.RFC3339),
		},
	}
	pub := &stubPublisher{}
	router := newTestRouterWith(t, domain.RoleAnnotator, images, jobs, pub)

	req := newJobReq(t, map[string]any{
		"image_id": imgID.String(), "type": "auto", "payload": map[string]any{},
	})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d body=%s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Idempotent-Replayed"); got != "true" {
		t.Fatalf("Idempotent-Replayed header: got %q want true", got)
	}
	if pub.calls != 0 {
		t.Fatalf("expected zero publishes on replay, got %d", pub.calls)
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["replayed"] != true {
		t.Fatalf("replayed flag: %v", resp["replayed"])
	}
	if resp["id"] != existingID.String() {
		t.Fatalf("id: %v want %s", resp["id"], existingID)
	}
}

func TestSubmitJob_PublishFails_503WithRetryAfter(t *testing.T) {
	t.Parallel()
	imgID := uuid.New()
	images := &readyImage{id: imgID, orgID: callerOrg}
	jobs := &stubJobs{}
	pub := &stubPublisher{failWith: errPublish("broker offline")}
	router := newTestRouterWith(t, domain.RoleAnnotator, images, jobs, pub)

	req := newJobReq(t, map[string]any{
		"image_id": imgID.String(), "type": "box", "payload": map[string]any{"box": []int{1, 2, 3, 4}},
	})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d body=%s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Retry-After"); got == "" {
		t.Fatal("expected Retry-After header")
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	errBody, _ := resp["error"].(map[string]any)
	if errBody["code"] != "JOB_PUBLISH_FAILED" {
		t.Fatalf("error.code: %v", errBody["code"])
	}
	if _, ok := errBody["job_id"]; !ok {
		t.Fatal("expected error.job_id")
	}
	if _, ok := errBody["retry_after_ms"]; !ok {
		t.Fatal("expected error.retry_after_ms")
	}
	if !jobs.markErroredOK {
		t.Fatal("expected use-case to mark row errored")
	}
}

func TestSubmitJob_RejectsTrainType(t *testing.T) {
	t.Parallel()
	imgID := uuid.New()
	images := &readyImage{id: imgID, orgID: callerOrg}
	router := newTestRouterWith(t, domain.RoleAnnotator, images, &stubJobs{}, &stubPublisher{})

	req := newJobReq(t, map[string]any{"image_id": imgID.String(), "type": "train"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestSubmitJob_ImageOtherOrg_404NotEnumerable(t *testing.T) {
	t.Parallel()
	// Image belongs to a *different* org than caller's; readyImage returns
	// ErrNotFound for that.
	images := &readyImage{id: uuid.New(), orgID: uuid.New()}
	router := newTestRouterWith(t, domain.RoleAnnotator, images, &stubJobs{}, &stubPublisher{})

	req := newJobReq(t, map[string]any{"image_id": uuid.New().String(), "type": "auto"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404 (no enumeration), got %d body=%s", w.Code, w.Body.String())
	}
}

func TestSubmitJob_ViewerForbidden(t *testing.T) {
	t.Parallel()
	imgID := uuid.New()
	images := &readyImage{id: imgID, orgID: callerOrg}
	router := newTestRouterWith(t, domain.RoleViewer, images, &stubJobs{}, &stubPublisher{})

	req := newJobReq(t, map[string]any{"image_id": imgID.String(), "type": "auto"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d", w.Code)
	}
}

func TestSubmitJob_BadImageID(t *testing.T) {
	t.Parallel()
	images := &readyImage{id: uuid.New(), orgID: callerOrg}
	router := newTestRouterWith(t, domain.RoleAnnotator, images, &stubJobs{}, &stubPublisher{})

	req := newJobReq(t, map[string]any{"image_id": "not-a-uuid", "type": "auto"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

// errPublish is a small helper so the test file doesn't need errors import.
type errPublish string

func (e errPublish) Error() string { return string(e) }

// GET /v1/jobs/:id —————————————————————————————————————————————————

func newGetReq(t *testing.T, id string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/v1/jobs/"+id, nil)
	r.AddCookie(&http.Cookie{Name: "better-auth.session_token", Value: "valid-token"})
	return r
}

func TestGetJob_HappyPath(t *testing.T) {
	t.Parallel()
	imgID := uuid.New()
	images := &readyImage{id: imgID, orgID: callerOrg}
	jobs := &stubJobs{}
	router := newTestRouterWith(t, domain.RoleAnnotator, images, jobs, &stubPublisher{})

	// Submit first to seed a row through the use-case → keeps test paths
	// honest (we read what we wrote).
	w := httptest.NewRecorder()
	router.ServeHTTP(w, newJobReq(t, map[string]any{
		"image_id": imgID.String(), "type": "auto", "payload": map[string]any{},
	}))
	if w.Code != http.StatusAccepted {
		t.Fatalf("setup submit: %d body=%s", w.Code, w.Body.String())
	}
	var submitted map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &submitted)
	jobID := submitted["id"].(string)

	gw := httptest.NewRecorder()
	router.ServeHTTP(gw, newGetReq(t, jobID))
	if gw.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", gw.Code, gw.Body.String())
	}
	if got := gw.Header().Get("ETag"); got != `"`+jobID+`"` {
		t.Fatalf("ETag: got %q", got)
	}
	var dto map[string]any
	_ = json.Unmarshal(gw.Body.Bytes(), &dto)
	if dto["id"] != jobID {
		t.Fatalf("id: %v", dto["id"])
	}
	if dto["state"] != "pending" {
		t.Fatalf("state: %v", dto["state"])
	}
	if dto["type"] != "auto" {
		t.Fatalf("type: %v", dto["type"])
	}
}

func TestGetJob_ViewerAllowed(t *testing.T) {
	t.Parallel()
	imgID := uuid.New()
	images := &readyImage{id: imgID, orgID: callerOrg}
	// Pre-seed a row directly in the stub (viewer can't POST).
	id := uuid.New()
	jobs := &stubJobs{created: []domain.Job{{
		ID: id, OrgID: callerOrg, ImageID: &imgID,
		Type: domain.JobTypeAuto, State: domain.JobPending, DedupKey: "x",
	}}}
	router := newTestRouterWith(t, domain.RoleViewer, images, jobs, &stubPublisher{})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, newGetReq(t, id.String()))
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 for viewer, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestGetJob_NotFound(t *testing.T) {
	t.Parallel()
	images := &readyImage{id: uuid.New(), orgID: callerOrg}
	router := newTestRouterWith(t, domain.RoleAnnotator, images, &stubJobs{}, &stubPublisher{})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, newGetReq(t, uuid.New().String()))
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", w.Code)
	}
}

func TestGetJob_CrossOrgIs404NotEnumerable(t *testing.T) {
	t.Parallel()
	imgID := uuid.New()
	images := &readyImage{id: imgID, orgID: callerOrg}
	otherOrg := uuid.New()
	id := uuid.New()
	// Row exists but in a different org — stub.Get filters by org_id.
	jobs := &stubJobs{created: []domain.Job{{
		ID: id, OrgID: otherOrg, ImageID: &imgID,
		Type: domain.JobTypeAuto, State: domain.JobPending,
	}}}
	router := newTestRouterWith(t, domain.RoleAnnotator, images, jobs, &stubPublisher{})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, newGetReq(t, id.String()))
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404 (no enumeration leak), got %d", w.Code)
	}
}

func TestGetJob_BadID(t *testing.T) {
	t.Parallel()
	images := &readyImage{id: uuid.New(), orgID: callerOrg}
	router := newTestRouterWith(t, domain.RoleAnnotator, images, &stubJobs{}, &stubPublisher{})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, newGetReq(t, "not-a-uuid"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}
