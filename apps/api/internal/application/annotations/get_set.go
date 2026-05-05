package annotations

import (
	"context"

	"github.com/google/uuid"

	"github.com/visionloop/api/internal/application"
	"github.com/visionloop/api/internal/domain"
)

// GetAnnotationSet returns the per-image annotation set + its
// annotations. Read access is allowed to all roles (Viewer included)
// per spec §9.
type GetAnnotationSet struct {
	Sets application.AnnotationSetRepository
}

type GetAnnotationSetInput struct {
	Caller  domain.Caller
	ImageID uuid.UUID
}

type GetAnnotationSetOutput struct {
	Set         domain.AnnotationSet
	Annotations []domain.Annotation
}

func (u GetAnnotationSet) Execute(ctx context.Context, in GetAnnotationSetInput) (GetAnnotationSetOutput, error) {
	set, anns, err := u.Sets.GetByImage(ctx, in.ImageID, in.Caller.OrgID)
	if err != nil {
		return GetAnnotationSetOutput{}, err
	}
	return GetAnnotationSetOutput{Set: set, Annotations: anns}, nil
}
