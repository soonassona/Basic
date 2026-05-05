package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-contrib/sse"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/visionloop/api/internal/application"
	"github.com/visionloop/api/internal/application/jobs"
	"github.com/visionloop/api/internal/domain"
)

// publishRetryAfterMs is the Retry-After hint sent to clients on 503
// from POST /v1/jobs. Long enough that retrying immediately won't beat
// a transient broker hiccup; short enough to feel responsive.
const publishRetryAfterMs = 2000

// SSE timeouts. The 10-minute server cap is from ADR-0004 — clients
// reconnect via Last-Event-ID. Heartbeat keeps proxies from killing the
// idle stream; comments are ignored by EventSource.
const (
	sseConnectionTimeout = 10 * time.Minute
	sseHeartbeatInterval = 20 * time.Second
)

type JobHandlers struct {
	Submit   jobs.SubmitJob
	Get      jobs.GetJob
	Callback jobs.ApplyCallback
	Hub      application.JobEventHub
}

type submitJobRequest struct {
	ImageID string          `json:"image_id" binding:"required"`
	Type    string          `json:"type"     binding:"required"`
	Payload json.RawMessage `json:"payload"`
}

type submitJobResponse struct {
	ID        uuid.UUID `json:"id"`
	State     string    `json:"state"`
	Type      string    `json:"type"`
	ImageID   uuid.UUID `json:"image_id"`
	Replayed  bool      `json:"replayed"`
	CreatedAt string    `json:"created_at,omitempty"`
}

// SubmitJob — POST /v1/jobs
func (h *JobHandlers) SubmitJob(c *gin.Context) {
	caller, ok := callerFrom(c)
	if !ok {
		abortJSON(c, http.StatusUnauthorized, "unauthorized", "missing caller")
		return
	}

	var req submitJobRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		abortJSON(c, http.StatusBadRequest, "invalid_input", err.Error())
		return
	}
	imageID, err := uuid.Parse(req.ImageID)
	if err != nil {
		abortJSON(c, http.StatusBadRequest, "invalid_input", "image_id must be a uuid")
		return
	}

	out, err := h.Submit.Execute(c.Request.Context(), jobs.SubmitJobInput{
		Caller:  caller,
		ImageID: imageID,
		Type:    domain.JobType(req.Type),
		Payload: req.Payload,
	})
	if err != nil {
		if errors.Is(err, jobs.ErrPublishFailed) {
			c.Header("Retry-After", strconv.Itoa(publishRetryAfterMs/1000))
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
				"error": gin.H{
					"code":           "JOB_PUBLISH_FAILED",
					"message":        "row committed but downstream publish failed; retry safe (idempotent)",
					"job_id":         out.Job.ID,
					"retry_after_ms": publishRetryAfterMs,
					"request_id":     c.GetString("vl.request_id"),
				},
			})
			return
		}
		status, code := httpStatusFor(err)
		abortJSON(c, status, code, err.Error())
		return
	}

	if out.Replayed {
		c.Header("Idempotent-Replayed", "true")
	}
	c.Header("ETag", `"`+out.Job.ID.String()+`"`)
	c.JSON(http.StatusAccepted, submitJobResponse{
		ID:        out.Job.ID,
		State:     string(out.Job.State),
		Type:      string(out.Job.Type),
		ImageID:   derefUUID(out.Job.ImageID),
		Replayed:  out.Replayed,
		CreatedAt: out.Job.CreatedAt,
	})
}

type jobDTO struct {
	ID        uuid.UUID `json:"id"`
	OrgID     uuid.UUID `json:"org_id"`
	ImageID   uuid.UUID `json:"image_id"`
	Type      string    `json:"type"`
	State     string    `json:"state"`
	Attempt   int16     `json:"attempt"`
	Error     string    `json:"error,omitempty"`
	DedupKey  string    `json:"dedup_key"`
	CreatedAt string    `json:"created_at,omitempty"`
}

func toJobDTO(j domain.Job) jobDTO {
	return jobDTO{
		ID:        j.ID,
		OrgID:     j.OrgID,
		ImageID:   derefUUID(j.ImageID),
		Type:      string(j.Type),
		State:     string(j.State),
		Attempt:   j.Attempt,
		Error:     j.Error,
		DedupKey:  j.DedupKey,
		CreatedAt: j.CreatedAt,
	}
}

// CallbackRequest is the worker callback envelope.
type CallbackRequest struct {
	State   string          `json:"state"   binding:"required"`
	Attempt int16           `json:"attempt"`
	Error   string          `json:"error"`
	Result  json.RawMessage `json:"result"`
}

// JobCallback — POST /internal/jobs/callback (HMAC-authenticated).
// Always returns the current row state so the worker can confirm
// convergence; 200 + `no_change: true` on idempotent replay.
func (h *JobHandlers) JobCallback(c *gin.Context) {
	jobIDStr := c.GetString("vl.callback.job_id")
	if jobIDStr == "" {
		abortJSON(c, http.StatusUnauthorized, "unauthorized", "missing job id from auth")
		return
	}
	jobID, err := uuid.Parse(jobIDStr)
	if err != nil {
		abortJSON(c, http.StatusBadRequest, "invalid_input", "X-Job-ID is not a uuid")
		return
	}

	var req CallbackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		abortJSON(c, http.StatusBadRequest, "invalid_input", err.Error())
		return
	}

	out, err := h.Callback.Execute(c.Request.Context(), jobs.ApplyCallbackInput{
		JobID:   jobID,
		State:   domain.JobState(req.State),
		Attempt: req.Attempt,
		Error:   req.Error,
		Result:  []byte(req.Result),
	})
	if err != nil {
		status, code := httpStatusFor(err)
		abortJSON(c, status, code, err.Error())
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id":        out.Job.ID,
		"state":     string(out.Job.State),
		"attempt":   out.Job.Attempt,
		"no_change": out.NoChange,
	})
}

