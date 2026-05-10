// Package labels holds use-cases for /v1/labels.
package labels

import (
	"context"

	"github.com/visionloop/api/internal/application"
	"github.com/visionloop/api/internal/domain"
)

// ListLabels returns the org-scoped label catalog for the studio's
// label picker (spec §10). Read access is allowed to all roles.
type ListLabels struct {
	Labels application.LabelRepository
}

type ListLabelsInput struct {
	Caller domain.Caller
}

type ListLabelsOutput struct {
	Items []domain.Label
}

func (u ListLabels) Execute(ctx context.Context, in ListLabelsInput) (ListLabelsOutput, error) {
	items, err := u.Labels.ListByOrg(ctx, in.Caller.OrgID)
	if err != nil {
		return ListLabelsOutput{}, err
	}
	return ListLabelsOutput{Items: items}, nil
}
