package httpapi_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/visionloop/api/internal/domain"
)

func newPatchReq(t *testing.T, id, ifMatch string, body any) *http.Request {
	t.Helper()
	b, _ := json.Marshal(body)
	r := httptest.NewRequest(http.MethodPatch, "/v1/annotations/"+id, bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	r.AddCookie(&http.Cookie{Name: "better-auth.session_token", Value: "valid-token"})
	if ifMatch != "" {
		r.Header.Set("If-Match", ifMatch)
	}
	return r
}

func TestPatchAnnotation_HappyPath(t *testing.T) {
	t.Parallel()
	annID := uuid.New()
	anns := &stubAnnotations{
		row: domain.Annotation{
			ID: annID, OrgID: callerOrg, AnnotationSetID: uuid.New(),
			Kind: domain.AnnotationBBox, Geometry: []byte(`{"type":"bbox","coords":[0,0,1,1]}`),
		},
		version: 5,
	}
	router := newTestRouterWithAnnotations(t, domain.RoleAnnotator, anns)

	w := httptest.NewRecorder()
	accepted := true
	router.ServeHTTP(w, newPatchReq(t, annID.String(), "5", map[string]any{
		"geometry":       json.RawMessage(`{"type":"bbox","coords":[0,0,2,2]}`),
		"human_accepted": accepted,
	}))

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("ETag"); got != `"6"` {
		t.Fatalf("ETag: %q", got)
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if v, _ := resp["new_version"].(float64); int(v) != 6 {
		t.Fatalf("new_version: %v", resp["new_version"])
	}
	// Stub should now reflect the bumped version.
	if anns.version != 6 {
		t.Fatalf("repo version: %d", anns.version)
	}
}

func TestPatchAnnotation_StaleIfMatch_409WithCurrentVersion(t *testing.T) {
	t.Parallel()
	annID := uuid.New()
	anns := &stubAnnotations{
		row:     domain.Annotation{ID: annID, OrgID: callerOrg, Kind: domain.AnnotationBBox},
		version: 7,
	}
	router := newTestRouterWithAnnotations(t, domain.RoleAnnotator, anns)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, newPatchReq(t, annID.String(), "3", map[string]any{
		"human_accepted": true,
	}))

	if w.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	errBody, _ := resp["error"].(map[string]any)
	if errBody["code"] != "conflict" {
		t.Fatalf("error.code: %v", errBody["code"])
	}
	if v, _ := errBody["current_version"].(float64); int(v) != 7 {
		t.Fatalf("current_version: %v want 7", errBody["current_version"])
	}
}

func TestPatchAnnotation_MissingIfMatch_428(t *testing.T) {
	t.Parallel()
	annID := uuid.New()
	router := newTestRouterWithAnnotations(t, domain.RoleAnnotator, &stubAnnotations{
		row: domain.Annotation{ID: annID, OrgID: callerOrg}, version: 1,
	})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, newPatchReq(t, annID.String(), "", map[string]any{
		"human_accepted": true,
	}))

	if w.Code != http.StatusPreconditionRequired {
		t.Fatalf("want 428, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestPatchAnnotation_MalformedIfMatch_412(t *testing.T) {
	t.Parallel()
	annID := uuid.New()
	router := newTestRouterWithAnnotations(t, domain.RoleAnnotator, &stubAnnotations{
		row: domain.Annotation{ID: annID, OrgID: callerOrg}, version: 1,
	})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, newPatchReq(t, annID.String(), "not-a-number", map[string]any{
		"human_accepted": true,
	}))

	if w.Code != http.StatusPreconditionFailed {
		t.Fatalf("want 412, got %d", w.Code)
	}
}

func TestPatchAnnotation_NotFound_404(t *testing.T) {
	t.Parallel()
	router := newTestRouterWithAnnotations(t, domain.RoleAnnotator, &stubAnnotations{notFound: true})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, newPatchReq(t, uuid.New().String(), "1", map[string]any{
		"human_accepted": true,
	}))
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", w.Code)
	}
}

