package repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"

	"github.com/visionloop/api/internal/application"
	"github.com/visionloop/api/internal/db/sqlc"
	"github.com/visionloop/api/internal/domain"
)

// pgUniqueViolation is the PG error code for a unique-constraint failure.
// We map it to domain.ErrJobDuplicateActive only when the offending
// constraint is the dedup partial index, so unrelated unique violations
// (defensive future-proofing) still surface as raw errors.
const (
	pgUniqueViolation     = "23505"
	dedupConstraintName   = "idx_jobs_dedup_window"
	defaultPgConstraintFK = "" // sentinel; constraint may be reported on .ConstraintName
)

// JobRepo adapts sqlc-generated job queries to the application port.
type JobRepo struct {
	pool *pgxpool.Pool
	q    *sqlc.Queries
}

func NewJobRepo(pool *pgxpool.Pool) *JobRepo {
	return &JobRepo{pool: pool, q: sqlc.New(pool)}
}

func (r *JobRepo) Create(ctx context.Context, j domain.Job) (domain.Job, error) {
	row, err := r.q.CreateJob(ctx, sqlc.CreateJobParams{
		ID:          j.ID,
		OrgID:       j.OrgID,
		ImageID:     toPGUUID(j.ImageID),
		SubmittedBy: j.SubmittedBy,
		Type:        sqlc.JobType(j.Type),
		State:       sqlc.JobState(j.State),
		Column7:     j.Payload,
		DedupKey:    j.DedupKey,
		Attempt:     j.Attempt,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation && pgErr.ConstraintName == dedupConstraintName {
			return domain.Job{}, domain.ErrJobDuplicateActive
		}
		return domain.Job{}, fmt.Errorf("insert job: %w", err)
	}
	return jobRowToDomain(row), nil
}

func (r *JobRepo) Get(ctx context.Context, id, orgID uuid.UUID) (domain.Job, error) {
	row, err := r.q.GetJob(ctx, sqlc.GetJobParams{ID: id, OrgID: orgID})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Job{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Job{}, fmt.Errorf("get job: %w", err)
	}
	return jobRowToDomain(row), nil
}

func (r *JobRepo) FindActiveByDedupKey(ctx context.Context, orgID uuid.UUID, dedupKey string, since time.Time) (domain.Job, error) {
	row, err := r.q.FindActiveJobByDedupKey(ctx, sqlc.FindActiveJobByDedupKeyParams{
		OrgID:     orgID,
		DedupKey:  dedupKey,
		CreatedAt: since,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Job{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Job{}, fmt.Errorf("dedup lookup: %w", err)
	}
	return jobRowToDomain(row), nil
}

func (r *JobRepo) ApplyCallback(ctx context.Context, in application.JobCallback) (domain.Job, error) {
	var errPtr *string
	if in.Error != "" {
		s := in.Error
		errPtr = &s
	}
	row, err := r.q.ApplyJobCallback(ctx, sqlc.ApplyJobCallbackParams{
		ID:      in.ID,
		State:   sqlc.JobState(in.State),
		Attempt: in.Attempt,
		Error:   errPtr,
		Result:  in.Result,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// Row is either gone or already terminal. Distinguish via a
		// follow-up read so the use-case can map "missing" → 404 and
		// "terminal" → 409.
		existing, gerr := r.q.GetJobByID(ctx, in.ID)
		if errors.Is(gerr, pgx.ErrNoRows) {
			return domain.Job{}, domain.ErrNotFound
		}
		if gerr != nil {
			return domain.Job{}, fmt.Errorf("post-callback read: %w", gerr)
		}
		return jobRowToDomain(existing), domain.ErrConflict
	}
	if err != nil {
		return domain.Job{}, fmt.Errorf("apply callback: %w", err)
	}
	return jobRowToDomain(row), nil
}

func (r *JobRepo) MarkErrored(ctx context.Context, id, orgID uuid.UUID, errMsg string) error {
	msg := errMsg
	if err := r.q.MarkJobErrored(ctx, sqlc.MarkJobErroredParams{
		ID:    id,
		OrgID: orgID,
		Error: &msg,
	}); err != nil {
		return fmt.Errorf("mark job errored: %w", err)
	}
	return nil
}

func toPGUUID(u *uuid.UUID) pgtype.UUID {
	if u == nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: *u, Valid: true}
}

func fromPGUUID(p pgtype.UUID) *uuid.UUID {
	if !p.Valid {
		return nil
	}
	id := uuid.UUID(p.Bytes)
	return &id
}

func jobRowToDomain(r sqlc.Job) domain.Job {
	out := domain.Job{
		ID:          r.ID,
		OrgID:       r.OrgID,
		ImageID:     fromPGUUID(r.ImageID),
		SubmittedBy: r.SubmittedBy,
		Type:        domain.JobType(r.Type),
		State:       domain.JobState(r.State),
		Payload:     r.Payload,
		DedupKey:    r.DedupKey,
		Attempt:     r.Attempt,
	}
	if r.Error != nil {
		out.Error = *r.Error
	}
	out.CreatedAt = r.CreatedAt.UTC().Format("2006-01-02T15:04:05.999999Z07:00")
	return out
}
