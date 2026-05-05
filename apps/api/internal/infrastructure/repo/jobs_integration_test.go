//go:build integration

package repo_test

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/visionloop/api/internal/domain"
	"github.com/visionloop/api/internal/infrastructure/db"
	"github.com/visionloop/api/internal/infrastructure/repo"
)

// startPostgres boots pgvector/pgvector:pg17 (matches CI), runs all
// migrations from internal/db/migrations, and returns a connected pool.
func startPostgres(ctx context.Context, t *testing.T) *pgxpool.Pool {
	t.Helper()

	req := testcontainers.ContainerRequest{
		Image:        "pgvector/pgvector:pg17",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_DB":       "visionloop",
			"POSTGRES_USER":     "visionloop",
			"POSTGRES_PASSWORD": "visionloop",
		},
		WaitingFor: wait.ForListeningPort("5432/tcp").WithStartupTimeout(60 * time.Second),
	}
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	t.Cleanup(func() { _ = c.Terminate(context.Background()) })

	host, err := c.Host(ctx)
	if err != nil {
		t.Fatalf("host: %v", err)
	}
	port, err := c.MappedPort(ctx, "5432/tcp")
	if err != nil {
		t.Fatalf("port: %v", err)
	}

	url := "postgres://visionloop:visionloop@" + host + ":" + port.Port() + "/visionloop?sslmode=disable"

	// Apply migrations. We poll the DSN because Postgres may accept TCP a
	// fraction of a second before the role is provisioned.
	migrationsDir := migrationsAbs(t)
	deadline := time.Now().Add(30 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		if err := db.MigrateUp(url, migrationsDir); err == nil {
			lastErr = nil
			break
		} else {
			lastErr = err
			time.Sleep(500 * time.Millisecond)
		}
	}
	if lastErr != nil {
		t.Fatalf("migrate up: %v", lastErr)
	}

	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func migrationsAbs(t *testing.T) string {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir, err := filepath.Abs(filepath.Join(filepath.Dir(thisFile), "..", "..", "db", "migrations"))
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	return dir
}

// seedOrgAndUser inserts the minimum referenced rows so jobs FK
// constraints pass. Returns (orgID, userID, imageID).
func seedOrgAndUser(t *testing.T, pool *pgxpool.Pool) (uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := context.Background()

	orgID := uuid.New()
	userID := uuid.New()
	imgID := uuid.New()

	_, err := pool.Exec(ctx,
		`INSERT INTO organizations (id, slug, name, plan, cors_allowlist) VALUES ($1, $2, $3, 'free', '{}')`,
		orgID, "org-"+orgID.String()[:8], "Test Org")
	if err != nil {
		t.Fatalf("seed org: %v", err)
	}
	_, err = pool.Exec(ctx,
		`INSERT INTO users (id, email, email_verified, display_name, locale) VALUES ($1, $2, true, 'Test', 'en')`,
		userID, userID.String()+"@example.com")
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	_, err = pool.Exec(ctx,
		`INSERT INTO memberships (org_id, user_id, role) VALUES ($1, $2, 'owner')`, orgID, userID)
	if err != nil {
		t.Fatalf("seed membership: %v", err)
	}
	_, err = pool.Exec(ctx,
		`INSERT INTO images (id, org_id, uploaded_by, storage_key, content_type, byte_size, status)
		 VALUES ($1, $2, $3, $4, 'image/jpeg', 1024, 'ready')`,
		imgID, orgID, userID, "orgs/"+orgID.String()+"/images/"+imgID.String()+".jpg")
	if err != nil {
		t.Fatalf("seed image: %v", err)
	}
	return orgID, userID, imgID
}

func newJob(orgID, userID, imgID uuid.UUID, dedupKey string) domain.Job {
	id := uuid.New()
	return domain.Job{
		ID: id, OrgID: orgID, ImageID: &imgID, SubmittedBy: userID,
		Type: domain.JobTypeAuto, State: domain.JobPending,
		Payload: []byte(`{}`), DedupKey: dedupKey,
	}
}

// TestJobRepo_DedupRace_AllowsExactlyOneActive proves the partial unique
// index works under concurrent writers. This is the contract the
// SubmitJob use-case relies on for race recovery — fakes can't honestly
// simulate it, so we exercise the real DB behaviour here.
func TestJobRepo_DedupRace_AllowsExactlyOneActive(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	pool := startPostgres(ctx, t)
	r := repo.NewJobRepo(pool)
	orgID, userID, imgID := seedOrgAndUser(t, pool)

	const dedup = "deadbeef00000000000000000000000000000000000000000000000000000000"
	const N = 8

	var wg sync.WaitGroup
	var successes int64
	var dupErrors int64
	var otherErrors int64
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := r.Create(ctx, newJob(orgID, userID, imgID, dedup))
			switch {
			case err == nil:
				atomic.AddInt64(&successes, 1)
			case errors.Is(err, domain.ErrJobDuplicateActive):
				atomic.AddInt64(&dupErrors, 1)
			default:
				atomic.AddInt64(&otherErrors, 1)
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()

	if successes != 1 {
		t.Fatalf("successes: got %d want 1", successes)
	}
	if dupErrors != N-1 {
		t.Fatalf("dup errors: got %d want %d", dupErrors, N-1)
	}
	if otherErrors != 0 {
		t.Fatalf("other errors: %d", otherErrors)
	}
}

// TestJobRepo_FindActiveByDedupKey_RespectsWindow proves the use-case's
// 10-minute policy is honoured by the WHERE created_at >= $since clause.
func TestJobRepo_FindActiveByDedupKey_RespectsWindow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	pool := startPostgres(ctx, t)
	r := repo.NewJobRepo(pool)
	orgID, userID, imgID := seedOrgAndUser(t, pool)

	const dedup = "ca11ab1ec01dca11ab1ec01dca11ab1ec01dca11ab1ec01dca11ab1ec01dca11"
	created, err := r.Create(ctx, newJob(orgID, userID, imgID, dedup))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Within window: found.
	found, err := r.FindActiveByDedupKey(ctx, orgID, dedup, time.Now().Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("find within window: %v", err)
	}
	if found.ID != created.ID {
		t.Fatalf("found wrong row")
	}

	// Outside window (since=now+1h): not found.
	_, err = r.FindActiveByDedupKey(ctx, orgID, dedup, time.Now().Add(1*time.Hour))
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound outside window, got %v", err)
	}
}
