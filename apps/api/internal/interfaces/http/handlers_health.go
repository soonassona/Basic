package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

// HealthHandlers serves liveness and readiness checks. Liveness only returns
// process status; readiness probes Postgres so Kubernetes does not route
// traffic to a pod that lost its database connection.
type HealthHandlers struct {
	Pool      *pgxpool.Pool
	StartedAt time.Time
}

func (h *HealthHandlers) Liveness(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"uptime":  time.Since(h.StartedAt).Seconds(),
		"service": "visionloop-api",
	})
}

func (h *HealthHandlers) Readiness(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	if err := h.Pool.Ping(ctx); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status": "degraded",
			"error":  "postgres_unreachable",
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ready"})
}
