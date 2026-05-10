// Package httpapi assembles middleware and handlers into a Gin engine.
package httpapi

import (
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/visionloop/api/internal/application"
	"github.com/visionloop/api/internal/application/annotations"
	"github.com/visionloop/api/internal/application/images"
	"github.com/visionloop/api/internal/application/jobs"
	"github.com/visionloop/api/internal/application/labels"
	"github.com/visionloop/api/internal/domain"
)

// ginModeOnce avoids the data race that gin.SetMode triggers when
// NewRouter is invoked from parallel tests.
var ginModeOnce sync.Once

type RouterDeps struct {
	Logger                *slog.Logger
	Pool                  *pgxpool.Pool
	Sessions              application.SessionStore
	Users                 application.UserDirectory
	ImagesRepo            application.ImageRepository
	JobsRepo              application.JobRepository
	JobPublisher          application.JobPublisher
	JobEvents             application.JobEventHub
	AnnotationsRepo       application.AnnotationRepository
	AnnotationSetsRepo    application.AnnotationSetRepository
	LabelsRepo            application.LabelRepository
	Storage               application.ObjectStore
	Audit                 application.AuditRecorder
	WebOrigin             string
	PresignTTL            time.Duration
	StartedAt             time.Time
	JobCallbackToken      string
	JobCallbackHMACSecret string
}

// NewRouter wires the engine. Order of middleware mirrors section 9.
func NewRouter(d RouterDeps) *gin.Engine {
	ginModeOnce.Do(func() { gin.SetMode(gin.ReleaseMode) })
	r := gin.New()
	r.HandleMethodNotAllowed = true
	r.Use(gin.Recovery())
	r.Use(RequestID())
	r.Use(AccessLog(d.Logger))

	corsCfg := cors.Config{
		AllowOrigins:     []string{d.WebOrigin},
		AllowMethods:     []string{"GET", "POST", "PATCH", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Authorization", "Content-Type", "If-Match", "X-Request-ID"},
		ExposeHeaders:    []string{"ETag", "X-Request-ID"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}
	r.Use(cors.New(corsCfg))

	// Security headers — section 9.
	r.Use(func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains; preload")
		c.Next()
	})

	health := &HealthHandlers{Pool: d.Pool, StartedAt: d.StartedAt}
	r.GET("/healthz", health.Liveness)
	r.GET("/readyz", health.Readiness)

	imageHandlers := &ImageHandlers{
		Presign: images.PresignUpload{
			Images: d.ImagesRepo, Storage: d.Storage, Audit: d.Audit,
			PresignTTL: d.PresignTTL, Clock: application.SystemClock{},
		},
		Finalize: images.FinalizeUpload{
			Images: d.ImagesRepo, Storage: d.Storage, Audit: d.Audit,
		},
		List: images.ListImages{Images: d.ImagesRepo},
		Get: images.GetImage{
			Images: d.ImagesRepo, Storage: d.Storage, PresignTTL: d.PresignTTL,
		},
	}
	me := &MeHandlers{Users: d.Users}

	jobHandlers := &JobHandlers{
		Submit: jobs.SubmitJob{
			Jobs: d.JobsRepo, Images: d.ImagesRepo, Publisher: d.JobPublisher,
			Audit: d.Audit, Clock: application.SystemClock{},
		},
		Get: jobs.GetJob{Jobs: d.JobsRepo},
		Callback: jobs.ApplyCallback{
			Jobs: d.JobsRepo, Hub: d.JobEvents, Audit: d.Audit, Clock: application.SystemClock{},
		},
		Hub: d.JobEvents,
	}

	annHandlers := &AnnotationHandlers{
		Patch:  annotations.PatchAnnotation{Annotations: d.AnnotationsRepo, Audit: d.Audit},
		Create: annotations.CreateAnnotation{Annotations: d.AnnotationsRepo, Audit: d.Audit},
		Delete: annotations.DeleteAnnotation{Annotations: d.AnnotationsRepo, Audit: d.Audit},
		GetSet: annotations.GetAnnotationSet{Sets: d.AnnotationSetsRepo},
	}

	var labelHandlers *LabelHandlers
	if d.LabelsRepo != nil {
		labelHandlers = &LabelHandlers{
			List: labels.ListLabels{Labels: d.LabelsRepo},
		}
	}

	v1 := r.Group("/v1")
	v1.Use(AuthRequired(d.Sessions, d.Users))
	{
		v1.GET("/me", me.Me)

		// Phase 1 limits writes to roles that can author images.
		v1.GET("/images", imageHandlers.ListImages)
		v1.GET("/images/:id", imageHandlers.GetImage)
		v1.POST("/images:presign",
			RequireRole(domain.RoleOwner, domain.RoleAdmin, domain.RoleAnnotator),
			imageHandlers.PresignUpload)
		v1.POST("/images/:id/confirm",
			RequireRole(domain.RoleOwner, domain.RoleAdmin, domain.RoleAnnotator),
			imageHandlers.FinalizeUpload)

		// Phase 2 — job submission. Annotator and above; viewers cannot
		// kick off inference work (spec §9 RBAC).
		v1.POST("/jobs",
			RequireRole(domain.RoleOwner, domain.RoleAdmin, domain.RoleAnnotator),
			jobHandlers.SubmitJob)

		// Read endpoint — all authenticated roles, including Viewer.
		v1.GET("/jobs/:id", jobHandlers.GetJob)
		v1.GET("/jobs/:id/events", jobHandlers.JobEvents)

		// Optimistic-locked annotation edits (spec §10). Create / patch /
		// delete all share the same If-Match → 409 contract; viewers blocked.
		v1.POST("/annotations",
			RequireRole(domain.RoleOwner, domain.RoleAdmin, domain.RoleAnnotator),
			annHandlers.CreateAnnotation)
		v1.PATCH("/annotations/:id",
			RequireRole(domain.RoleOwner, domain.RoleAdmin, domain.RoleAnnotator),
			annHandlers.PatchAnnotation)
		v1.DELETE("/annotations/:id",
			RequireRole(domain.RoleOwner, domain.RoleAdmin, domain.RoleAnnotator),
			annHandlers.DeleteAnnotation)
		v1.GET("/annotation-sets/:image_id", annHandlers.GetAnnotationSet)

		// Label catalog (spec §10) — read-only for the studio's picker.
		if labelHandlers != nil {
			v1.GET("/labels", labelHandlers.ListLabels)
		}
	}

	// Internal worker callback. NOT under v1 (no session auth) — guarded
	// by HMAC + bearer (ADR-0004). Intentionally outside the v1 group
	// so it can never be matched by a browser-bound auth flow.
	if d.JobCallbackToken != "" && d.JobCallbackHMACSecret != "" {
		internal := r.Group("/internal")
		internal.Use(JobCallbackAuth(d.JobCallbackToken, d.JobCallbackHMACSecret))
		internal.POST("/jobs/callback", jobHandlers.JobCallback)
	}

	r.NoRoute(func(c *gin.Context) {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"code": "not_found", "message": "no such route"}})
	})

	return r
}
