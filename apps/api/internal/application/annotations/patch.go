// Package annotations holds use-cases for /v1/annotations/*.
package annotations

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/visionloop/api/internal/application"
	"github.com/visionloop/api/internal/domain"
)

// PatchAnnotation enforces the optimistic-locking contract from spec
// §10. The repository owns the atomic check-and-bump; this use-case
// adds RBAC + audit + a no-op guard.
type PatchAnnotation struct {
	Annotations application.AnnotationRepository
	Audit       application.AuditRecorder
}

type PatchAnnotationInput struct {
	Caller  domain.Caller
	ID      uuid.UUID
	IfMatch int64
	Patch   domain.AnnotationPatch
}

type PatchAnnotationOutput struct {
	Annotation domain.Annotation
	NewVersion int64
}

func (u PatchAnnotation) Execute(ctx context.Context, in PatchAnnotationInput) (PatchAnnotationOutput, error) {
	if !in.Caller.Role.CanWriteImages() {
		return PatchAnnotationOutput{}, fmt.Errorf("%w: viewers cannot edit annotations", domain.ErrForbidden)
	}
	if !in.Patch.HasChanges() {
		return PatchAnnotationOutput{}, fmt.Errorf("%w: at least one of geometry, label_id, human_accepted must be supplied", domain.ErrInvalidInput)
	}

	ann, version, err := u.Annotations.Patch(ctx, in.ID, in.Caller.OrgID, in.IfMatch, in.Patch)
	if err != nil {
		// Conflict: bubble up with the current version stuffed into the
		// returned output so the handler can echo it in the 409 body.
		if errors.Is(err, domain.ErrConflict) {
			return PatchAnnotationOutput{NewVersion: version}, err
		}
		return PatchAnnotationOutput{}, err
	}

	orgID := in.Caller.OrgID
	userID := in.Caller.UserID
	annID := ann.ID
	_ = u.Audit.Record(ctx, application.AuditEntry{
		OrgID:      &orgID,
		ActorID:    &userID,
		ActorKind:  "user",
		Action:     "annotation.patched",
		Resource:   "annotation",
		ResourceID: &annID,
		Metadata: map[string]any{
			"new_version": version,
		},
	})

	return PatchAnnotationOutput{Annotation: ann, NewVersion: version}, nil
}
