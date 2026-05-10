package httpapi

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/visionloop/api/internal/application/labels"
	"github.com/visionloop/api/internal/domain"
)

type LabelHandlers struct {
	List labels.ListLabels
}

type labelDTO struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Color       string    `json:"color"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

func toLabelDTO(l domain.Label) labelDTO {
	return labelDTO{
		ID: l.ID, Name: l.Name, Color: l.Color, Description: l.Description, CreatedAt: l.CreatedAt,
	}
}

// ListLabels — GET /v1/labels. Returns the org's non-archived labels for
// the studio picker (spec §10). Read access for all authenticated roles.
func (h *LabelHandlers) ListLabels(c *gin.Context) {
	caller, ok := callerFrom(c)
	if !ok {
		abortJSON(c, http.StatusUnauthorized, "unauthorized", "missing caller")
		return
	}
	out, err := h.List.Execute(c.Request.Context(), labels.ListLabelsInput{Caller: caller})
	if err != nil {
		status, code := httpStatusFor(err)
		abortJSON(c, status, code, err.Error())
		return
	}
	items := make([]labelDTO, 0, len(out.Items))
	for _, l := range out.Items {
		items = append(items, toLabelDTO(l))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}
