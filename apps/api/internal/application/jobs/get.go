package jobs

import (
	"context"

	"github.com/google/uuid"

	"github.com/visionloop/api/internal/application"
	"github.com/visionloop/api/internal/domain"
)

// GetJob returns a single job scoped to the caller's organisation. The
// 404-on-cross-tenant rule is enforced in the SQL (org_id WHERE), not
// here — a cross-tenant id surfaces as domain.ErrNotFound, which the
// handler renders as 404 (no enumeration leak per spec §9).
type GetJob struct {
	Jobs application.JobRepository
}

type GetJobInput struct {
	Caller domain.Caller
	JobID  uuid.UUID
}

func (u GetJob) Execute(ctx context.Context, in GetJobInput) (domain.Job, error) {
	return u.Jobs.Get(ctx, in.JobID, in.Caller.OrgID)
}
