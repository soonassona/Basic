// Command api is the VisionLoop API gateway entrypoint.
package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/visionloop/api/internal/config"
	"github.com/visionloop/api/internal/infrastructure/auth"
	"github.com/visionloop/api/internal/infrastructure/db"
	"github.com/visionloop/api/internal/infrastructure/eventbus"
	"github.com/visionloop/api/internal/infrastructure/queue"
	"github.com/visionloop/api/internal/infrastructure/repo"
	"github.com/visionloop/api/internal/infrastructure/storage"
	httpapi "github.com/visionloop/api/internal/interfaces/http"
	"github.com/visionloop/api/internal/observability"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		// Fail fast — section 18: "validate at service startup, fail fast on mismatch."
		_, _ = os.Stderr.WriteString("config error: " + err.Error() + "\n")
		os.Exit(1)
	}

	logger := observability.NewLogger(cfg.LogLevel, cfg.ServiceName)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Best-effort migrations on boot in non-prod. Production deploys run
	// `migrate up` as a pre-deploy hook, but having this here makes
	// `go run ./cmd/api` work against a fresh database.
	if cfg.Env != "production" {
		if mDir := findMigrationsDir(); mDir != "" {
			if err := db.MigrateUp(cfg.PostgresURL, mDir); err != nil {
				logger.Warn("migration failed (continuing)", "error", err)
			}
		}
	}

	pool, err := db.Connect(ctx, cfg.PostgresURL)
	if err != nil {
		logger.Error("postgres connect failed", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	mq, err := queue.Dial(ctx, cfg.RabbitMQURL, cfg.RabbitMQDialTimeout, cfg.RabbitMQPublishTimeout)
	if err != nil {
		// Fail fast (spec §18). Topology declaration runs inside Dial,
		// so a mismatch with an existing broker definition surfaces here.
		logger.Error("rabbitmq dial/declare failed", "error", err)
		os.Exit(1)
	}
	defer func() { _ = mq.Close() }()
	logger.Info("queue topology declared",
		"exchanges", []string{queue.ExchangeJobs, queue.ExchangeJobsDLX},
		"queues", []string{queue.QueuePending, queue.QueueRetry, queue.QueueDead},
	)
	jobPublisher := queue.NewJobPublisherAdapter(queue.NewAMQPPublisher(mq))
	jobEvents := eventbus.NewMemoryHub()

	r2, err := storage.New(ctx, storage.Options{
		Endpoint:        cfg.S3Endpoint,
		Region:          cfg.S3Region,
		AccessKeyID:     cfg.S3AccessKeyID,
		SecretAccessKey: cfg.S3SecretAccessKey,
		Bucket:          cfg.S3Bucket,
		ForcePathStyle:  cfg.S3ForcePathStyle,
	})
	if err != nil {
		logger.Error("storage init failed", "error", err)
		os.Exit(1)
	}

	router := httpapi.NewRouter(httpapi.RouterDeps{
		Logger:       logger,
		Pool:         pool,
		Sessions:     auth.NewSessionLookup(pool, cfg.BetterAuthSecret),
		Users:        repo.NewUserDirectory(pool),
		ImagesRepo:   repo.NewImageRepo(pool),
		JobsRepo:     repo.NewJobRepo(pool),
		JobPublisher: jobPublisher,
		AnnotationsRepo:    repo.NewAnnotationRepo(pool),
		AnnotationSetsRepo: repo.NewAnnotationRepo(pool),
		LabelsRepo:         repo.NewLabelRepo(pool),
		JobEvents:             jobEvents,
		JobCallbackToken:      cfg.JobCallbackToken,
		JobCallbackHMACSecret: cfg.JobCallbackHMACSecret,
		Storage:      r2,
		Audit:        repo.NewAuditRecorder(pool),
		WebOrigin:    cfg.WebOrigin,
		PresignTTL:   cfg.S3PresignExpiry,
		StartedAt:    time.Now().UTC(),
	})

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		logger.Info("api listening", "addr", cfg.HTTPAddr, "env", cfg.Env)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server failed", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down")
	shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdown); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
	}
}

// findMigrationsDir walks up from the executable to locate the
// internal/db/migrations directory, so `go run` from any cwd works.
func findMigrationsDir() string {
	candidates := []string{
		"internal/db/migrations",
		"apps/api/internal/db/migrations",
		"../internal/db/migrations",
	}
	for _, c := range candidates {
		if abs, err := filepath.Abs(c); err == nil {
			if info, err := os.Stat(abs); err == nil && info.IsDir() {
				return abs
			}
		}
	}
	return ""
}
