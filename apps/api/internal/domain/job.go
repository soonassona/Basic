package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/gowebpki/jcs"
)

// JobType enumerates the values written to the job_type Postgres enum.
// Submission via POST /v1/jobs accepts the user-driven types only;
// `train` and `export` are produced internally by the continuous-learning
// pipeline (Phase 7) and are rejected at the submission boundary.
type JobType string

const (
	JobTypeAuto    JobType = "auto"
	JobTypeBox     JobType = "box"
	JobTypePoints  JobType = "points"
	JobTypePolygon JobType = "polygon"
	JobTypeDetect  JobType = "detect"
	JobTypeTrain   JobType = "train"
	JobTypeExport  JobType = "export"
)

// SubmittableJobTypes is the closed set accepted by POST /v1/jobs.
var SubmittableJobTypes = map[JobType]struct{}{
	JobTypeAuto:    {},
	JobTypeBox:     {},
	JobTypePoints:  {},
	JobTypePolygon: {},
	JobTypeDetect:  {},
}

func (t JobType) Submittable() bool {
	_, ok := SubmittableJobTypes[t]
	return ok
}

type JobState string

const (
	JobPending   JobState = "pending"
	JobRunning   JobState = "running"
	JobSucceeded JobState = "succeeded"
	JobFailed    JobState = "failed"
	JobCancelled JobState = "cancelled"
)

// ErrJobDuplicateActive is returned by the JobRepository when the
// partial unique index (state IN pending|running) trips. The use-case
// reads it as a signal to fall through to the dedup-window lookup and
// return the existing job_id.
var ErrJobDuplicateActive = errors.New("job: duplicate active dedup_key")

// Job is the in-memory projection of a row in the jobs table.
type Job struct {
	ID          uuid.UUID
	OrgID       uuid.UUID
	ImageID     *uuid.UUID
	SubmittedBy uuid.UUID
	Type        JobType
	State       JobState
	Payload     []byte // canonical JSON
	DedupKey    string // hex SHA-256, 64 chars
	Attempt     int16
	Error       string
	CreatedAt   string // RFC 3339 — handlers fill this from the row
}

// CanonicalisePayload reduces an arbitrary JSON document to its RFC 8785
// JCS form. Doing this once at the submission boundary lets every
// downstream component (dedup hash, message body, audit metadata) work
// from byte-identical input.
func CanonicalisePayload(rawJSON []byte) ([]byte, error) {
	if len(rawJSON) == 0 {
		return []byte("{}"), nil
	}
	out, err := jcs.Transform(rawJSON)
	if err != nil {
		return nil, fmt.Errorf("%w: payload is not valid JSON", ErrInvalidInput)
	}
	return out, nil
}

// ComputeDedupKey is the canonical hash from spec §11:
//
//	SHA-256( org_id || 0x1f || image_id || 0x1f || type || 0x1f || canonical(payload) )
//
// The 0x1f (ASCII unit separator) byte prevents accidental collisions
// where two fields concatenate to the same string. Both apps/api and
// apps/ai-service must compute this identically — see
// apps/ai-service/app/dedup.py.
func ComputeDedupKey(orgID uuid.UUID, imageID *uuid.UUID, jobType JobType, canonicalPayload []byte) string {
	const sep = 0x1f
	h := sha256.New()
	h.Write([]byte(orgID.String()))
	h.Write([]byte{sep})
	if imageID != nil {
		h.Write([]byte(imageID.String()))
	}
	h.Write([]byte{sep})
	h.Write([]byte(jobType))
	h.Write([]byte{sep})
	h.Write(canonicalPayload)
	return hex.EncodeToString(h.Sum(nil))
}

// ValidateJobSubmission enforces the rules a client-submitted job must
// satisfy before it touches the database. Image-existence and
// org-membership checks live in the use-case (they need a repository).
func ValidateJobSubmission(jobType JobType, image Image) error {
	if !jobType.Submittable() {
		return fmt.Errorf("%w: job type %q is not submittable", ErrInvalidInput, jobType)
	}
	if image.Status != ImageReady {
		return fmt.Errorf("%w: image must be in 'ready' state, got %q", ErrInvalidInput, image.Status)
	}
	return nil
}
