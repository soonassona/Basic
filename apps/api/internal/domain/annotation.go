package domain

import (
	"time"

	"github.com/google/uuid"
)

// AnnotationSet groups annotations for a single image. Version is the
// optimistic-lock token (spec §10).
type AnnotationSet struct {
	ID        uuid.UUID
	OrgID     uuid.UUID
	ImageID   uuid.UUID
	Version   int64
	Notes     string
	CreatedBy uuid.UUID
	CreatedAt time.Time
	UpdatedAt time.Time
}

// AnnotationKind mirrors the annotation_kind Postgres enum.
type AnnotationKind string

const (
	AnnotationBBox    AnnotationKind = "bbox"
	AnnotationPolygon AnnotationKind = "polygon"
	AnnotationMask    AnnotationKind = "mask"
	AnnotationPoint   AnnotationKind = "point"
)

// Annotation is the in-memory projection of a row from the annotations
// table. Geometry is stored as raw JSON (GeoJSON-ish) — the studio
// owns the schema; the API just round-trips it.
type Annotation struct {
	ID              uuid.UUID
	OrgID           uuid.UUID
	AnnotationSetID uuid.UUID
	LabelID         *uuid.UUID
	Kind            AnnotationKind
	Geometry        []byte
	MaskStorageKey  string
	AIScore         *float64
	QualityScore    *float64
	ModelUsed       string
	HumanAccepted   *bool
	CreatedBy       uuid.UUID
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// AnnotationPatch is the partial-update payload used by PATCH
// /v1/annotations/:id. A nil pointer means "field not provided" — we
// don't model an explicit-null tri-state in v1; clearing a field is a
// separate operation.
type AnnotationPatch struct {
	Geometry      []byte     // nil = not provided; non-nil JSON = update
	LabelID       *uuid.UUID // nil = not provided
	HumanAccepted *bool      // nil = not provided
}

// HasChanges returns false if the patch would be a no-op.
func (p AnnotationPatch) HasChanges() bool {
	return p.Geometry != nil || p.LabelID != nil || p.HumanAccepted != nil
}
