//go:build integration || pact

// Shared test helpers for the integration package. Both the
// testcontainers e2e (`integration` tag) and the Pact provider
// verification (`pact` tag) need the same Postgres+RabbitMQ container
// boot and the same constSessions/constUsers/nopStorage stubs, so they
// live here under a combined build constraint.
package integration_test

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/visionloop/api/internal/application"
	"github.com/visionloop/api/internal/domain"
	"github.com/visionloop/api/internal/infrastructure/db"
)

func startPostgres(ctx context.Context, t *testing.T) *pgxpool.Pool {
	t.Helper()
	req := testcontainers.ContainerRequest{
		Image:        "pgvector/pgvector:pg17",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_DB": "visionloop", "POSTGRES_USER": "visionloop", "POSTGRES_PASSWORD": "visionloop",
		},
		WaitingFor: wait.ForListeningPort("5432/tcp").WithStartupTimeout(60 * time.Second),
	}
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{ContainerRequest: req, Started: true})
	if err != nil {
		t.Fatalf("pg: %v", err)
	}
	t.Cleanup(func() { _ = c.Terminate(context.Background()) })
	host, _ := c.Host(ctx)
	port, _ := c.MappedPort(ctx, "5432/tcp")
	url := fmt.Sprintf("postgres://visionloop:visionloop@%s:%s/visionloop?sslmode=disable", host, port.Port())

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if err := db.MigrateUp(url, migrationsAbs(t)); err == nil {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func startRabbit(ctx context.Context, t *testing.T) string {
	t.Helper()
	req := testcontainers.ContainerRequest{
		Image:        "rabbitmq:4.0-alpine",
		ExposedPorts: []string{"5672/tcp"},
		WaitingFor: wait.ForAll(
			wait.ForLog("Server startup complete"),
			wait.ForListeningPort("5672/tcp"),
		).WithDeadline(60 * time.Second),
	}
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{ContainerRequest: req, Started: true})
	if err != nil {
		t.Fatalf("rabbit: %v", err)
	}
	t.Cleanup(func() { _ = c.Terminate(context.Background()) })
	host, _ := c.Host(ctx)
	port, _ := c.MappedPort(ctx, "5672/tcp")
	return "amqp://guest:guest@" + host + ":" + port.Port() + "/"
}

func migrationsAbs(t *testing.T) string {
	_, this, _, _ := runtime.Caller(0)
	abs, err := filepath.Abs(filepath.Join(filepath.Dir(this), "..", "db", "migrations"))
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	return abs
}

func seedTenant(t *testing.T, pool *pgxpool.Pool) (uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	orgID, userID, imgID := uuid.New(), uuid.New(), uuid.New()

	if _, err := pool.Exec(ctx,
		`INSERT INTO organizations (id, slug, name, plan, cors_allowlist) VALUES ($1, $2, 'E2E', 'free', '{}')`,
		orgID, "e2e-"+orgID.String()[:8]); err != nil {
		t.Fatalf("org: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, email_verified, display_name, locale) VALUES ($1, $2, true, 'E2E', 'en')`,
		userID, userID.String()+"@e2e"); err != nil {
		t.Fatalf("user: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO memberships (org_id, user_id, role) VALUES ($1, $2, 'annotator')`, orgID, userID); err != nil {
		t.Fatalf("membership: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO images (id, org_id, uploaded_by, storage_key, content_type, byte_size, status)
		 VALUES ($1, $2, $3, $4, 'image/jpeg', 1024, 'ready')`,
		imgID, orgID, userID, "orgs/"+orgID.String()+"/images/"+imgID.String()+".jpg"); err != nil {
		t.Fatalf("image: %v", err)
	}
	return orgID, userID, imgID
}

type constSessions struct{ userID uuid.UUID }

func (c constSessions) LookupSession(_ context.Context, token string) (uuid.UUID, time.Time, error) {
	if token != "valid-token" {
		return uuid.Nil, time.Time{}, domain.ErrUnauthorized
	}
	return c.userID, time.Now().Add(time.Hour), nil
}

type constUsers struct {
	userID uuid.UUID
	orgID  uuid.UUID
	role   domain.Role
}

func (c constUsers) GetUser(_ context.Context, _ uuid.UUID) (domain.User, error) {
	return domain.User{ID: c.userID, Email: "e2e", DisplayName: "e2e", Locale: "en"}, nil
}
func (c constUsers) PrimaryMembership(_ context.Context, _ uuid.UUID) (domain.Membership, error) {
	return domain.Membership{OrgID: c.orgID, UserID: c.userID, Role: c.role}, nil
}

type nopStorage struct{}

func (nopStorage) PresignPut(_ context.Context, _, _ string, _ int64, _ time.Duration) (application.PresignedURL, error) {
	return application.PresignedURL{}, nil
}
func (nopStorage) HeadObject(_ context.Context, _ string) (application.ObjectInfo, error) {
	return application.ObjectInfo{}, domain.ErrNotFound
}