func TestPatchAnnotation_ViewerForbidden(t *testing.T) {
	t.Parallel()
	annID := uuid.New()
	router := newTestRouterWithAnnotations(t, domain.RoleViewer, &stubAnnotations{
		row: domain.Annotation{ID: annID, OrgID: callerOrg}, version: 1,
	})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, newPatchReq(t, annID.String(), "1", map[string]any{
		"human_accepted": true,
	}))
	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestPatchAnnotation_NoChanges_400(t *testing.T) {
	t.Parallel()
	annID := uuid.New()
	router := newTestRouterWithAnnotations(t, domain.RoleAnnotator, &stubAnnotations{
		row: domain.Annotation{ID: annID, OrgID: callerOrg}, version: 1,
	})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, newPatchReq(t, annID.String(), "1", map[string]any{}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

func TestPatchAnnotation_QuotedIfMatchAccepted(t *testing.T) {
	t.Parallel()
	annID := uuid.New()
	anns := &stubAnnotations{
		row: domain.Annotation{ID: annID, OrgID: callerOrg, Kind: domain.AnnotationBBox},
		version: 1,
	}
	router := newTestRouterWithAnnotations(t, domain.RoleAnnotator, anns)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, newPatchReq(t, annID.String(), `"1"`, map[string]any{
		"human_accepted": true,
	}))
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 with quoted ETag form, got %d", w.Code)
	}
}

// ── POST /v1/annotations (Slice B4) ────────────────────────────────────────

func newCreateReq(t *testing.T, ifMatch string, body any) *http.Request {
	t.Helper()
	b, _ := json.Marshal(body)
	r := httptest.NewRequest(http.MethodPost, "/v1/annotations", bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	r.AddCookie(&http.Cookie{Name: "better-auth.session_token", Value: "valid-token"})
	if ifMatch != "" {
		r.Header.Set("If-Match", ifMatch)
	}
	return r
}

func TestCreateAnnotation_HappyPath_201WithETag(t *testing.T) {
	t.Parallel()
	setID := uuid.New()
	anns := &stubAnnotations{
		row:     domain.Annotation{OrgID: callerOrg, AnnotationSetID: setID},
		version: 3,
	}
	router := newTestRouterWithAnnotations(t, domain.RoleAnnotator, anns)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, newCreateReq(t, "3", map[string]any{
		"annotation_set_id": setID.String(),
		"kind":              "bbox",
		"geometry":          json.RawMessage(`{"x":0,"y":0,"w":1,"h":1}`),
	}))

	if w.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d body=%s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("ETag"); got != `"4"` {
		t.Errorf("ETag: got %q want %q", got, `"4"`)
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if v, _ := resp["new_version"].(float64); int(v) != 4 {
		t.Errorf("new_version: %v", resp["new_version"])
	}
}

func TestCreateAnnotation_StaleIfMatch_409WithCurrentVersion(t *testing.T) {
	t.Parallel()
	setID := uuid.New()
	anns := &stubAnnotations{
		row:     domain.Annotation{OrgID: callerOrg, AnnotationSetID: setID},
		version: 9,
	}
	router := newTestRouterWithAnnotations(t, domain.RoleAnnotator, anns)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, newCreateReq(t, "1", map[string]any{
		"annotation_set_id": setID.String(),
		"kind":              "bbox",
		"geometry":          json.RawMessage(`{"x":0,"y":0,"w":1,"h":1}`),
	}))

	if w.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	errBody, _ := resp["error"].(map[string]any)
	if v, _ := errBody["current_version"].(float64); int(v) != 9 {
		t.Errorf("current_version: %v want 9", errBody["current_version"])
	}
}

func TestCreateAnnotation_MissingIfMatch_428(t *testing.T) {
	t.Parallel()
	setID := uuid.New()
	router := newTestRouterWithAnnotations(t, domain.RoleAnnotator, &stubAnnotations{
		row:     domain.Annotation{OrgID: callerOrg, AnnotationSetID: setID},
		version: 1,
	})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, newCreateReq(t, "", map[string]any{
		"annotation_set_id": setID.String(),
		"kind":              "bbox",
		"geometry":          json.RawMessage(`{"x":0}`),
	}))

	if w.Code != http.StatusPreconditionRequired {
		t.Fatalf("want 428, got %d", w.Code)
	}
}

