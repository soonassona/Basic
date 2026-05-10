package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/visionloop/api/internal/application/annotations"
	"github.com/visionloop/api/internal/domain"
)

type AnnotationHandlers struct {
	Patch  annotations.PatchAnnotation
	Create annotations.CreateAnnotation
	Delete annotations.DeleteAnnotation
	GetSet annotations.GetAnnotationSet
}

type createAnnotationRequest struct {
	AnnotationSetID string          `json:"annotation_set_id" binding:"required"`
	Kind            string          `json:"kind"              binding:"required"`
	Geometry        json.RawMessage `json:"geometry"          binding:"required"`
	LabelID         *string         `json:"label_id"`
}

type patchAnnotationRequest struct {
	Geometry      json.RawMessage `json:"geometry"`
	LabelID       *string         `json:"label_id"`
	HumanAccepted *bool           `json:"human_accepted"`
}

type annotationDTO struct {
	ID              uuid.UUID `json:"id"`
	AnnotationSetID uuid.UUID `json:"annotation_set_id"`
	LabelID         *string   `json:"label_id"`
	Kind            string    `json:"kind"`
	Geometry        json.RawMessage `json:"geometry"`
	HumanAccepted   *bool     `json:"human_accepted"`
}

func toAnnotationDTO(a domain.Annotation) annotationDTO {
	dto := annotationDTO{
		ID:              a.ID,
		AnnotationSetID: a.AnnotationSetID,
		Kind:            string(a.Kind),
		Geometry:        json.RawMessage(a.Geometry),
		HumanAccepted:   a.HumanAccepted,
	}
	if a.LabelID != nil {
		s := a.LabelID.String()
		dto.LabelID = &s
	}
	return dto
}

// PatchAnnotation — PATCH /v1/annotations/:id with If-Match header.
//
// Wire contract (spec §10):
//   - Request: If-Match: <int64>; body has any of geometry/label_id/human_accepted.
//   - 200 + ETag: <new_version> on success.
//   - 409 with body { code: "conflict", current_version: <int> } on stale.
//   - 412 if If-Match is missing or malformed.
//   - 404 if the annotation doesn't exist or belongs to another org.
//   - 403 if caller is a viewer.
func (h *AnnotationHandlers) PatchAnnotation(c *gin.Context) {
	caller, ok := callerFrom(c)
	if !ok {
		abortJSON(c, http.StatusUnauthorized, "unauthorized", "missing caller")
		return
	}

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		abortJSON(c, http.StatusBadRequest, "invalid_input", "id must be a uuid")
		return
	}

	ifMatchRaw := c.GetHeader("If-Match")
	if ifMatchRaw == "" {
		abortJSON(c, http.StatusPreconditionRequired, "if_match_required", "If-Match header is required")
		return
	}
	ifMatch, err := parseIfMatch(ifMatchRaw)
	if err != nil {
		abortJSON(c, http.StatusPreconditionFailed, "if_match_invalid", err.Error())
		return
	}

	var req patchAnnotationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		abortJSON(c, http.StatusBadRequest, "invalid_input", err.Error())
		return
	}

	patch := domain.AnnotationPatch{
		Geometry:      []byte(req.Geometry),
		HumanAccepted: req.HumanAccepted,
	}
	if req.LabelID != nil {
		labelID, err := uuid.Parse(*req.LabelID)
		if err != nil {
			abortJSON(c, http.StatusBadRequest, "invalid_input", "label_id must be a uuid")
			return
		}
		patch.LabelID = &labelID
	}

	out, err := h.Patch.Execute(c.Request.Context(), annotations.PatchAnnotationInput{
		Caller: caller, ID: id, IfMatch: ifMatch, Patch: patch,
	})
	if err != nil {
		if errors.Is(err, domain.ErrConflict) {
			c.AbortWithStatusJSON(http.StatusConflict, gin.H{
				"error": gin.H{
					"code":            "conflict",
					"message":         "If-Match version is stale; reload and retry",
					"current_version": out.NewVersion,
					"request_id":      c.GetString("vl.request_id"),
				},
			})
			return
		}
		status, code := httpStatusFor(err)
		abortJSON(c, status, code, err.Error())
		return
	}

	c.Header("ETag", `"`+strconv.FormatInt(out.NewVersion, 10)+`"`)
	c.JSON(http.StatusOK, gin.H{
		"annotation":  toAnnotationDTO(out.Annotation),
		"new_version": out.NewVersion,
	})
}

