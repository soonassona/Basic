package httpapi_test

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/visionloop/api/internal/application"
	"github.com/visionloop/api/internal/domain"
	"github.com/visionloop/api/internal/infrastructure/eventbus"
)

// readSSEFrame reads up to (and including) the blank-line terminator
// of a single SSE frame and returns it as a single string (with the
// trailing "\n\n" stripped). It uses a bufio scanner with a custom
// split that treats a literal "\n\n" as the boundary.
func readSSEFrame(t *testing.T, r *bufio.Reader, deadline time.Duration) string {
	t.Helper()
	type result struct {
		s   string
		err error
	}
	ch := make(chan result, 1)
	go func() {
		var b strings.Builder
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				ch <- result{b.String(), err}
				return
			}
			if line == "\n" {
				ch <- result{strings.TrimRight(b.String(), "\n"), nil}
				return
			}
			b.WriteString(line)
		}
	}()
	select {
	case r := <-ch:
		if r.err != nil && r.s == "" {
			t.Fatalf("read sse: %v", r.err)
		}
		return r.s
	case <-time.After(deadline):
		t.Fatalf("timed out waiting %s for SSE frame", deadline)
		return ""
	}
}

func TestSSE_TerminalState_EmitsSnapshotAndCloses(t *testing.T) {
	t.Parallel()
	imgID := uuid.New()
	images := &readyImage{id: imgID, orgID: callerOrg}

	id := uuid.New()
	jobs := &stubJobs{created: []domain.Job{{
		ID: id, OrgID: callerOrg, ImageID: &imgID,
		Type: domain.JobTypeAuto, State: domain.JobSucceeded, Attempt: 0,
	}}}
	hub := eventbus.NewMemoryHub()
	router := newTestRouterWithHub(t, domain.RoleAnnotator, images, jobs, &stubPublisher{}, hub)

	srv := httptest.NewServer(router)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/jobs/"+id.String()+"/events", nil)
	req.AddCookie(&http.Cookie{Name: "better-auth.session_token", Value: "valid-token"})

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("content-type: %q", got)
	}

	r := bufio.NewReader(resp.Body)
	frame := readSSEFrame(t, r, 3*time.Second)
	if !strings.Contains(frame, "event:snapshot") && !strings.Contains(frame, "event: snapshot") {
		t.Fatalf("expected snapshot event in frame, got %q", frame)
	}
	if !strings.Contains(frame, "succeeded") {
		t.Fatalf("expected state=succeeded in snapshot, got %q", frame)
	}
	// After terminal snapshot the server closes; subsequent read returns EOF.
	if _, err := r.ReadByte(); err == nil {
		t.Fatal("expected stream to close after terminal snapshot")
	}
}

func TestSSE_PendingJob_StreamsTransition(t *testing.T) {
	t.Parallel()
	imgID := uuid.New()
	images := &readyImage{id: imgID, orgID: callerOrg}

	id := uuid.New()
	jobs := &stubJobs{created: []domain.Job{{
		ID: id, OrgID: callerOrg, ImageID: &imgID,
		Type: domain.JobTypeAuto, State: domain.JobPending, Attempt: 0,
	}}}
	hub := eventbus.NewMemoryHub()
	router := newTestRouterWithHub(t, domain.RoleAnnotator, images, jobs, &stubPublisher{}, hub)

	srv := httptest.NewServer(router)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/v1/jobs/"+id.String()+"/events", nil)
	req.AddCookie(&http.Cookie{Name: "better-auth.session_token", Value: "valid-token"})

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	r := bufio.NewReader(resp.Body)

	// First frame is the snapshot.
	if frame := readSSEFrame(t, r, 3*time.Second); !strings.Contains(frame, "snapshot") {
		t.Fatalf("expected snapshot first, got %q", frame)
	}

	// Publish a transition; expect a state event next.
	go func() {
		time.Sleep(50 * time.Millisecond)
		hub.Publish(id, application.JobEvent{
			JobID: id, State: "running", Attempt: 0, UpdatedAt: time.Now(),
		})
	}()

	frame := readSSEFrame(t, r, 3*time.Second)
	if !strings.Contains(frame, "event:state") && !strings.Contains(frame, "event: state") {
		t.Fatalf("expected state event, got %q", frame)
	}
	if !strings.Contains(frame, "running") {
		t.Fatalf("expected state=running in frame, got %q", frame)
	}

	// Terminal transition closes the stream.
	go func() {
		time.Sleep(20 * time.Millisecond)
		hub.Publish(id, application.JobEvent{
			JobID: id, State: "succeeded", Attempt: 0, UpdatedAt: time.Now(),
		})
	}()
	frame = readSSEFrame(t, r, 3*time.Second)
	if !strings.Contains(frame, "succeeded") {
		t.Fatalf("expected succeeded transition, got %q", frame)
	}
	if _, err := r.ReadByte(); err == nil {
		t.Fatal("expected stream to close after terminal transition")
	}
}

func TestSSE_LastEventID_SuppressesSnapshot(t *testing.T) {
	t.Parallel()
	imgID := uuid.New()
	images := &readyImage{id: imgID, orgID: callerOrg}

	id := uuid.New()
	jobs := &stubJobs{created: []domain.Job{{
		ID: id, OrgID: callerOrg, ImageID: &imgID,
		Type: domain.JobTypeAuto, State: domain.JobPending, Attempt: 0,
	}}}
	hub := eventbus.NewMemoryHub()
	router := newTestRouterWithHub(t, domain.RoleAnnotator, images, jobs, &stubPublisher{}, hub)

	srv := httptest.NewServer(router)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/v1/jobs/"+id.String()+"/events", nil)
	req.AddCookie(&http.Cookie{Name: "better-auth.session_token", Value: "valid-token"})
	// ID format must match snapshotEvent: "snapshot:<state>:<attempt>".
	req.Header.Set("Last-Event-ID", "snapshot:pending:0")

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	r := bufio.NewReader(resp.Body)

	// Publish a transition immediately. If snapshot was suppressed, the
	// FIRST frame is the state transition, not a snapshot.
	go func() {
		time.Sleep(20 * time.Millisecond)
		hub.Publish(id, application.JobEvent{
			JobID: id, State: "succeeded", Attempt: 0, UpdatedAt: time.Now(),
		})
	}()

	frame := readSSEFrame(t, r, 3*time.Second)
	if strings.Contains(frame, "snapshot") {
		t.Fatalf("expected snapshot to be suppressed by Last-Event-ID, got %q", frame)
	}
	if !strings.Contains(frame, "succeeded") {
		t.Fatalf("expected first frame to be the state transition, got %q", frame)
	}
}

func TestSSE_CrossOrgJob_404(t *testing.T) {
	t.Parallel()
	imgID := uuid.New()
	images := &readyImage{id: imgID, orgID: callerOrg}
	otherOrg := uuid.New()
	id := uuid.New()
	jobs := &stubJobs{created: []domain.Job{{
		ID: id, OrgID: otherOrg, ImageID: &imgID,
		Type: domain.JobTypeAuto, State: domain.JobPending,
	}}}
	router := newTestRouterWith(t, domain.RoleAnnotator, images, jobs, &stubPublisher{})

	srv := httptest.NewServer(router)
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/jobs/"+id.String()+"/events", nil)
	req.AddCookie(&http.Cookie{Name: "better-auth.session_token", Value: "valid-token"})

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("want 404, got %d", resp.StatusCode)
	}
}
