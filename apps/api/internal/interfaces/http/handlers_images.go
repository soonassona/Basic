package httpapi

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/visionloop/api/internal/application/images"
	"github.com/visionloop/api/internal/domain"
)

type ImageHandlers struct {
	Presign  images.PresignUpload
	Finalize images.FinalizeUpload
	List     images.ListImages
	Get      images.GetImage
}

type presignRequest struct {
	ContentType string `json:"content_type" binding:"required"`
	ByteSize    int64  `json:"byte_size"    binding:"required,gt=0"`
}

type presignResponse struct {
	Image  imageDTO    `json:"image"`
	Upload presignDTO  `json:"upload"`
}

type imageDTO struct {
	ID          uuid.UUID `json:"id"`
	OrgID       uuid.UUID `json:"org_id"`
	Status      string    `json:"status"`
	StorageKey  string    `json:"storage_key"`
	ContentType string    `json:"content_type"`
	ByteSize    int64     `json:"byte_size"`
	Width       int32     `json:"width,omitempty"`
	Height      int32     `json:"height,omitempty"`
}

type presignDTO struct {
	URL     string            `json:"url"`
	Method  string            `json:"method"`
	Headers map[string]string `json:"headers"`
	Expires string            `json:"expires_at"`
}

func toImageDTO(img domain.Image) imageDTO {
	return imageDTO{
		ID:          img.ID,
		OrgID:       img.OrgID,
		Status:      string(img.Status),
		StorageKey:  img.StorageKey,
		ContentType: img.ContentType,
		ByteSize:    img.ByteSize,
		Width:       img.Width,
		Height:      img.Height,
	}
}

// PresignUpload — POST /v1/images:presign
func (h *ImageHandlers) PresignUpload(c *gin.Context) {
	caller, ok := callerFrom(c)
	if !ok {
		abortJSON(c, http.StatusUnauthorized, "unauthorized", "missing caller")
		return
	}
	if !caller.Role.CanWriteImages() {
		abortJSON(c, http.StatusForbidden, "forbidden", "viewer cannot upload")
		return
	}

	var req presignRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		abortJSON(c, http.StatusBadRequest, "invalid_input", err.Error())
		return
	}

	out, err := h.Presign.Execute(c.Request.Context(), images.PresignUploadInput{
		Caller:      caller,
		ContentType: req.ContentType,
		ByteSize:    req.ByteSize,
	})
	if err != nil {
		status, code := httpStatusFor(err)
		abortJSON(c, status, code, err.Error())
		return
	}

	c.JSON(http.StatusCreated, presignResponse{
		Image: toImageDTO(out.Image),
		Upload: presignDTO{
			URL:     out.Upload.URL,
			Method:  out.Upload.Method,
			Headers: out.Upload.Headers,
			Expires: out.Upload.Expires.UTC().Format("2006-01-02T15:04:05Z"),
		},
	})
}

type finalizeRequest struct {
	Width  int32  `json:"width"  binding:"required,gt=0"`
	Height int32  `json:"height" binding:"required,gt=0"`
	SHA256 string `json:"sha256"`
}

// FinalizeUpload — POST /v1/images/:id:confirm
func (h *ImageHandlers) FinalizeUpload(c *gin.Context) {
	caller, ok := callerFrom(c)
	if !ok {
		abortJSON(c, http.StatusUnauthorized, "unauthorized", "missing caller")
		return
	}
	if !caller.Role.CanWriteImages() {
		abortJSON(c, http.StatusForbidden, "forbidden", "viewer cannot upload")
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		abortJSON(c, http.StatusBadRequest, "invalid_input", "bad image id")
		return
	}
	var req finalizeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		abortJSON(c, http.StatusBadRequest, "invalid_input", err.Error())
		return
	}

	final, err := h.Finalize.Execute(c.Request.Context(), images.FinalizeUploadInput{
		Caller:  caller,
		ImageID: id,
		Width:   req.Width,
		Height:  req.Height,
		SHA256:  req.SHA256,
	})
	if err != nil {
		status, code := httpStatusFor(err)
		abortJSON(c, status, code, err.Error())
		return
	}
	c.Header("ETag", `"1"`) // annotation_set version is 1 on creation; reserved for Phase 4
	c.JSON(http.StatusOK, gin.H{"image": toImageDTO(final)})
}

type listImagesResponse struct {
	Items  []imageDTO `json:"items"`
	Total  int64      `json:"total"`
	Limit  int32      `json:"limit"`
	Offset int32      `json:"offset"`
}

// ListImages — GET /v1/images
func (h *ImageHandlers) ListImages(c *gin.Context) {
	caller, ok := callerFrom(c)
	if !ok {
		abortJSON(c, http.StatusUnauthorized, "unauthorized", "missing caller")
		return
	}
	limit := parseInt32(c.Query("limit"), 50)
	offset := parseInt32(c.Query("offset"), 0)
	status := domain.ImageStatus(c.Query("status"))

	out, err := h.List.Execute(c.Request.Context(), images.ListImagesInput{
		Caller: caller,
		Status: status,
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		s, code := httpStatusFor(err)
		abortJSON(c, s, code, err.Error())
		return
	}

	items := make([]imageDTO, 0, len(out.Items))
	for _, i := range out.Items {
		items = append(items, toImageDTO(i))
	}
	c.JSON(http.StatusOK, listImagesResponse{
		Items: items, Total: out.Total, Limit: limit, Offset: offset,
	})
}

// GetImage — GET /v1/images/:id
//
// Returns the image record + a freshly-minted presigned download URL the
// studio canvas (spec §10) loads directly from R2/MinIO. TTL matches the
// upload presign window so storage signing config has a single source.
func (h *ImageHandlers) GetImage(c *gin.Context) {
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

	out, err := h.Get.Execute(c.Request.Context(), images.GetImageInput{
		Caller: caller, ID: id,
	})
	if err != nil {
		s, code := httpStatusFor(err)
		abortJSON(c, s, code, err.Error())
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"image": toImageDTO(out.Image),
		"download": presignDTO{
			URL:     out.DownloadURL.URL,
			Method:  out.DownloadURL.Method,
			Headers: out.DownloadURL.Headers,
			Expires: out.DownloadURL.Expires.UTC().Format("2006-01-02T15:04:05Z07:00"),
		},
	})
}

func parseInt32(s string, fallback int32) int32 {
	if s == "" {
		return fallback
	}
	n, err := strconv.ParseInt(s, 10, 32)
	if err != nil {
		return fallback
	}
	return int32(n)
}
