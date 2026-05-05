package jobs

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/visionloop/api/internal/application"
	"github.com/visionloop/api/internal/domain"
)

// ApplyCallback persists a worker callback and fans the new state out
// to SSE subscribers. Idempotent — replaying a terminal-state callback
// returns ErrConflict, which the handler maps to 200 + "no_change" so
// the worker's retry loop converges without churn.
type ApplyCallback struct {
	Jobs   application.JobRepository
	Hub    application.JobEventHub
	Audit  application.AuditRecorder
	Clock  application.Clock
}

type ApplyCallbackInput struct {
	JobID   uuid.UUID
	State   domain.JobState
	Attempt int16
	Error   string
	Result  []byte // raw JSON; canonicalised by the worker
}

type ApplyCallbackOutput struct {
	Job        domain.Job
	NoChange   bool // true iff the row was already terminal (idempotent replay)
}

// validCallbackStates is the set of states a worker is allowed to
// report. `pending` and `cancelled` are excluded — pending is never a
// transition into, and cancellation is owned by user actions, not the
// worker.
var validCallbackStates = map[domain.JobState]struct{}{
	domain.JobRunning:   {},
	domain.JobSucceeded: {},
	domain.JobFailed:    {},
}

func (u ApplyCallback) Execute(ctx context.Context, in ApplyCallbackInput) (ApplyCallbackOutput, error) {
	if _, ok := validCallbackStates[in.State]; !ok {
		return ApplyCallbackOutput{}, fmt.Errorf("%w: state %q is not a valid callback transition", domain.ErrInvalidInput, in.State)
	}

	persisted, err := u.Jobs.ApplyCallback(ctx, application.JobCallback{
		ID:      in.JobID,
		State:   in.State,
		Attempt: in.Attempt,
		Error:   in.Error,
		Result:  in.Result,
	})
	if err != nil {
		if errors.Is(err, domain.ErrConflict) {
			// Already terminal — return the existing row so the worker
			// can confirm convergence without retrying.
			return ApplyCallbackOutput{Job: persisted, NoChange: true}, nil
		}
		return ApplyCallbackOutput{}, err
	}

	u.Hub.Publish(persisted.ID, application.JobEvent{
		JobID:     persisted.ID,
		State:     string(persisted.State),
		Attempt:   persisted.Attempt,
		Error:     persisted.Error,
		UpdatedAt: u.Clock.Now(),
	})

	orgID := persisted.OrgID
	jobID := persisted.ID
	_ = u.Audit.Record(ctx, application.AuditEntry{
		OrgID:      &orgID,
		ActorKind:  "worker",
		Action:     "job.callback",
		Resource:   "job",
		ResourceID: &jobID,
		Metadata: map[string]any{
			"state":   string(persisted.State),
			"attempt": persisted.Attempt,
		},
	})

	return ApplyCallbackOutput{Job: persisted}, nil
}
