package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/visionloop/api/internal/domain"
)

func newGetSetReq(t *testing.T, imageID, ifNoneMatch string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/v1/annotation-sets/"+imageID, nil)
	r.AddCookie(&http.Cookie{Name: "better-auth.session_token", Value: "valid-token"})
	if ifNoneMatch != "" {
		r.Header.Set("If-None-Match", ifNoneMatch)
	}
	return r
}

func TestGetAnnotationSet_HappyPath(t *testing.T) {
	t.Parallel()
	imgID := uuid.New()
	setID := uuid.New()
	annID := uuid.New()
	anns := &stubAnnotations{
		setFound: true,
		set: domain.AnnotationSet{
			ID: setID, OrgID: callerOrg, ImageID: imgID, Version: 7,
			CreatedBy: uuid.New(), Notes: "wip",
		},
		setItems: []domain.Annotation{{
			ID: annID, OrgID: callerOrg, AnnotationSetID: setID,
			Kind: domain.AnnotationBBox, Geometry: []byte(`{"type":"bbox"}`),
		}},
	}
	router := newTestRouterWithAnnotations(t, domain.RoleViewer, anns)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, newGetSetReq(t, imgID.String(), ""))

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("ETag"); got != `"7"` {
		t.Fatalf("ETag: %q", got)
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if int(resp["version"].(float64)) != 7 {
		t.Fatalf("version: %v", resp["version"])
	}
	if resp["image_id"] != imgID.String() {
		t.Fatalf("image_id: %v", resp["image_id"])
	}
	items := resp["annotations"].([]any)
	if len(items) != 1 {
		t.Fatalf("annotations count: %d", len(items))
	}
}

func TestGetAnnotationSet_IfNoneMatch_304(t *testing.T) {
	t.Parallel()
	imgID := uuid.New()
	anns := &stubAnnotations{
		setFound: true,
		set:      domain.AnnotationSet{OrgID: callerOrg, ImageID: imgID, Version: 12},
	}
	router := newTestRouterWithAnnotations(t, domain.RoleAnnotator, anns)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, newGetSetReq(t, imgID.String(), `"12"`))

	if w.Code != http.StatusNotModified {
		t.Fatalf("want 304, got %d", w.Code)
	}
	if got := w.Header().Get("ETag"); got != `"12"` {
		t.Fatalf("ETag: %q", got)
	}
	if w.Body.Len() != 0 {
		t.Fatalf("304 body must be empty, got %q", w.Body.String())
	}
}

func TestGetAnnotationSet_IfNoneMatchMismatch_200(t *testing.T) {
	t.Parallel()
	imgID := uuid.New()
	anns := &stubAnnotations{
		setFound: true,
		set:      domain.AnnotationSet{OrgID: callerOrg, ImageID: imgID, Version: 12},
	}
	router := newTestRouterWithAnnotations(t, domain.RoleAnnotator, anns)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, newGetSetReq(t, imgID.String(), `"3"`))
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 (etag mismatch), got %d", w.Code)
	}
}

func TestGetAnnotationSet_NoSet_404(t *testing.T) {
	t.Parallel()
	router := newTestRouterWithAnnotations(t, domain.RoleAnnotator, &stubAnnotations{notFound: true})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, newGetSetReq(t, uuid.New().String(), ""))
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", w.Code)
	}
}

func TestGetAnnotationSet_CrossOrg_404(t *testing.T) {
	t.Parallel()
	imgID := uuid.New()
	otherOrg := uuid.New()
	anns := &stubAnnotations{
		setFound: true,
		set:      domain.AnnotationSet{OrgID: otherOrg, ImageID: imgID, Version: 1},
	}
	router := newTestRouterWithAnnotations(t, domain.RoleAnnotator, anns)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, newGetSetReq(t, imgID.String(), ""))
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestGetAnnotationSet_BadImageID_400(t *testing.T) {
	t.Parallel()
	router := newTestRouterWithAnnotations(t, domain.RoleAnnotator, &stubAnnotations{notFound: true})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, newGetSetReq(t, "not-a-uuid", ""))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}
