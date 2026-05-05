package httpapi_test

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/visionloop/api/internal/domain"
	"github.com/visionloop/api/internal/infrastructure/eventbus"
)

func sign(jobID string) string {
	mac := hmac.New(sha256.New, []byte(testCallbackHMAC))
	mac.Write([]byte(jobID))
	return hex.EncodeToString(mac.Sum(nil))
}

func newCallbackReq(t *testing.T, jobID string, body any, opts ...func(*http.Request)) *http.Request {
	t.Helper()
	b, _ := json.Marshal(body)
	r := httptest.NewRequest(http.MethodPost, "/internal/jobs/callback", bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+testCallbackToken)
	r.Header.Set("X-Job-ID", jobID)
	r.Header.Set("X-Job-Signature", sign(jobID))
	for _, opt := range opts {
		opt(r)
	}
	return r
}

func TestCallback_HappyPath_PendingToRunning(t *testing.T) {
	t.Parallel()
	imgID := uuid.New()
	id := uuid.New()
	jobs := &stubJobs{created: []domain.Job{{
		ID: id, OrgID: callerOrg, ImageID: &imgID,
		Type: domain.JobTypeAuto, State: domain.JobPending,
	}}}
	hub := eventbus.NewMemoryHub()
	router := newTestRouterWithHub(t, domain.RoleAnnotator,
		&readyImage{id: imgID, orgID: callerOrg}, jobs, &stubPublisher{}, hub)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, newCallbackReq(t, id.String(), map[string]any{
		"state": "running", "attempt": 1,
	}))

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["state"] != "running" {
		t.Fatalf("state: %v", resp["state"])
	}
	if resp["no_change"] != false {
		t.Fatalf("no_change: %v", resp["no_change"])
	}
	if jobs.created[0].State != domain.JobRunning {
		t.Fatalf("repo state: %v", jobs.created[0].State)
	}
}

func TestCallback_TerminalReplay_NoChange(t *testing.T) {
	t.Parallel()
	imgID := uuid.New()
	id := uuid.New()
	jobs := &stubJobs{created: []domain.Job{{
		ID: id, OrgID: callerOrg, ImageID: &imgID,
		Type: domain.JobTypeAuto, State: domain.JobSucceeded, Attempt: 1,
	}}}
	router := newTestRouterWithHub(t, domain.RoleAnnotator,
		&readyImage{id: imgID, orgID: callerOrg}, jobs, &stubPublisher{}, eventbus.NewMemoryHub())

	w := httptest.NewRecorder()
	router.ServeHTTP(w, newCallbackReq(t, id.String(), map[string]any{
		"state": "succeeded", "attempt": 1,
	}))
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 on idempotent replay, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["no_change"] != true {
		t.Fatalf("no_change: %v want true", resp["no_change"])
	}
}

func TestCallback_RejectsInvalidState(t *testing.T) {
	t.Parallel()
	imgID := uuid.New()
	id := uuid.New()
	jobs := &stubJobs{created: []domain.Job{{ID: id, OrgID: callerOrg, ImageID: &imgID, State: domain.JobPending, Type: domain.JobTypeAuto}}}
	router := newTestRouterWithHub(t, domain.RoleAnnotator,
		&readyImage{id: imgID, orgID: callerOrg}, jobs, &stubPublisher{}, eventbus.NewMemoryHub())

	w := httptest.NewRecorder()
	router.ServeHTTP(w, newCallbackReq(t, id.String(), map[string]any{"state": "cancelled"}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

func TestCallback_BadBearer_401(t *testing.T) {
	t.Parallel()
	imgID := uuid.New()
	id := uuid.New()
	jobs := &stubJobs{created: []domain.Job{{ID: id, OrgID: callerOrg, ImageID: &imgID, State: domain.JobPending, Type: domain.JobTypeAuto}}}
	router := newTestRouterWithHub(t, domain.RoleAnnotator,
		&readyImage{id: imgID, orgID: callerOrg}, jobs, &stubPublisher{}, eventbus.NewMemoryHub())

	w := httptest.NewRecorder()
	router.ServeHTTP(w, newCallbackReq(t, id.String(), map[string]any{"state": "running"}, func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer wrong-token-with-padding-to-32-bytes-yes-ok")
	}))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
}

func TestCallback_BadSignature_401(t *testing.T) {
	t.Parallel()
	imgID := uuid.New()
	id := uuid.New()
	jobs := &stubJobs{created: []domain.Job{{ID: id, OrgID: callerOrg, ImageID: &imgID, State: domain.JobPending, Type: domain.JobTypeAuto}}}
	router := newTestRouterWithHub(t, domain.RoleAnnotator,
		&readyImage{id: imgID, orgID: callerOrg}, jobs, &stubPublisher{}, eventbus.NewMemoryHub())

	w := httptest.NewRecorder()
	router.ServeHTTP(w, newCallbackReq(t, id.String(), map[string]any{"state": "running"}, func(r *http.Request) {
		// Sign a *different* job id — server recomputes against X-Job-ID.
		bad := hmac.New(sha256.New, []byte(testCallbackHMAC))
		bad.Write([]byte("not-the-real-job"))
		r.Header.Set("X-Job-Signature", hex.EncodeToString(bad.Sum(nil)))
	}))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
}

func TestCallback_MissingHeaders_401(t *testing.T) {
	t.Parallel()
	imgID := uuid.New()
	id := uuid.New()
	jobs := &stubJobs{created: []domain.Job{{ID: id, OrgID: callerOrg, ImageID: &imgID, State: domain.JobPending, Type: domain.JobTypeAuto}}}
	router := newTestRouterWithHub(t, domain.RoleAnnotator,
		&readyImage{id: imgID, orgID: callerOrg}, jobs, &stubPublisher{}, eventbus.NewMemoryHub())

	w := httptest.NewRecorder()
	router.ServeHTTP(w, newCallbackReq(t, id.String(), map[string]any{"state": "running"}, func(r *http.Request) {
		r.Header.Del("X-Job-Signature")
	}))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
}

func TestCallback_PublishesToHub(t *testing.T) {
	t.Parallel()
	imgID := uuid.New()
	id := uuid.New()
	jobs := &stubJobs{created: []domain.Job{{
		ID: id, OrgID: callerOrg, ImageID: &imgID,
		Type: domain.JobTypeAuto, State: domain.JobPending,
	}}}
	hub := eventbus.NewMemoryHub()

	// Subscribe before issuing the callback.
	ch, unsub := hub.Subscribe(t.Context(), id)
	defer unsub()

	router := newTestRouterWithHub(t, domain.RoleAnnotator,
		&readyImage{id: imgID, orgID: callerOrg}, jobs, &stubPublisher{}, hub)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, newCallbackReq(t, id.String(), map[string]any{"state": "succeeded"}))
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}

	select {
	case ev := <-ch:
		if ev.State != "succeeded" {
			t.Fatalf("hub event state: %q", ev.State)
		}
	default:
		t.Fatal("expected hub publish on callback success")
	}
}