type annotationSetDTO struct {
	ID          uuid.UUID       `json:"id"`
	ImageID     uuid.UUID       `json:"image_id"`
	Version     int64           `json:"version"`
	Notes       string          `json:"notes,omitempty"`
	Annotations []annotationDTO `json:"annotations"`
}

// GetAnnotationSet — GET /v1/annotation-sets/:image_id
//
// Returns the set's version as ETag so the client can use it as
// If-Match in subsequent PATCHes. Honours If-None-Match → 304.
// Read-only; viewers allowed.
func (h *AnnotationHandlers) GetAnnotationSet(c *gin.Context) {
	caller, ok := callerFrom(c)
	if !ok {
		abortJSON(c, http.StatusUnauthorized, "unauthorized", "missing caller")
		return
	}
	imageID, err := uuid.Parse(c.Param("image_id"))
	if err != nil {
		abortJSON(c, http.StatusBadRequest, "invalid_input", "image_id must be a uuid")
		return
	}

	out, err := h.GetSet.Execute(c.Request.Context(), annotations.GetAnnotationSetInput{
		Caller: caller, ImageID: imageID,
	})
	if err != nil {
		status, code := httpStatusFor(err)
		abortJSON(c, status, code, err.Error())
		return
	}

	etag := `"` + strconv.FormatInt(out.Set.Version, 10) + `"`
	if c.GetHeader("If-None-Match") == etag {
		c.Header("ETag", etag)
		c.Status(http.StatusNotModified)
		return
	}

	dto := annotationSetDTO{
		ID:          out.Set.ID,
		ImageID:     out.Set.ImageID,
		Version:     out.Set.Version,
		Notes:       out.Set.Notes,
		Annotations: make([]annotationDTO, 0, len(out.Annotations)),
	}
	for _, a := range out.Annotations {
		dto.Annotations = append(dto.Annotations, toAnnotationDTO(a))
	}

	c.Header("ETag", etag)
	c.JSON(http.StatusOK, dto)
}

// CreateAnnotation — POST /v1/annotations with If-Match header.
//
// Wire contract (spec §10):
//   - Body: { annotation_set_id, kind, geometry, label_id? }
//   - Header: If-Match: <set_version>
//   - 201 + ETag: <new_version> on success.
//   - 409 on stale If-Match (same body shape as PATCH).
//   - 412 if If-Match is missing or malformed.
//   - 404 if the annotation_set doesn't exist or belongs to another org.
//   - 403 if caller is a viewer.
func (h *AnnotationHandlers) CreateAnnotation(c *gin.Context) {
	caller, ok := callerFrom(c)
	if !ok {
		abortJSON(c, http.StatusUnauthorized, "unauthorized", "missing caller")
		return
	}

	ifMatchRaw := c.GetHeader("If-Match")
	if ifMatchRaw == "" {
		abortJSON(c, http.StatusPreconditionRequired, "if_match_required", "If-Match header is required")
		return
	}
	ifMatch, err := parseIfMatch(ifMatchRaw)
	if err != nil {
		abortJSON(c, http.StatusPreconditionFailed, "if_match_invalid", err.Error())
		return
	}

	var req createAnnotationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		abortJSON(c, http.StatusBadRequest, "invalid_input", err.Error())
		return
	}

	setID, err := uuid.Parse(req.AnnotationSetID)
	if err != nil {
		abortJSON(c, http.StatusBadRequest, "invalid_input", "annotation_set_id must be a uuid")
		return
	}

	create := domain.AnnotationCreate{
		AnnotationSetID: setID,
		Kind:            domain.AnnotationKind(req.Kind),
		Geometry:        []byte(req.Geometry),
	}
	if req.LabelID != nil {
		labelID, err := uuid.Parse(*req.LabelID)
		if err != nil {
			abortJSON(c, http.StatusBadRequest, "invalid_input", "label_id must be a uuid")
			return
		}
		create.LabelID = &labelID
	}

	out, err := h.Create.Execute(c.Request.Context(), annotations.CreateAnnotationInput{
		Caller: caller, IfMatch: ifMatch, Create: create,
	})
	if err != nil {
		if errors.Is(err, domain.ErrConflict) {
			c.AbortWithStatusJSON(http.StatusConflict, gin.H{
				"error": gin.H{
					"code":            "conflict",
					"message":         "If-Match version is stale; reload and retry",
					"current_version": out.NewVersion,
					"request_id":      c.GetString("vl.request_id"),
				},
			})
			return
		}
		status, code := httpStatusFor(err)
		abortJSON(c, status, code, err.Error())
		return
	}

	c.Header("ETag", `"`+strconv.FormatInt(out.NewVersion, 10)+`"`)
	c.JSON(http.StatusCreated, gin.H{
		"annotation":  toAnnotationDTO(out.Annotation),
		"new_version": out.NewVersion,
	})
}

