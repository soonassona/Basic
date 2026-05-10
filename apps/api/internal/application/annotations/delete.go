package annotations

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/visionloop/api/internal/application"
	"github.com/visionloop/api/internal/domain"
)

// DeleteAnnotation implements DELETE /v1/annotations/:id. Soft-delete only —
// the row stays in the database for audit + retraining purposes (spec §10
// + §11), it just stops appearing in GET responses.
type DeleteAnnotation struct {
	Annotations application.AnnotationRepository
	Audit       application.AuditRecorder
}

type DeleteAnnotationInput struct {
	Caller  domain.Caller
	ID      uuid.UUID
	IfMatch int64
}

type DeleteAnnotationOutput struct {
	NewVersion int64
}

func (u DeleteAnnotation) Execute(ctx context.Context, in DeleteAnnotationInput) (DeleteAnnotationOutput, error) {
	if !in.Caller.Role.CanWriteImages() {
		return DeleteAnnotationOutput{}, fmt.Errorf("%w: viewers cannot delete annotations", domain.ErrForbidden)
	}

	version, err := u.Annotations.SoftDelete(ctx, in.ID, in.Caller.OrgID, in.IfMatch)
	if err != nil {
		if errors.Is(err, domain.ErrConflict) {
			return DeleteAnnotationOutput{NewVersion: version}, err
		}
		return DeleteAnnotationOutput{}, err
	}

	orgID := in.Caller.OrgID
	userID := in.Caller.UserID
	annID := in.ID
	_ = u.Audit.Record(ctx, application.AuditEntry{
		OrgID:      &orgID,
		ActorID:    &userID,
		ActorKind:  "user",
		Action:     "annotation.deleted",
		Resource:   "annotation",
		ResourceID: &annID,
		Metadata: map[string]any{
			"new_version": version,
		},
	})

	return DeleteAnnotationOutput{NewVersion: version}, nil
}
