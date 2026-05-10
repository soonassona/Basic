package annotations

import (
	"context"
	"errors"
	"fmt"

	"github.com/visionloop/api/internal/application"
	"github.com/visionloop/api/internal/domain"
)

// CreateAnnotation implements POST /v1/annotations. Same optimistic-locking
// flow as PatchAnnotation: the repository owns the atomic check-and-bump,
// this use-case adds RBAC + audit + input validation.
type CreateAnnotation struct {
	Annotations application.AnnotationRepository
	Audit       application.AuditRecorder
}

type CreateAnnotationInput struct {
	Caller  domain.Caller
	IfMatch int64
	Create  domain.AnnotationCreate
}

type CreateAnnotationOutput struct {
	Annotation domain.Annotation
	NewVersion int64
}

func (u CreateAnnotation) Execute(ctx context.Context, in CreateAnnotationInput) (CreateAnnotationOutput, error) {
	if !in.Caller.Role.CanWriteImages() {
		return CreateAnnotationOutput{}, fmt.Errorf("%w: viewers cannot create annotations", domain.ErrForbidden)
	}
	if err := in.Create.Validate(); err != nil {
		return CreateAnnotationOutput{}, err
	}

	ann, version, err := u.Annotations.Create(ctx, in.Caller.OrgID, in.Caller.UserID, in.IfMatch, in.Create)
	if err != nil {
		if errors.Is(err, domain.ErrConflict) {
			return CreateAnnotationOutput{NewVersion: version}, err
		}
		return CreateAnnotationOutput{}, err
	}

	orgID := in.Caller.OrgID
	userID := in.Caller.UserID
	annID := ann.ID
	_ = u.Audit.Record(ctx, application.AuditEntry{
		OrgID:      &orgID,
		ActorID:    &userID,
		ActorKind:  "user",
		Action:     "annotation.created",
		Resource:   "annotation",
		ResourceID: &annID,
		Metadata: map[string]any{
			"new_version": version,
			"kind":        string(ann.Kind),
		},
	})

	return CreateAnnotationOutput{Annotation: ann, NewVersion: version}, nil
}
