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
