package repo

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/visionloop/api/internal/application"
)

// AuditRecorder appends to audit_log. The table is INSERT-only at the
// application role; UPDATE and DELETE are revoked in migration 000002.
type AuditRecorder struct {
	pool *pgxpool.Pool
}

func NewAuditRecorder(pool *pgxpool.Pool) *AuditRecorder {
	return &AuditRecorder{pool: pool}
}

func (r *AuditRecorder) Record(ctx context.Context, e application.AuditEntry) error {
	meta, err := json.Marshal(e.Metadata)
	if err != nil {
		meta = []byte(`{}`)
	}

	const q = `
INSERT INTO audit_log (org_id, actor_id, actor_kind, action, resource, resource_id, metadata, request_id, trace_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, ''), NULLIF($9, ''))`

	_, err = r.pool.Exec(ctx, q,
		nullUUID(e.OrgID),
		nullUUID(e.ActorID),
		e.ActorKind,
		e.Action,
		e.Resource,
		nullUUID(e.ResourceID),
		meta,
		e.RequestID,
		e.TraceID,
	)
	if err != nil {
		return fmt.Errorf("audit insert: %w", err)
	}
	return nil
}

func nullUUID(u *uuid.UUID) any {
	if u == nil {
		return nil
	}
	return *u
}
