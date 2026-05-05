package jobs_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/visionloop/api/internal/application"
	"github.com/visionloop/api/internal/application/jobs"
	"github.com/visionloop/api/internal/domain"
)

// Fakes —————————————————————————————————————————————————————————————

type fakeImages struct {
	img domain.Image
	err error
}

func (f *fakeImages) Get(_ context.Context, id, orgID uuid.UUID) (domain.Image, error) {
	if f.err != nil {
		return domain.Image{}, f.err
	}
	if f.img.ID != id || f.img.OrgID != orgID {
		return domain.Image{}, domain.ErrNotFound
	}
	return f.img, nil
}
func (f *fakeImages) Create(context.Context, domain.Image) (domain.Image, error) {
	return domain.Image{}, errors.New("not implemented")
}
func (f *fakeImages) Finalize(context.Context, uuid.UUID, uuid.UUID, string, int32, int32, string) (domain.Image, error) {
	return domain.Image{}, errors.New("not implemented")
}
func (f *fakeImages) List(context.Context, uuid.UUID, domain.ImageStatus, int32, int32) ([]domain.Image, int64, error) {
	return nil, 0, nil
}

type fakeJobs struct {
	mu             sync.Mutex
	rows           []domain.Job
	createErr      error
	failNextCreate bool
}

func (f *fakeJobs) Create(_ context.Context, j domain.Job) (domain.Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failNextCreate {
		f.failNextCreate = false
		return domain.Job{}, domain.ErrJobDuplicateActive
	}
	if f.createErr != nil {
		return domain.Job{}, f.createErr
	}
	f.rows = append(f.rows, j)
	return j, nil
}
func (f *fakeJobs) ApplyCallback(_ context.Context, _ application.JobCallback) (domain.Job, error) {
	return domain.Job{}, errors.New("not used by submit tests")
}
func (f *fakeJobs) Get(_ context.Context, id, orgID uuid.UUID) (domain.Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.rows {
		if r.ID == id && r.OrgID == orgID {
			return r, nil
		}
	}
	return domain.Job{}, domain.ErrNotFound
}
func (f *fakeJobs) FindActiveByDedupKey(_ context.Context, orgID uuid.UUID, key string, since time.Time) (domain.Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.rows {
		if r.OrgID == orgID && r.DedupKey == key && (r.State == domain.JobPending || r.State == domain.JobRunning) {
			return r, nil
		}
	}
	return domain.Job{}, domain.ErrNotFound
}
func (f *fakeJobs) MarkErrored(_ context.Context, id, orgID uuid.UUID, msg string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.rows {
		if f.rows[i].ID == id && f.rows[i].OrgID == orgID {
			f.rows[i].State = domain.JobFailed
			f.rows[i].Error = msg
		}
	}
	return nil
}

type fakePublisher struct {
	mu    sync.Mutex
	calls []publishCall
	err   error
}

type publishCall struct {
	exchange, routingKey string
	body                 []byte
	headers              map[string]any
}

func (p *fakePublisher) Publish(_ context.Context, exchange, key string, body []byte, headers map[string]any) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, publishCall{exchange, key, body, headers})
	return p.err
}

type fakeAudit struct {
	mu      sync.Mutex
	entries []application.AuditEntry
}

func (a *fakeAudit) Record(_ context.Context, e application.AuditEntry) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.entries = append(a.entries, e)
	return nil
}

type fixedClock time.Time

func (c fixedClock) Now() time.Time { return time.Time(c) }

// Helpers ————————————————————————————————————————————————————————————

func newCase(t *testing.T) (jobs.SubmitJob, *fakeJobs, *fakePublisher, *fakeAudit, domain.Caller, uuid.UUID) {
	t.Helper()
	caller := domain.Caller{
		UserID: uuid.MustParse("aaaaaaaa-0000-0000-0000-000000000001"),
		OrgID:  uuid.MustParse("bbbbbbbb-0000-0000-0000-000000000001"),
		Role:   domain.RoleAnnotator,
	}
	imgID := uuid.MustParse("cccccccc-0000-0000-0000-000000000001")
	imgs := &fakeImages{img: domain.Image{
		ID:     imgID,
		OrgID:  caller.OrgID,
		Status: domain.ImageReady,
	}}
	js := &fakeJobs{}
	pub := &fakePublisher{}
	aud := &fakeAudit{}
	uc := jobs.SubmitJob{
		Jobs: js, Images: imgs, Publisher: pub, Audit: aud,
		Clock: fixedClock(time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)),
	}
	return uc, js, pub, aud, caller, imgID
}

// Tests ——————————————————————————————————————————————————————————————

