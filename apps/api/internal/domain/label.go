package domain

import (
	"time"

	"github.com/google/uuid"
)

// Label is the studio's annotation category. Org-scoped (spec §9 RBAC),
// soft-archived rather than deleted so historical annotations keep
// resolving via label_id even after a label is retired.
type Label struct {
	ID          uuid.UUID
	OrgID       uuid.UUID
	Name        string
	Color       string // 7-char hex like "#4a8ff5"
	Description string
	Archived    bool
	CreatedAt   time.Time
}
