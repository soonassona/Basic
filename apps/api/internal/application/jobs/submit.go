// Package jobs holds the use-cases backing /v1/jobs.
package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/visionloop/api/internal/application"
	"github.com/visionloop/api/internal/domain"
	"github.com/visionloop/api/internal/observability"
)

// DedupWindow is the 10-minute look-back from spec §11 — duplicate
// submissions within this period return the same job_id.
const DedupWindow = 10 * time.Minute

// JobsExchange + RoutingPrefix mirror queue.ExchangeJobs and the
// `jobs.pending.{type}` shape declared by topology.Declare. They live
// here as constants instead of an import so the application layer stays
// free of infrastructure imports (ADR-0001).
const (
	JobsExchange  = "jobs"
	RoutingPrefix = "jobs.pending."
)

// SubmitJob turns a validated request into a persisted job row plus a
// queue message. Behaviour:
//
//  1. Domain-validate the job type and image state.
//  2. Canonicalise the payload (RFC 8785) and compute the dedup hash.
//  3. Look up an existing job in the 10-minute window (spec §11). On
//     hit, return it with Replayed=true — no new row, no new publish.
//  4. Insert. If the partial unique index trips
//     (domain.ErrJobDuplicateActive), re-read and treat as a replay.
//  5. Publish to jobs exchange with routing key jobs.pending.{type}.
//  6. On publish failure post-commit: mark the row as failed, increment
//     the Prometheus counter, and return ErrPublishFailed so the handler
//     can render a 503 with Retry-After (spec §11 reliability).
//  7. Audit job.submitted | job.replayed.
type SubmitJob struct {
	Jobs      application.JobRepository
	Images    application.ImageRepository
	Publisher application.JobPublisher
	Audit     application.AuditRecorder
	Clock     application.Clock
}

type SubmitJobInput struct {
	Caller  domain.Caller
	ImageID uuid.UUID
	Type    domain.JobType
	Payload []byte // raw JSON; canonicalised inside Execute
}

type SubmitJobOutput struct {
	Job      domain.Job
	Replayed bool
}

// ErrPublishFailed is returned when the row is committed but the
// downstream publish fails. The handler renders 503 + Retry-After.
var ErrPublishFailed = errors.New("submit job: publish failed after commit")

