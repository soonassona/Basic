package repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/visionloop/api/internal/application"
	"github.com/visionloop/api/internal/db/sqlc"
	"github.com/visionloop/api/internal/domain"
)

// AnnotationRepo implements application.AnnotationRepository.
//
// Patch runs three queries inside a single transaction so the
// version-check + version-bump + row-update are atomic. Repeatable
// Read isolation isn't necessary — the WHERE version = $ predicate on
// the set update is the safety net (a concurrent writer that already
// bumped the version will return zero rows from BumpAnnotationSetVersion,
// which we map to ErrConflict).
type AnnotationRepo struct {
	pool *pgxpool.Pool
}

func NewAnnotationRepo(pool *pgxpool.Pool) *AnnotationRepo {
	return &AnnotationRepo{pool: pool}
}

func (r *AnnotationRepo) Patch(ctx context.Context, id, orgID uuid.UUID, ifMatch int64, patch domain.AnnotationPatch) (domain.Annotation, int64, error) {
	var (
		out        domain.Annotation
		newVersion int64
	)

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return out, 0, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := sqlc.New(tx)

	current, err := q.GetAnnotationWithSetVersion(ctx, sqlc.GetAnnotationWithSetVersionParams{ID: id, OrgID: orgID})
	if errors.Is(err, pgx.ErrNoRows) {
		return out, 0, domain.ErrNotFound
	}
	if err != nil {
		return out, 0, fmt.Errorf("read annotation: %w", err)
	}

	if current.SetVersion != ifMatch {
		// Stale ETag — return current version so the handler can render
		// it in the 409 body.
		return out, current.SetVersion, domain.ErrConflict
	}

	bumped, err := q.BumpAnnotationSetVersion(ctx, sqlc.BumpAnnotationSetVersionParams{
		ID:      current.AnnotationSetID,
		OrgID:   orgID,
		Version: ifMatch,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// Race: another writer bumped between the read and our update.
		// Re-read to get the actual current version for the 409 body.
		latest, gerr := q.GetAnnotationWithSetVersion(ctx, sqlc.GetAnnotationWithSetVersionParams{ID: id, OrgID: orgID})
		if gerr != nil {
			return out, 0, fmt.Errorf("post-conflict reread: %w", gerr)
		}
		return out, latest.SetVersion, domain.ErrConflict
	}
	if err != nil {
		return out, 0, fmt.Errorf("bump set version: %w", err)
	}
	newVersion = bumped

	row, err := q.ApplyAnnotationPatch(ctx, sqlc.ApplyAnnotationPatchParams{
		ID:            id,
		OrgID:         orgID,
		Column3:       patch.Geometry,
		LabelID:       toPGUUID(patch.LabelID),
		HumanAccepted: patch.HumanAccepted,
	})
	if err != nil {
		return out, 0, fmt.Errorf("apply annotation patch: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return out, 0, fmt.Errorf("commit annotation patch: %w", err)
	}

	out = domain.Annotation{
		ID:              row.ID,
		OrgID:           row.OrgID,
		AnnotationSetID: row.AnnotationSetID,
		LabelID:         fromPGUUID(row.LabelID),
		Kind:            domain.AnnotationKind(row.Kind),
		Geometry:        row.Geometry,
		AIScore:         row.AiScore,
		QualityScore:    row.QualityScore,
		HumanAccepted:   row.HumanAccepted,
		CreatedBy:       row.CreatedBy,
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
	}
	if row.MaskStorageKey != nil {
		out.MaskStorageKey = *row.MaskStorageKey
	}
	if row.ModelUsed != nil {
		out.ModelUsed = *row.ModelUsed
	}
	return out, newVersion, nil
}

// GetByImage returns the set + annotations for an image. Tenant scope
// is enforced by org_id appearing in both queries' WHERE.
func (r *AnnotationRepo) GetByImage(ctx context.Context, imageID, orgID uuid.UUID) (domain.AnnotationSet, []domain.Annotation, error) {
	q := sqlc.New(r.pool)

	srow, err := q.GetAnnotationSetByImage(ctx, sqlc.GetAnnotationSetByImageParams{
		ImageID: imageID, OrgID: orgID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AnnotationSet{}, nil, domain.ErrNotFound
	}
	if err != nil {
		return domain.AnnotationSet{}, nil, fmt.Errorf("get annotation set: %w", err)
	}

	set := domain.AnnotationSet{
		ID:        srow.ID,
		OrgID:     srow.OrgID,
		ImageID:   srow.ImageID,
		Version:   srow.Version,
		CreatedBy: srow.CreatedBy,
		CreatedAt: srow.CreatedAt,
		UpdatedAt: srow.UpdatedAt,
	}
	if srow.Notes != nil {
		set.Notes = *srow.Notes
	}

	rows, err := q.ListAnnotationsBySet(ctx, sqlc.ListAnnotationsBySetParams{
		AnnotationSetID: set.ID, OrgID: orgID,
	})
	if err != nil {
		return set, nil, fmt.Errorf("list annotations: %w", err)
	}

	out := make([]domain.Annotation, 0, len(rows))
	for _, r := range rows {
		ann := domain.Annotation{
			ID:              r.ID,
			OrgID:           r.OrgID,
			AnnotationSetID: r.AnnotationSetID,
			LabelID:         fromPGUUID(r.LabelID),
			Kind:            domain.AnnotationKind(r.Kind),
			Geometry:        r.Geometry,
			AIScore:         r.AiScore,
			QualityScore:    r.QualityScore,
			HumanAccepted:   r.HumanAccepted,
			CreatedBy:       r.CreatedBy,
			CreatedAt:       r.CreatedAt,
			UpdatedAt:       r.UpdatedAt,
		}
		if r.MaskStorageKey != nil {
			ann.MaskStorageKey = *r.MaskStorageKey
		}
		if r.ModelUsed != nil {
			ann.ModelUsed = *r.ModelUsed
		}
		out = append(out, ann)
	}
	return set, out, nil
}

// Create inserts a new annotation atomically with a parent-set version
// bump. Mirrors Patch's locking flow: read current set version → 409 on
// mismatch → bump → insert → commit.
func (r *AnnotationRepo) Create(
	ctx context.Context,
	orgID, createdBy uuid.UUID,
	ifMatch int64,
	in domain.AnnotationCreate,
) (domain.Annotation, int64, error) {
	var (
		out        domain.Annotation
		newVersion int64
	)

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return out, 0, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := sqlc.New(tx)

	// Atomically check + bump the parent set's version. On no-rows, we
	// disambiguate "set missing in this org" (404) from "stale If-Match"
	// (409) by re-reading the set by id.
	bumped, err := q.BumpAnnotationSetVersion(ctx, sqlc.BumpAnnotationSetVersionParams{
		ID:      in.AnnotationSetID,
		OrgID:   orgID,
		Version: ifMatch,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		current, gerr := q.GetAnnotationSetByID(ctx, sqlc.GetAnnotationSetByIDParams{
			ID: in.AnnotationSetID, OrgID: orgID,
		})
		if errors.Is(gerr, pgx.ErrNoRows) {
			return out, 0, domain.ErrNotFound
		}
		if gerr != nil {
			return out, 0, fmt.Errorf("post-conflict reread: %w", gerr)
		}
		return out, current.Version, domain.ErrConflict
	}
	if err != nil {
		return out, 0, fmt.Errorf("bump set version: %w", err)
	}
	newVersion = bumped

	row, err := q.CreateAnnotation(ctx, sqlc.CreateAnnotationParams{
		OrgID:           orgID,
		AnnotationSetID: in.AnnotationSetID,
		LabelID:         toPGUUID(in.LabelID),
		Kind:            sqlc.AnnotationKind(in.Kind),
		Column5:         in.Geometry,
		CreatedBy:       createdBy,
	})
	if err != nil {
		return out, 0, fmt.Errorf("insert annotation: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return out, 0, fmt.Errorf("commit annotation create: %w", err)
	}

	out = domain.Annotation{
		ID:              row.ID,
		OrgID:           row.OrgID,
		AnnotationSetID: row.AnnotationSetID,
		LabelID:         fromPGUUID(row.LabelID),
		Kind:            domain.AnnotationKind(row.Kind),
		Geometry:        row.Geometry,
		AIScore:         row.AiScore,
		QualityScore:    row.QualityScore,
		HumanAccepted:   row.HumanAccepted,
		CreatedBy:       row.CreatedBy,
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
	}
	if row.MaskStorageKey != nil {
		out.MaskStorageKey = *row.MaskStorageKey
	}
	if row.ModelUsed != nil {
		out.ModelUsed = *row.ModelUsed
	}
	return out, newVersion, nil
}

// SoftDelete marks an annotation deleted and bumps the parent set version
// in the same transaction. Returns the new version on success.
func (r *AnnotationRepo) SoftDelete(
	ctx context.Context,
	id, orgID uuid.UUID,
	ifMatch int64,
) (int64, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := sqlc.New(tx)

	// Look the annotation up first so we can verify it exists, learn its
	// set id, and read the current set version for conflict reporting.
	current, err := q.GetAnnotationWithSetVersion(ctx, sqlc.GetAnnotationWithSetVersionParams{
		ID: id, OrgID: orgID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, domain.ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("read annotation: %w", err)
	}
	if current.SetVersion != ifMatch {
		return current.SetVersion, domain.ErrConflict
	}

	bumped, err := q.BumpAnnotationSetVersion(ctx, sqlc.BumpAnnotationSetVersionParams{
		ID: current.AnnotationSetID, OrgID: orgID, Version: ifMatch,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// Race: someone else bumped between our read and the update.
		latest, gerr := q.GetAnnotationWithSetVersion(ctx, sqlc.GetAnnotationWithSetVersionParams{
			ID: id, OrgID: orgID,
		})
		if gerr != nil {
			return 0, fmt.Errorf("post-conflict reread: %w", gerr)
		}
		return latest.SetVersion, domain.ErrConflict
	}
	if err != nil {
		return 0, fmt.Errorf("bump set version: %w", err)
	}

	if _, err := q.SoftDeleteAnnotation(ctx, sqlc.SoftDeleteAnnotationParams{
		ID: id, OrgID: orgID,
	}); err != nil {
		return 0, fmt.Errorf("soft delete annotation: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit annotation delete: %w", err)
	}
	return bumped, nil
}

// WriteAIResult writes mask_storage_key, ai_score, and model_used onto
// every annotation in the set. Called by the job callback handler when
// state = succeeded (spec §5).
func (r *AnnotationRepo) WriteAIResult(ctx context.Context, in application.AIResultWrite) error {
	q := sqlc.New(r.pool)
	return q.WriteAIResult(ctx, sqlc.WriteAIResultParams{
		AnnotationSetID: in.AnnotationSetID,
		OrgID:           in.OrgID,
		MaskStorageKey:  in.MaskStorageKey,
		AiScore:         in.AiScore,
		ModelUsed:       in.ModelUsed,
	})
}

// silence pgtype import on go vet when the only consumer is via
// helpers in jobs.go (which share the file); keeping a reference here
// is harmless and self-documenting.
var _ = pgtype.UUID{}
