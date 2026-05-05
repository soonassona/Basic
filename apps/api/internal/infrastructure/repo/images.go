// Package repo adapts sqlc-generated queries to the application ports
// declared in internal/application.
package repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/visionloop/api/internal/domain"
)

// ImageRepo is a thin adapter over the sqlc Queries that translates between
// the persistence model and the domain entity. Multi-tenant scoping by
// org_id lives in the SQL itself (ADR-0002).
type ImageRepo struct {
	pool *pgxpool.Pool
}

func NewImageRepo(pool *pgxpool.Pool) *ImageRepo {
	return &ImageRepo{pool: pool}
}

// imageRow mirrors the columns from internal/db/queries/images.sql.
type imageRow struct {
	ID          uuid.UUID
	OrgID       uuid.UUID
	UploadedBy  uuid.UUID
	StorageKey  string
	StorageEtag *string
	ContentType string
	ByteSize    int64
	Width       *int32
	Height      *int32
	SHA256      *string
	Status      string
	Metadata    []byte
	CreatedAt   pgtype.Timestamptz
}

func (r imageRow) toDomain() domain.Image {
	out := domain.Image{
		ID:          r.ID,
		OrgID:       r.OrgID,
		UploadedBy:  r.UploadedBy,
		StorageKey:  r.StorageKey,
		ContentType: r.ContentType,
		ByteSize:    r.ByteSize,
		Status:      domain.ImageStatus(r.Status),
	}
	if r.Width != nil {
		out.Width = *r.Width
	}
	if r.Height != nil {
		out.Height = *r.Height
	}
	if r.SHA256 != nil {
		out.SHA256 = *r.SHA256
	}
	if r.CreatedAt.Valid {
		out.CreatedAt = r.CreatedAt.Time
	}
	return out
}

func (r *ImageRepo) Create(ctx context.Context, img domain.Image) (domain.Image, error) {
	const q = `
INSERT INTO images (id, org_id, uploaded_by, storage_key, content_type, byte_size, status, metadata)
VALUES ($1, $2, $3, $4, $5, $6, 'uploading', $7)
RETURNING id, org_id, uploaded_by, storage_key, storage_etag, content_type, byte_size,
          width, height, sha256, status, metadata, created_at`
	row := imageRow{}
	err := r.pool.QueryRow(ctx, q,
		img.ID, img.OrgID, img.UploadedBy, img.StorageKey,
		img.ContentType, img.ByteSize, json.RawMessage(`{}`),
	).Scan(
		&row.ID, &row.OrgID, &row.UploadedBy, &row.StorageKey, &row.StorageEtag,
		&row.ContentType, &row.ByteSize, &row.Width, &row.Height, &row.SHA256,
		&row.Status, &row.Metadata, &row.CreatedAt,
	)
	if err != nil {
		return domain.Image{}, fmt.Errorf("insert image: %w", err)
	}
	return row.toDomain(), nil
}

func (r *ImageRepo) Finalize(ctx context.Context, id, orgID uuid.UUID, etag string, w, h int32, sha256 string) (domain.Image, error) {
	const q = `
UPDATE images
SET status = 'ready', storage_etag = $3, width = $4, height = $5, sha256 = $6, updated_at = now()
WHERE id = $1 AND org_id = $2 AND deleted_at IS NULL
RETURNING id, org_id, uploaded_by, storage_key, storage_etag, content_type, byte_size,
          width, height, sha256, status, metadata, created_at`
	row := imageRow{}
	err := r.pool.QueryRow(ctx, q, id, orgID, etag, w, h, sha256).Scan(
		&row.ID, &row.OrgID, &row.UploadedBy, &row.StorageKey, &row.StorageEtag,
		&row.ContentType, &row.ByteSize, &row.Width, &row.Height, &row.SHA256,
		&row.Status, &row.Metadata, &row.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Image{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Image{}, fmt.Errorf("finalize image: %w", err)
	}
	return row.toDomain(), nil
}

func (r *ImageRepo) Get(ctx context.Context, id, orgID uuid.UUID) (domain.Image, error) {
	const q = `
SELECT id, org_id, uploaded_by, storage_key, storage_etag, content_type, byte_size,
       width, height, sha256, status, metadata, created_at
FROM images
WHERE id = $1 AND org_id = $2 AND deleted_at IS NULL`
	row := imageRow{}
	err := r.pool.QueryRow(ctx, q, id, orgID).Scan(
		&row.ID, &row.OrgID, &row.UploadedBy, &row.StorageKey, &row.StorageEtag,
		&row.ContentType, &row.ByteSize, &row.Width, &row.Height, &row.SHA256,
		&row.Status, &row.Metadata, &row.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Image{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Image{}, fmt.Errorf("get image: %w", err)
	}
	return row.toDomain(), nil
}

func (r *ImageRepo) List(ctx context.Context, orgID uuid.UUID, status domain.ImageStatus, limit, offset int32) ([]domain.Image, int64, error) {
	listQ := `
SELECT id, org_id, uploaded_by, storage_key, storage_etag, content_type, byte_size,
       width, height, sha256, status, metadata, created_at
FROM images
WHERE org_id = $1 AND deleted_at IS NULL
  AND ($2::image_status IS NULL OR status = $2)
ORDER BY created_at DESC
LIMIT $3 OFFSET $4`
	countQ := `
SELECT COUNT(*) FROM images
WHERE org_id = $1 AND deleted_at IS NULL
  AND ($2::image_status IS NULL OR status = $2)`

	var statusArg any
	if status != "" {
		statusArg = string(status)
	}

	rows, err := r.pool.Query(ctx, listQ, orgID, statusArg, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list images: %w", err)
	}
	defer rows.Close()

	out := make([]domain.Image, 0, limit)
	for rows.Next() {
		var row imageRow
		if err := rows.Scan(
			&row.ID, &row.OrgID, &row.UploadedBy, &row.StorageKey, &row.StorageEtag,
			&row.ContentType, &row.ByteSize, &row.Width, &row.Height, &row.SHA256,
			&row.Status, &row.Metadata, &row.CreatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan image: %w", err)
		}
		out = append(out, row.toDomain())
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	var total int64
	if err := r.pool.QueryRow(ctx, countQ, orgID, statusArg).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count images: %w", err)
	}
	return out, total, nil
}