func (u SubmitJob) Execute(ctx context.Context, in SubmitJobInput) (SubmitJobOutput, error) {
	img, err := u.Images.Get(ctx, in.ImageID, in.Caller.OrgID)
	if err != nil {
		// Map to ErrNotFound so the handler returns 404 — never 403 here,
		// to avoid leaking the existence of cross-tenant images (spec §9).
		if errors.Is(err, domain.ErrNotFound) {
			return SubmitJobOutput{}, domain.ErrNotFound
		}
		return SubmitJobOutput{}, fmt.Errorf("load image: %w", err)
	}
	if err := domain.ValidateJobSubmission(in.Type, img); err != nil {
		return SubmitJobOutput{}, err
	}

	canonical, err := domain.CanonicalisePayload(in.Payload)
	if err != nil {
		return SubmitJobOutput{}, err
	}
	imgID := img.ID
	dedupKey := domain.ComputeDedupKey(in.Caller.OrgID, &imgID, in.Type, canonical)

	now := u.Clock.Now()
	since := now.Add(-DedupWindow)

	// Step 3 — explicit window lookup before insert keeps the common case
	// cheap (one read instead of insert-then-recover-from-conflict).
	if existing, err := u.Jobs.FindActiveByDedupKey(ctx, in.Caller.OrgID, dedupKey, since); err == nil {
		u.recordAudit(ctx, in.Caller, existing.ID, "job.replayed", canonical)
		return SubmitJobOutput{Job: existing, Replayed: true}, nil
	} else if !errors.Is(err, domain.ErrNotFound) {
		return SubmitJobOutput{}, fmt.Errorf("dedup lookup: %w", err)
	}

	// Step 4 — race recovery. Two callers past the lookup may both reach
	// Create; the second hits the partial unique index and surfaces as
	// ErrJobDuplicateActive. Re-read and return the winning row.
	job := domain.Job{
		ID:          uuid.Must(uuid.NewRandom()),
		OrgID:       in.Caller.OrgID,
		ImageID:     &imgID,
		SubmittedBy: in.Caller.UserID,
		Type:        in.Type,
		State:       domain.JobPending,
		Payload:     canonical,
		DedupKey:    dedupKey,
	}
	persisted, err := u.Jobs.Create(ctx, job)
	if err != nil {
		if errors.Is(err, domain.ErrJobDuplicateActive) {
			existing, lookupErr := u.Jobs.FindActiveByDedupKey(ctx, in.Caller.OrgID, dedupKey, since)
			if lookupErr != nil {
				return SubmitJobOutput{}, fmt.Errorf("recover dedup race: %w", lookupErr)
			}
			u.recordAudit(ctx, in.Caller, existing.ID, "job.replayed", canonical)
			return SubmitJobOutput{Job: existing, Replayed: true}, nil
		}
		return SubmitJobOutput{}, fmt.Errorf("persist job: %w", err)
	}

	// Step 5 — publish.
	body, err := buildEnvelope(persisted)
	if err != nil {
		return SubmitJobOutput{}, fmt.Errorf("envelope: %w", err)
	}
	routingKey := RoutingPrefix + string(persisted.Type)
	headers := map[string]any{
		"x-job-id":     persisted.ID.String(),
		"x-org-id":     persisted.OrgID.String(),
		"x-attempt":    int32(persisted.Attempt),
		"x-dedup-key":  persisted.DedupKey,
		"content-type": "application/json",
	}

	if pubErr := u.Publisher.Publish(ctx, JobsExchange, routingKey, body, headers); pubErr != nil {
		// Step 6 — record the failure on the row + counter, then surface
		// ErrPublishFailed so the handler returns 503 + Retry-After. A
		// reconciler that resurrects orphan failed rows is a Phase 6
		// reliability concern (deferred per ADR-0004 appendix).
		_ = u.Jobs.MarkErrored(ctx, persisted.ID, persisted.OrgID, "publish_failed")
		observability.JobPublishFailures.WithLabelValues(persisted.OrgID.String(), "publish_failed").Inc()
		return SubmitJobOutput{Job: persisted, Replayed: false}, fmt.Errorf("%w: %v", ErrPublishFailed, pubErr)
	}

	u.recordAudit(ctx, in.Caller, persisted.ID, "job.submitted", canonical)
	return SubmitJobOutput{Job: persisted, Replayed: false}, nil
}

func (u SubmitJob) recordAudit(ctx context.Context, caller domain.Caller, jobID uuid.UUID, action string, canonical []byte) {
	orgID := caller.OrgID
	userID := caller.UserID
	_ = u.Audit.Record(ctx, application.AuditEntry{
		OrgID:      &orgID,
		ActorID:    &userID,
		ActorKind:  "user",
		Action:     action,
		Resource:   "job",
		ResourceID: &jobID,
		Metadata: map[string]any{
			"payload_bytes": len(canonical),
		},
	})
}

// buildEnvelope is the wire format consumed by the AI service. Keep the
// schema additive: workers older than the API must still be able to
// parse a newer envelope.
func buildEnvelope(j domain.Job) ([]byte, error) {
	env := struct {
		JobID    uuid.UUID       `json:"job_id"`
		OrgID    uuid.UUID       `json:"org_id"`
		ImageID  *uuid.UUID      `json:"image_id"`
		Type     domain.JobType  `json:"type"`
		Attempt  int16           `json:"attempt"`
		DedupKey string          `json:"dedup_key"`
		Payload  json.RawMessage `json:"payload"`
	}{
		JobID:    j.ID,
		OrgID:    j.OrgID,
		ImageID:  j.ImageID,
		Type:     j.Type,
		Attempt:  j.Attempt,
		DedupKey: j.DedupKey,
		Payload:  j.Payload,
	}
	return json.Marshal(env)
}
