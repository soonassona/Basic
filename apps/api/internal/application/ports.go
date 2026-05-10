// Package application contains the use-cases (commands and queries). It
// defines the ports it consumes; infrastructure provides the adapters
// (ADR-0001).
package application

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/visionloop/api/internal/domain"
)

// SessionStore validates a session token and returns the authenticated user
// plus the membership chosen for the active organisation.
type SessionStore interface {
	// LookupSession returns ErrUnauthorized if the token is missing,
	// expired, or otherwise invalid.
	LookupSession(ctx context.Context, token string) (uuid.UUID, time.Time, error)
}

// ObjectStore generates time-limited, scoped upload URLs against R2.
type ObjectStore interface {
	PresignPut(ctx context.Context, key, contentType string, byteSize int64, ttl time.Duration) (PresignedURL, error)
	HeadObject(ctx context.Context, key string) (ObjectInfo, error)
}

type PresignedURL struct {
	URL     string
	Method  string
	Headers map[string]string
	Expires time.Time
}

type ObjectInfo struct {
	ETag        string
	ContentType string
	ByteSize    int64
}

// UserDirectory exposes the read side of the identity domain.
type UserDirectory interface {
	GetUser(ctx context.Context, id uuid.UUID) (domain.User, error)
	PrimaryMembership(ctx context.Context, userID uuid.UUID) (domain.Membership, error)
}

// ImageRepository is the persistence port for the image library.
type ImageRepository interface {
	Create(ctx context.Context, img domain.Image) (domain.Image, error)
	Finalize(ctx context.Context, id, orgID uuid.UUID, etag string, width, height int32, sha256 string) (domain.Image, error)
	Get(ctx context.Context, id, orgID uuid.UUID) (domain.Image, error)
	List(ctx context.Context, orgID uuid.UUID, status domain.ImageStatus, limit, offset int32) ([]domain.Image, int64, error)
}

// JobRepository persists jobs and looks them up by dedup key. The
// 10-minute window from spec §11 is enforced by the repository (passed
// the cutoff time by the use-case) rather than encoded in SQL constants.
type JobRepository interface {
	Create(ctx context.Context, j domain.Job) (domain.Job, error)
	Get(ctx context.Context, id, orgID uuid.UUID) (domain.Job, error)
	FindActiveByDedupKey(ctx context.Context, orgID uuid.UUID, dedupKey string, since time.Time) (domain.Job, error)
	MarkErrored(ctx context.Context, id, orgID uuid.UUID, errMsg string) error
	// ApplyCallback idempotently updates a job from a worker callback.
	// Returns ErrConflict if the row was already in a terminal state
	// (the callback was a duplicate or arrived out of order).
	ApplyCallback(ctx context.Context, in JobCallback) (domain.Job, error)
}

// JobCallback is the input shape for ApplyCallback. Result and Error
// are mutually exclusive in practice but we let the application layer
// enforce that — the repository just stores what it's given.
//
// org_id is intentionally absent: this update is keyed by job id alone
// because the endpoint is HMAC-authenticated. The repository returns
// the persisted row (with its org_id) so the handler can fan out SSE
// and audit on the right tenant.
type JobCallback struct {
	ID      uuid.UUID
	State   domain.JobState
	Attempt int16
	Error   string
	Result  []byte // canonical JSON; nil = NULL
}

// JobPublisher abstracts queue.Publisher so the use-case stays testable
// without a broker. Headers are AMQP-typed but the use-case treats them
// as opaque; only the queue package interprets them.
type JobPublisher interface {
	Publish(ctx context.Context, exchange, routingKey string, body []byte, headers map[string]any) error
}

// JobEventHub fans out job state transitions to SSE subscribers. It's
// in-process for v1 — multi-replica fan-out via Redis Pub/Sub is a
// Phase 8 scaling concern. Handlers Subscribe per job_id; the worker
// callback (next TODO row) Publishes when state changes.
type JobEventHub interface {
	Subscribe(ctx context.Context, jobID uuid.UUID) (<-chan JobEvent, func())
	Publish(jobID uuid.UUID, ev JobEvent)
}

// JobEvent is the in-process projection of a state transition. The
// SSE handler renders it; consumers downstream (UI) decode it.
type JobEvent struct {
	JobID     uuid.UUID
	State     string
	Attempt   int16
	Error     string
	UpdatedAt time.Time
}

// AnnotationSetRepository reads the per-image annotation set + its
// annotations. Used by GET /v1/annotation-sets/:image_id.
type AnnotationSetRepository interface {
	GetByImage(ctx context.Context, imageID, orgID uuid.UUID) (domain.AnnotationSet, []domain.Annotation, error)
}

// AnnotationRepository persists annotations with optimistic locking
// against the parent annotation_set's `version` column (spec §10).
//
// Patch behaviour:
//   - ErrNotFound if the annotation doesn't exist or belongs to another org.
//   - ErrConflict if the supplied If-Match version doesn't match the
//     current set version. The returned `currentVersion` is the value
//     the client should reload from.
//   - On success, the parent set's version is incremented by 1; the
//     returned annotation reflects the patch and the returned
//     `currentVersion` is the new (post-bump) value.
type AnnotationRepository interface {
	Patch(ctx context.Context, id, orgID uuid.UUID, ifMatch int64, patch domain.AnnotationPatch) (ann domain.Annotation, currentVersion int64, err error)
	// Create inserts a new annotation, atomically bumping the parent
	// annotation_set version. Same conflict semantics as Patch: ErrConflict
	// when the supplied ifMatch is stale; the returned currentVersion is
	// the value the client should reload from. createdBy is the user id.
	Create(ctx context.Context, orgID, createdBy uuid.UUID, ifMatch int64, in domain.AnnotationCreate) (ann domain.Annotation, currentVersion int64, err error)
	// SoftDelete marks an annotation deleted (deleted_at = now) and bumps
	// the parent set version. Returns the new version on success, or
	// ErrConflict (with the actual current version) on an If-Match mismatch.
	SoftDelete(ctx context.Context, id, orgID uuid.UUID, ifMatch int64) (currentVersion int64, err error)
	WriteAIResult(ctx context.Context, in AIResultWrite) error
}

// AIResultWrite carries the AI inference fields written by a job callback.
type AIResultWrite struct {
	AnnotationSetID uuid.UUID
	OrgID           uuid.UUID
	MaskStorageKey  *string
	AiScore         *float64
	ModelUsed       *string
}

// AuditRecorder appends to the immutable audit trail.
type AuditRecorder interface {
	Record(ctx context.Context, entry AuditEntry) error
}

type AuditEntry struct {
	OrgID      *uuid.UUID
	ActorID    *uuid.UUID
	ActorKind  string
	Action     string
	Resource   string
	ResourceID *uuid.UUID
	Metadata   map[string]any
	RequestID  string
	TraceID    string
}

// Clock abstracts time so tests can drive presign expiry deterministically.
type Clock interface {
	Now() time.Time
}

type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now().UTC() }
