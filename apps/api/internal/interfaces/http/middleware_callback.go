package httpapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// JobCallbackAuth verifies a worker callback authenticates with both
// (a) the shared bearer token and (b) a per-job HMAC over the job_id
// path parameter, as ADR-0004 requires. The handler binds at a route
// like /internal/jobs/:id/callback OR receives job_id in the body —
// we accept the latter via the X-Job-ID header so the auth check
// completes before we parse JSON (denial-of-service hardening).
//
// Header contract:
//
//	Authorization: Bearer <JOB_CALLBACK_TOKEN>
//	X-Job-ID:      <uuid>
//	X-Job-Signature: <hex(HMAC-SHA256(JOB_CALLBACK_HMAC_SECRET, job_id))>
//
// Compares both with constant time. On any mismatch the response is a
// generic 401 — no leakage of which check failed.
func JobCallbackAuth(token, hmacSecret string) gin.HandlerFunc {
	tokenBytes := []byte(token)
	secretBytes := []byte(hmacSecret)
	return func(c *gin.Context) {
		auth := c.GetHeader("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			abortJSON(c, http.StatusUnauthorized, "unauthorized", "missing bearer token")
			return
		}
		got := []byte(strings.TrimSpace(strings.TrimPrefix(auth, "Bearer ")))
		if subtle.ConstantTimeCompare(got, tokenBytes) != 1 {
			abortJSON(c, http.StatusUnauthorized, "unauthorized", "bad bearer")
			return
		}

		jobID := strings.TrimSpace(c.GetHeader("X-Job-ID"))
		sigHex := strings.TrimSpace(c.GetHeader("X-Job-Signature"))
		if jobID == "" || sigHex == "" {
			abortJSON(c, http.StatusUnauthorized, "unauthorized", "missing job signature")
			return
		}
		sig, err := hex.DecodeString(sigHex)
		if err != nil {
			abortJSON(c, http.StatusUnauthorized, "unauthorized", "bad signature encoding")
			return
		}
		mac := hmac.New(sha256.New, secretBytes)
		mac.Write([]byte(jobID))
		want := mac.Sum(nil)
		if subtle.ConstantTimeCompare(sig, want) != 1 {
			abortJSON(c, http.StatusUnauthorized, "unauthorized", "bad signature")
			return
		}

		c.Set("vl.callback.job_id", jobID)
		c.Next()
	}
}