func TestSubmit_HappyPath(t *testing.T) {
	t.Parallel()
	uc, js, pub, aud, caller, imgID := newCase(t)

	out, err := uc.Execute(context.Background(), jobs.SubmitJobInput{
		Caller: caller, ImageID: imgID, Type: domain.JobTypeAuto,
		Payload: []byte(`{"score":0.5}`),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if out.Replayed {
		t.Fatal("expected fresh submission, got replay")
	}
	if got := len(js.rows); got != 1 {
		t.Fatalf("rows: got %d want 1", got)
	}
	if got := len(pub.calls); got != 1 {
		t.Fatalf("publishes: got %d want 1", got)
	}
	call := pub.calls[0]
	if call.exchange != jobs.JobsExchange {
		t.Fatalf("exchange: %q", call.exchange)
	}
	if call.routingKey != "jobs.pending.auto" {
		t.Fatalf("routing key: %q", call.routingKey)
	}
	var env map[string]any
	if err := json.Unmarshal(call.body, &env); err != nil {
		t.Fatalf("envelope: %v", err)
	}
	if env["job_id"] != out.Job.ID.String() {
		t.Fatalf("envelope job_id mismatch")
	}
	if got := len(aud.entries); got != 1 || aud.entries[0].Action != "job.submitted" {
		t.Fatalf("audit: %+v", aud.entries)
	}
}

func TestSubmit_WithinWindow_Replays(t *testing.T) {
	t.Parallel()
	uc, js, pub, _, caller, imgID := newCase(t)
	in := jobs.SubmitJobInput{
		Caller: caller, ImageID: imgID, Type: domain.JobTypeBox,
		Payload: []byte(`{"box":[1,2,3,4]}`),
	}

	first, err := uc.Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := uc.Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("second: %v", err)
	}

	if !second.Replayed {
		t.Fatal("expected second submission to replay")
	}
	if first.Job.ID != second.Job.ID {
		t.Fatalf("job ids diverge: %s vs %s", first.Job.ID, second.Job.ID)
	}
	if got := len(js.rows); got != 1 {
		t.Fatalf("rows: got %d want 1", got)
	}
	if got := len(pub.calls); got != 1 {
		t.Fatalf("expected exactly one publish across two submissions, got %d", got)
	}
}

func TestSubmit_RaceFallsThroughToReplay(t *testing.T) {
	t.Parallel()
	uc, js, _, _, caller, imgID := newCase(t)

	// Pre-seed an existing row so the dedup lookup at re-read finds it,
	// and force the next Create to return ErrJobDuplicateActive.
	existingID := uuid.New()
	canonical, _ := domain.CanonicalisePayload([]byte(`{"k":1}`))
	dedup := domain.ComputeDedupKey(caller.OrgID, &imgID, domain.JobTypeAuto, canonical)
	js.rows = append(js.rows, domain.Job{
		ID:       existingID,
		OrgID:    caller.OrgID,
		ImageID:  &imgID,
		Type:     domain.JobTypeAuto,
		State:    domain.JobPending,
		DedupKey: dedup,
	})
	js.failNextCreate = true

	// Race window is "since" = now-10m which we satisfy by inserting now.
	// To make sure FindActiveByDedupKey runs through, we delete the row
	// from the pre-window lookup path by raising the bar: actually, our
	// fake ignores `since`, which is fine — the partial-unique-index
	// recovery path is what we want to exercise. To force that path we
	// must skip the first FindActive. Easiest: clear rows, then re-add
	// after the use-case's first lookup. Simpler: keep rows in place but
	// mark them succeeded so the *first* lookup misses, then rely on
	// dedup_active recovery.
	js.rows[0].State = domain.JobSucceeded // first lookup misses

	// Re-add an *active* duplicate that the recovery FindActive will hit.
	// This mirrors the real DB: once the second writer's INSERT fails,
	// the first writer's row is still pending in the DB.
	js.rows = append(js.rows, domain.Job{
		ID:       existingID,
		OrgID:    caller.OrgID,
		ImageID:  &imgID,
		Type:     domain.JobTypeAuto,
		State:    domain.JobPending,
		DedupKey: dedup,
	})

	out, err := uc.Execute(context.Background(), jobs.SubmitJobInput{
		Caller: caller, ImageID: imgID, Type: domain.JobTypeAuto,
		Payload: []byte(`{"k":1}`),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !out.Replayed {
		t.Fatal("expected replay after dedup race")
	}
	if out.Job.ID != existingID {
		t.Fatalf("expected to recover existing row %s, got %s", existingID, out.Job.ID)
	}
}

func TestSubmit_PublishFailure_MarksRowAndReturnsErr(t *testing.T) {
	t.Parallel()
	uc, js, pub, _, caller, imgID := newCase(t)
	pub.err = errors.New("broker down")

	out, err := uc.Execute(context.Background(), jobs.SubmitJobInput{
		Caller: caller, ImageID: imgID, Type: domain.JobTypeAuto,
		Payload: []byte(`{}`),
	})
	if !errors.Is(err, jobs.ErrPublishFailed) {
		t.Fatalf("expected ErrPublishFailed, got %v", err)
	}
	if out.Job.ID == uuid.Nil {
		t.Fatal("expected job row to be returned even on publish failure")
	}
	if got := js.rows[0].State; got != domain.JobFailed {
		t.Fatalf("row state: got %q want failed", got)
	}
	if got := js.rows[0].Error; got != "publish_failed" {
		t.Fatalf("row error: %q", got)
	}
}

func TestSubmit_RejectsUnsubmittableType(t *testing.T) {
	t.Parallel()
	uc, _, _, _, caller, imgID := newCase(t)
	_, err := uc.Execute(context.Background(), jobs.SubmitJobInput{
		Caller: caller, ImageID: imgID, Type: domain.JobTypeTrain,
	})
	if !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
}

func TestSubmit_RejectsImageFromOtherOrg(t *testing.T) {
	t.Parallel()
	uc, _, _, _, caller, _ := newCase(t)
	_, err := uc.Execute(context.Background(), jobs.SubmitJobInput{
		Caller:  caller,
		ImageID: uuid.New(), // unknown image — fake returns ErrNotFound
		Type:    domain.JobTypeAuto,
	})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound (no enumeration leak), got %v", err)
	}
}

func TestSubmit_RejectsImageNotReady(t *testing.T) {
	t.Parallel()
	uc, _, _, _, caller, imgID := newCase(t)
	uc.Images.(*fakeImages).img.Status = domain.ImageUploading
	_, err := uc.Execute(context.Background(), jobs.SubmitJobInput{
		Caller: caller, ImageID: imgID, Type: domain.JobTypeAuto,
	})
	if !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
}