// DeleteAnnotation — DELETE /v1/annotations/:id with If-Match header.
//
// Wire contract (spec §10):
//   - Header: If-Match: <set_version>
//   - 200 + body { new_version } + ETag header on success.
//   - 409 on stale If-Match (same shape as PATCH/CREATE).
//   - 412 if If-Match is missing or malformed.
//   - 404 if the annotation doesn't exist or belongs to another org.
//   - 403 if caller is a viewer.
func (h *AnnotationHandlers) DeleteAnnotation(c *gin.Context) {
	caller, ok := callerFrom(c)
	if !ok {
		abortJSON(c, http.StatusUnauthorized, "unauthorized", "missing caller")
		return
	}

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		abortJSON(c, http.StatusBadRequest, "invalid_input", "id must be a uuid")
		return
	}

	ifMatchRaw := c.GetHeader("If-Match")
	if ifMatchRaw == "" {
		abortJSON(c, http.StatusPreconditionRequired, "if_match_required", "If-Match header is required")
		return
	}
	ifMatch, err := parseIfMatch(ifMatchRaw)
	if err != nil {
		abortJSON(c, http.StatusPreconditionFailed, "if_match_invalid", err.Error())
		return
	}

	out, err := h.Delete.Execute(c.Request.Context(), annotations.DeleteAnnotationInput{
		Caller: caller, ID: id, IfMatch: ifMatch,
	})
	if err != nil {
		if errors.Is(err, domain.ErrConflict) {
			c.AbortWithStatusJSON(http.StatusConflict, gin.H{
				"error": gin.H{
					"code":            "conflict",
					"message":         "If-Match version is stale; reload and retry",
					"current_version": out.NewVersion,
					"request_id":      c.GetString("vl.request_id"),
				},
			})
			return
		}
		status, code := httpStatusFor(err)
		abortJSON(c, status, code, err.Error())
		return
	}

	c.Header("ETag", `"`+strconv.FormatInt(out.NewVersion, 10)+`"`)
	c.JSON(http.StatusOK, gin.H{"new_version": out.NewVersion})
}

// parseIfMatch accepts both `123` and `"123"` (RFC 7232 weak/strong
// ETag forms). We treat versions as strong validators — weak (`W/"x"`)
// is rejected because annotation versions are exact.
func parseIfMatch(raw string) (int64, error) {
	v := raw
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		v = v[1 : len(v)-1]
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, errors.New("If-Match must be a non-negative integer")
	}
	if n < 0 {
		return 0, errors.New("If-Match must be a non-negative integer")
	}
	return n, nil
}