// JobEvents — GET /v1/jobs/:id/events. Server-Sent Events stream.
// Read-only; viewers allowed.
//
// Wire format:
//   - One `event: snapshot` frame on connect with the current row state.
//     Suppressed if the client's Last-Event-ID already matches.
//   - One `event: state` frame per transition while connected.
//   - Heartbeat comment frame (`:\n\n`) every 20s.
//   - Server closes after 10 minutes (ADR-0004); client reconnects.
//
// IDs are formatted `{state}:{updatedAtUnixMicro}` so reconnection with
// Last-Event-ID is deterministic across replicas (see ADR-0004 — multi-
// replica fan-out is Phase 8, but the ID scheme is forward-compatible).
func (h *JobHandlers) JobEvents(c *gin.Context) {
	caller, ok := callerFrom(c)
	if !ok {
		abortJSON(c, http.StatusUnauthorized, "unauthorized", "missing caller")
		return
	}
	jobID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		abortJSON(c, http.StatusBadRequest, "invalid_input", "id must be a uuid")
		return
	}

	// Snapshot: confirms tenant scope (404 on cross-org) before we
	// commit to streaming headers.
	snap, err := h.Get.Execute(c.Request.Context(), jobs.GetJobInput{Caller: caller, JobID: jobID})
	if err != nil {
		status, code := httpStatusFor(err)
		abortJSON(c, status, code, err.Error())
		return
	}

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no") // disable nginx buffering
	c.Writer.WriteHeader(http.StatusOK)
	c.Writer.Flush()

	// Subscribe BEFORE emitting the snapshot to close the gap where a
	// state transition could land between snapshot read and subscribe.
	ctx, cancel := context.WithTimeout(c.Request.Context(), sseConnectionTimeout)
	defer cancel()
	events, unsub := h.Hub.Subscribe(ctx, jobID)
	defer unsub()

	lastEventID := c.GetHeader("Last-Event-ID")
	snapEvent := snapshotEvent(snap)
	if lastEventID != snapEvent.Id {
		if err := writeSSE(c.Writer, snapEvent); err != nil {
			return
		}
	}
	if isTerminal(snap.State) {
		// No further transitions possible — close after snapshot.
		return
	}

	heartbeat := time.NewTicker(sseHeartbeatInterval)
	defer heartbeat.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeat.C:
			if _, err := io.WriteString(c.Writer, ":\n\n"); err != nil {
				return
			}
			c.Writer.Flush()
		case ev, ok := <-events:
			if !ok {
				return
			}
			if err := writeSSE(c.Writer, transitionEvent(ev)); err != nil {
				return
			}
			if isTerminal(domain.JobState(ev.State)) {
				return
			}
		}
	}
}

func snapshotEvent(j domain.Job) sse.Event {
	// ID is deterministic in (state, attempt) so a client reconnecting
	// with Last-Event-ID can match and the server suppresses the
	// snapshot. Transition events use the same shape with the row's
	// updated_at micro suffix for ordering.
	return sse.Event{
		Event: "snapshot",
		Id:    fmt.Sprintf("snapshot:%s:%d", j.State, j.Attempt),
		Data: gin.H{
			"id":       j.ID,
			"state":    string(j.State),
			"type":     string(j.Type),
			"image_id": derefUUID(j.ImageID),
			"attempt":  j.Attempt,
			"error":    j.Error,
		},
	}
}

func transitionEvent(ev application.JobEvent) sse.Event {
	return sse.Event{
		Event: "state",
		Id:    fmt.Sprintf("%s:%d", ev.State, ev.UpdatedAt.UTC().UnixMicro()),
		Data: gin.H{
			"id":      ev.JobID,
			"state":   ev.State,
			"attempt": ev.Attempt,
			"error":   ev.Error,
		},
	}
}

func writeSSE(w gin.ResponseWriter, ev sse.Event) error {
	if err := sse.Encode(w, ev); err != nil {
		return err
	}
	w.Flush()
	return nil
}

func isTerminal(state domain.JobState) bool {
	switch state {
	case domain.JobSucceeded, domain.JobFailed, domain.JobCancelled:
		return true
	}
	return false
}

// GetJob — GET /v1/jobs/:id. Read-only; viewers allowed (spec §9).
// Cross-tenant ids return 404 (org_id is part of the SQL WHERE).
func (h *JobHandlers) GetJob(c *gin.Context) {
	caller, ok := callerFrom(c)
	if !ok {
		abortJSON(c, http.StatusUnauthorized, "unauthorized", "missing caller")
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		abortJSON(c, http.StatusBadRequest, "invalid_input", "id must be a uuid")
		return
	}

	job, err := h.Get.Execute(c.Request.Context(), jobs.GetJobInput{Caller: caller, JobID: id})
	if err != nil {
		status, code := httpStatusFor(err)
		abortJSON(c, status, code, err.Error())
		return
	}

	c.Header("ETag", `"`+job.ID.String()+`"`)
	c.JSON(http.StatusOK, toJobDTO(job))
}

func derefUUID(p *uuid.UUID) uuid.UUID {
	if p == nil {
		return uuid.Nil
	}
	return *p
}
