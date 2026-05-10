package repo

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/visionloop/api/internal/db/sqlc"
	"github.com/visionloop/api/internal/domain"
)

// LabelRepo implements application.LabelRepository.
type LabelRepo struct {
	pool *pgxpool.Pool
}

func NewLabelRepo(pool *pgxpool.Pool) *LabelRepo {
	return &LabelRepo{pool: pool}
}

func (r *LabelRepo) ListByOrg(ctx context.Context, orgID uuid.UUID) ([]domain.Label, error) {
	q := sqlc.New(r.pool)
	rows, err := q.ListLabelsByOrg(ctx, orgID)
	if err != nil {
		return nil, fmt.Errorf("list labels: %w", err)
	}
	out := make([]domain.Label, 0, len(rows))
	for _, row := range rows {
		l := domain.Label{
			ID:        row.ID,
			OrgID:     row.OrgID,
			Name:      row.Name,
			Color:     row.Color,
			Archived:  row.Archived,
			CreatedAt: row.CreatedAt,
		}
		if row.Description != nil {
			l.Description = *row.Description
		}
		out = append(out, l)
	}
	return out, nil
}