func TestCreateAnnotation_BadSetID_400(t *testing.T) {
	t.Parallel()
	router := newTestRouterWithAnnotations(t, domain.RoleAnnotator, &stubAnnotations{version: 1})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, newCreateReq(t, "1", map[string]any{
		"annotation_set_id": "not-a-uuid",
		"kind":              "bbox",
		"geometry":          json.RawMessage(`{"x":0}`),
	}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

func TestCreateAnnotation_ViewerForbidden_403(t *testing.T) {
	t.Parallel()
	setID := uuid.New()
	router := newTestRouterWithAnnotations(t, domain.RoleViewer, &stubAnnotations{version: 1})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, newCreateReq(t, "1", map[string]any{
		"annotation_set_id": setID.String(),
		"kind":              "bbox",
		"geometry":          json.RawMessage(`{"x":0}`),
	}))
	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d body=%s", w.Code, w.Body.String())
	}
}

// ── DELETE /v1/annotations/:id (Slice B4) ──────────────────────────────────

func newDeleteReq(t *testing.T, id, ifMatch string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodDelete, "/v1/annotations/"+id, nil)
	r.AddCookie(&http.Cookie{Name: "better-auth.session_token", Value: "valid-token"})
	if ifMatch != "" {
		r.Header.Set("If-Match", ifMatch)
	}
	return r
}

func TestDeleteAnnotation_HappyPath_200WithETag(t *testing.T) {
	t.Parallel()
	annID := uuid.New()
	anns := &stubAnnotations{
		row:     domain.Annotation{ID: annID, OrgID: callerOrg, Kind: domain.AnnotationBBox},
		version: 4,
	}
	router := newTestRouterWithAnnotations(t, domain.RoleAnnotator, anns)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, newDeleteReq(t, annID.String(), "4"))

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("ETag"); got != `"5"` {
		t.Errorf("ETag: got %q want %q", got, `"5"`)
	}
}

func TestDeleteAnnotation_StaleIfMatch_409(t *testing.T) {
	t.Parallel()
	annID := uuid.New()
	anns := &stubAnnotations{
		row:     domain.Annotation{ID: annID, OrgID: callerOrg, Kind: domain.AnnotationBBox},
		version: 8,
	}
	router := newTestRouterWithAnnotations(t, domain.RoleAnnotator, anns)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, newDeleteReq(t, annID.String(), "2"))

	if w.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestDeleteAnnotation_MissingIfMatch_428(t *testing.T) {
	t.Parallel()
	annID := uuid.New()
	router := newTestRouterWithAnnotations(t, domain.RoleAnnotator, &stubAnnotations{
		row: domain.Annotation{ID: annID, OrgID: callerOrg, Kind: domain.AnnotationBBox}, version: 1,
	})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, newDeleteReq(t, annID.String(), ""))
	if w.Code != http.StatusPreconditionRequired {
		t.Fatalf("want 428, got %d", w.Code)
	}
}

func TestDeleteAnnotation_BadID_400(t *testing.T) {
	t.Parallel()
	router := newTestRouterWithAnnotations(t, domain.RoleAnnotator, &stubAnnotations{version: 1})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, newDeleteReq(t, "not-a-uuid", "1"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

func TestDeleteAnnotation_ViewerForbidden_403(t *testing.T) {
	t.Parallel()
	annID := uuid.New()
	router := newTestRouterWithAnnotations(t, domain.RoleViewer, &stubAnnotations{
		row: domain.Annotation{ID: annID, OrgID: callerOrg, Kind: domain.AnnotationBBox}, version: 1,
	})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, newDeleteReq(t, annID.String(), "1"))
	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d body=%s", w.Code, w.Body.String())
	}
}
