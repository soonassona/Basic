//go:build integration

package repo_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/visionloop/api/internal/domain"
	"github.com/visionloop/api/internal/infrastructure/repo"
)

// seedSetAndAnnotation pre-creates an annotation_set + a single bbox
// annotation, returning everything tests need.
func seedSetAndAnnotation(t *testing.T, pool *pgxpool.Pool) (orgID, userID, imgID, setID, annID uuid.UUID, version int64) {
	t.Helper()
	orgID, userID, imgID = seedOrgAndUser(t, pool)

	setID = uuid.New()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO annotation_sets (id, org_id, image_id, version, created_by) VALUES ($1, $2, $3, 1, $4)`,
		setID, orgID, imgID, userID)
	if err != nil {
		t.Fatalf("seed set: %v", err)
	}

	annID = uuid.New()
	_, err = pool.Exec(context.Background(),
		`INSERT INTO annotations (id, org_id, annotation_set_id, kind, geometry, created_by)
		 VALUES ($1, $2, $3, 'bbox', $4::jsonb, $5)`,
		annID, orgID, setID, `{"type":"bbox","coords":[0,0,1,1]}`, userID)
	if err != nil {
		t.Fatalf("seed annotation: %v", err)
	}
	return orgID, userID, imgID, setID, annID, 1
}

func TestAnnotationRepo_Patch_HappyPath(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	pool := startPostgres(ctx, t)
	r := repo.NewAnnotationRepo(pool)
	orgID, _, _, _, annID, version := seedSetAndAnnotation(t, pool)

	accepted := true
	out, newVersion, err := r.Patch(ctx, annID, orgID, version, domain.AnnotationPatch{
		Geometry:      []byte(`{"type":"bbox","coords":[1,1,2,2]}`),
		HumanAccepted: &accepted,
	})
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	if newVersion != version+1 {
		t.Fatalf("new_version: got %d want %d", newVersion, version+1)
	}
	if out.HumanAccepted == nil || !*out.HumanAccepted {
		t.Fatalf("human_accepted: %v", out.HumanAccepted)
	}
}

func TestAnnotationRepo_Patch_StaleIfMatch_ReturnsCurrent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	pool := startPostgres(ctx, t)
	r := repo.NewAnnotationRepo(pool)
	orgID, _, _, _, annID, version := seedSetAndAnnotation(t, pool)

	// First patch advances to v2.
	accepted := true
	if _, _, err := r.Patch(ctx, annID, orgID, version, domain.AnnotationPatch{HumanAccepted: &accepted}); err != nil {
		t.Fatalf("first patch: %v", err)
	}

	// Second patch with the now-stale version should 409 with current=2.
	_, current, err := r.Patch(ctx, annID, orgID, version, domain.AnnotationPatch{HumanAccepted: &accepted})
	if err == nil || err.Error() != domain.ErrConflict.Error() {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
	if current != 2 {
		t.Fatalf("current_version: got %d want 2", current)
	}
}

func TestAnnotationRepo_Patch_CrossOrg_NotFound(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	pool := startPostgres(ctx, t)
	r := repo.NewAnnotationRepo(pool)
	_, _, _, _, annID, version := seedSetAndAnnotation(t, pool)

	otherOrg := uuid.New()
	_, _, err := r.Patch(ctx, annID, otherOrg, version, domain.AnnotationPatch{
		HumanAccepted: ptrBool(true),
	})
	if err == nil || err.Error() != domain.ErrNotFound.Error() {
		t.Fatalf("expected ErrNotFound (no enumeration), got %v", err)
	}
}

// TestAnnotationRepo_Patch_ConcurrentWriters_OneWins is the contract
// the optimistic-lock guard exists for. N goroutines all patch with
// If-Match=v1; exactly one bumps the version, the rest see ErrConflict.
func TestAnnotationRepo_Patch_ConcurrentWriters_OneWins(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	pool := startPostgres(ctx, t)
	r := repo.NewAnnotationRepo(pool)
	orgID, _, _, _, annID, version := seedSetAndAnnotation(t, pool)

	const N = 8
	var wg sync.WaitGroup
	var success, conflicts, other int64
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			accepted := true
			_, _, err := r.Patch(ctx, annID, orgID, version, domain.AnnotationPatch{HumanAccepted: &accepted})
			switch {
			case err == nil:
				atomic.AddInt64(&success, 1)
			case err.Error() == domain.ErrConflict.Error():
				atomic.AddInt64(&conflicts, 1)
			default:
				atomic.AddInt64(&other, 1)
				t.Errorf("unexpected: %v", err)
			}
		}()
	}
	wg.Wait()
	if success != 1 {
		t.Fatalf("success: got %d want 1", success)
	}
	if conflicts != N-1 {
		t.Fatalf("conflicts: got %d want %d", conflicts, N-1)
	}
	if other != 0 {
		t.Fatalf("other errors: %d", other)
	}
}

func ptrBool(b bool) *bool { return &b }
