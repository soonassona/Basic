//go:build integration

// Package integration owns end-to-end tests that boot real PG +
// RabbitMQ via testcontainers and exercise the full submit → broker →
// callback round-trip through the production-shaped router. Cheaper
// per-component contracts already live in:
//
//   internal/infrastructure/queue       — broker topology
//   internal/infrastructure/repo        — sqlc/pgx adapters
//   internal/interfaces/http            — handler/auth/contract tests
//
// This file is the only place where everything boots together.
package integration_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/visionloop/api/internal/application"
	"github.com/visionloop/api/internal/domain"
	"github.com/visionloop/api/internal/infrastructure/eventbus"
	"github.com/visionloop/api/internal/infrastructure/queue"
	"github.com/visionloop/api/internal/infrastructure/repo"
	httpapi "github.com/visionloop/api/internal/interfaces/http"
)

const (
	callbackToken = "e2e-callback-token-of-at-least-32-bytes-long"
	callbackHMAC  = "e2e-callback-hmac-of-at-least-32-bytes-padding"
)

// stack is the bag of resources e2e tests close at end of run.
type stack struct {
	pool   *pgxpool.Pool
	mq     *queue.Connection
	hub    *eventbus.MemoryHub
	router http.Handler

	// Test-only: a sessions stub + caller info so we don't need
	// Better Auth wired up here. Real auth is exercised by the
	// auth package's tests.
	caller domain.Caller
}

// startStack boots PG + RabbitMQ, applies migrations, seeds an org +
// user + image, and returns a router built from real repos + real
// queue.Publisher + real eventbus.
func startStack(ctx context.Context, t *testing.T) (*stack, uuid.UUID) {
	t.Helper()

	pool := startPostgres(ctx, t)
	rabbitURL := startRabbit(ctx, t)

	mq, err := queue.Dial(ctx, rabbitURL, 10*time.Second, 5*time.Second)
	if err != nil {
		t.Fatalf("dial rabbit: %v", err)
	}
	t.Cleanup(func() { _ = mq.Close() })

	orgID, userID, imgID := seedTenant(t, pool)

	hub := eventbus.NewMemoryHub()
	caller := domain.Caller{UserID: userID, OrgID: orgID, Role: domain.RoleAnnotator}

	router := httpapi.NewRouter(httpapi.RouterDeps{
		Logger:                slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Pool:                  pool,
		Sessions:              constSessions{userID: userID},
		Users:                 constUsers{userID: userID, orgID: orgID, role: domain.RoleAnnotator},
		ImagesRepo:            repo.NewImageRepo(pool),
		JobsRepo:              repo.NewJobRepo(pool),
		JobPublisher:          queue.NewJobPublisherAdapter(queue.NewAMQPPublisher(mq)),
		JobEvents:             hub,
		AnnotationsRepo:       repo.NewAnnotationRepo(pool),
		AnnotationSetsRepo:    repo.NewAnnotationRepo(pool),
		Storage:               nopStorage{},
		Audit:                 repo.NewAuditRecorder(pool),
		WebOrigin:             "http://localhost:3000",
		PresignTTL:             15 * time.Minute,
		StartedAt:             time.Now(),
		JobCallbackToken:      callbackToken,
		JobCallbackHMACSecret: callbackHMAC,
	})

	return &stack{pool: pool, mq: mq, hub: hub, router: router, caller: caller}, imgID
}

// ---- the test --------------------------------------------------------

func TestE2E_Submit_Consume_Callback_Updates_DB_And_SSE(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	s, imgID := startStack(ctx, t)

	// 1. Submit a job through the real router.
	body, _ := json.Marshal(map[string]any{
		"image_id": imgID.String(),
		"type":     "auto",
		"payload":  map[string]any{"score": 1},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/jobs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "better-auth.session_token", Value: "valid-token"})
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("submit: %d body=%s", w.Code, w.Body.String())
	}
	var subRes map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &subRes)
	jobID := subRes["id"].(string)
	t.Logf("submitted job %s", jobID)

	// 2. Subscribe to the SSE hub before posting the callback so we
	// can assert the publish reached subscribers.
	jobUUID := uuid.MustParse(jobID)
	evCh, unsub := s.hub.Subscribe(ctx, jobUUID)
	defer unsub()

	// 3. Drain a delivery from jobs.pending — proves the API published.
	consumeURL := s.mq.Channel()
	deliveries, err := consumeURL.Consume(queue.QueuePending, "e2e-consumer", false, false, false, false, nil)
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	select {
	case d := <-deliveries:
		if d.RoutingKey != "jobs.pending.auto" {
			t.Fatalf("routing key: %q", d.RoutingKey)
		}
		var env map[string]any
		if err := json.Unmarshal(d.Body, &env); err != nil {
			t.Fatalf("envelope: %v", err)
		}
		if env["job_id"] != jobID {
			t.Fatalf("envelope job_id: %v want %s", env["job_id"], jobID)
		}
		_ = d.Ack(false)
	case <-time.After(15 * time.Second):
		t.Fatal("timed out waiting for jobs.pending delivery")
	}

	// 4. Post a worker callback (running) — like the AI worker would.
	postCallback(t, s.router, jobID, map[string]any{"state": "running", "attempt": 0})

	// 5. Subscriber must see the running event from the hub.
	waitForEvent(t, evCh, "running", 5*time.Second)

	// 6. Post terminal callback (succeeded). Assert DB row reflects it.
	postCallback(t, s.router, jobID, map[string]any{"state": "succeeded", "attempt": 0})
	waitForEvent(t, evCh, "succeeded", 5*time.Second)

	// 7. GET /v1/jobs/:id should now return state=succeeded.
	getReq := httptest.NewRequest(http.MethodGet, "/v1/jobs/"+jobID, nil)
	getReq.AddCookie(&http.Cookie{Name: "better-auth.session_token", Value: "valid-token"})
	gw := httptest.NewRecorder()
	s.router.ServeHTTP(gw, getReq)
	if gw.Code != http.StatusOK {
		t.Fatalf("get: %d", gw.Code)
	}
	var got map[string]any
	_ = json.Unmarshal(gw.Body.Bytes(), &got)
	if got["state"] != "succeeded" {
		t.Fatalf("final DB state: %v", got["state"])
	}
}

// ---- helpers ---------------------------------------------------------

func postCallback(t *testing.T, h http.Handler, jobID string, body map[string]any) {
	t.Helper()
	b, _ := json.Marshal(body)
	r := httptest.NewRequest(http.MethodPost, "/internal/jobs/callback", bytes.NewReader(b))
	mac := hmac.New(sha256.New, []byte(callbackHMAC))
	mac.Write([]byte(jobID))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+callbackToken)
	r.Header.Set("X-Job-ID", jobID)
	r.Header.Set("X-Job-Signature", hex.EncodeToString(mac.Sum(nil)))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("callback %s: %d body=%s", body["state"], w.Code, w.Body.String())
	}
}

func waitForEvent(t *testing.T, ch <-chan application.JobEvent, wantState string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case ev := <-ch:
			if ev.State == wantState {
				return
			}
		case <-time.After(100 * time.Millisecond):
		}
	}
	t.Fatalf("timed out waiting for hub event state=%s", wantState)
}

// silence unused import vet warnings on amqp / sync if they appear in
// future edits but not in the body above.
var _ = amqp.Persistent
var _ sync.Mutex
