package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/visionloop/api/internal/application"
)

type MeHandlers struct {
	Users application.UserDirectory
}

// Me — GET /v1/me. The web app uses this to confirm session/membership
// resolved correctly and to populate the dashboard greeting.
func (h *MeHandlers) Me(c *gin.Context) {
	caller, ok := callerFrom(c)
	if !ok {
		abortJSON(c, http.StatusUnauthorized, "unauthorized", "missing caller")
		return
	}
	user, err := h.Users.GetUser(c.Request.Context(), caller.UserID)
	if err != nil {
		s, code := httpStatusFor(err)
		abortJSON(c, s, code, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"user": gin.H{
			"id":             user.ID,
			"email":          user.Email,
			"display_name":   user.DisplayName,
			"email_verified": user.EmailVerified,
			"locale":         user.Locale,
		},
		"membership": gin.H{
			"org_id": caller.OrgID,
			"role":   string(caller.Role),
		},
	})
}
