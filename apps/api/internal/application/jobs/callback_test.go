package jobs_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/visionloop/api/internal/application"
	"github.com/visionloop/api/internal/application/jobs"
	"github.com/visionloop/api/internal/domain"
)

// ── fakes ─────────────────────────────────────────────────────────────

type cbJobsRepo struct {
	job domain.Job
}

func (f *cbJobsRepo) Create(_ context.Context, _ domain.Job) (domain.Job, error) {
	return domain.Job{}, nil
}
func (f *cbJobsRepo) Get(_ context.Context, _, _ uuid.UUID) (domain.Job, error) {
	return f.job, nil
}
func (f *cbJobsRepo) FindActiveByDedupKey(_ context.Context, _ uuid.UUID, _ string, _ time.Time) (domain.Job, error) {
	return domain.Job{}, domain.ErrNotFound
}
func (f *cbJobsRepo) MarkErrored(_ context.Context, _, _ uuid.UUID, _ string) error { return nil }
func (f *cbJobsRepo) ApplyCallback(_ context.Context, in application.JobCallback) (domain.Job, error) {
	j := f.job
	j.State = in.State
	return j, nil
}

type cbHub struct{}

func (cbHub) Subscribe(_ context.Context, _ uuid.UUID) (<-chan application.JobEvent, func()) {
	ch := make(chan application.JobEvent)
	return ch, func() {}
}
func (cbHub) Publish(_ uuid.UUID, _ application.JobEvent) {}

type cbAudit struct{}

func (cbAudit) Record(_ context.Context, _ application.AuditEntry) error { return nil }

type cbClock struct{}

func (cbClock) Now() time.Time { return time.Time{} }

type cbAnnotationSets struct {
	set      domain.AnnotationSet
	notFound bool
}

func (f *cbAnnotationSets) GetByImage(_ context.Context, imageID, orgID uuid.UUID) (domain.AnnotationSet, []domain.Annotation, error) {
	if f.notFound || f.set.ImageID != imageID || f.set.OrgID != orgID {
		return domain.AnnotationSet{}, nil, domain.ErrNotFound
	}
	return f.set, nil, nil
}
func (f *cbAnnotationSets) EnsureForImage(_ context.Context, _, _, _ uuid.UUID) (domain.AnnotationSet, error) {
	return f.set, nil
}

type cbAnnotations struct {
	writeCalled bool
	lastWrite   application.AIResultWrite
	writeErr    error // if non-nil, WriteAIResult returns this error
}

func (f *cbAnnotations) Patch(_ context.Context, _ uuid.UUID, _ uuid.UUID, _ int64, _ domain.AnnotationPatch) (domain.Annotation, int64, error) {
	return domain.Annotation{}, 0, nil
}
func (f *cbAnnotations) WriteAIResult(_ context.Context, in application.AIResultWrite) error {
	f.writeCalled = true
	f.lastWrite = in
	return f.writeErr
}
func (f *cbAnnotations) Create(_ context.Context, _, _ uuid.UUID, _ int64, _ domain.AnnotationCreate) (domain.Annotation, int64, error) {
	return domain.Annotation{}, 0, nil
}
func (f *cbAnnotations) SoftDelete(_ context.Context, _, _ uuid.UUID, _ int64) (int64, error) {
	return 0, nil
}

// ── helpers ────────────────────────────────────────────────────────────

func newCBUseCase(repo *cbJobsRepo, anns *cbAnnotations, sets *cbAnnotationSets) jobs.ApplyCallback {
	return jobs.ApplyCallback{
		Jobs:           repo,
		Hub:            cbHub{},
		Audit:          cbAudit{},
		Clock:          cbClock{},
		Annotations:    anns,
		AnnotationSets: sets,
	}
}

func makeResult(maskKey, modelUsed, imageID string, aiScore float64) []byte {
	b, _ := json.Marshal(map[string]any{
		"mask_storage_key": maskKey,
		"model_used":       modelUsed,
		"ai_score":         aiScore,
		"image_id":         imageID,
	})
	return b
}

// ── tests ──────────────────────────────────────────────────────────────

func TestApplyCallback_Succeeded_WritesAIResult(t *testing.T) {
	imgID := uuid.New()
	orgID := uuid.New()
	setID := uuid.New()

	repo := &cbJobsRepo{job: domain.Job{ID: uuid.New(), OrgID: orgID, ImageID: &imgID}}
	anns := &cbAnnotations{}
	sets := &cbAnnotationSets{set: domain.AnnotationSet{ID: setID, ImageID: imgID, OrgID: orgID}}

	uc := newCBUseCase(repo, anns, sets)
	result := makeResult("jobs/abc/mask.png", "sam2.1_hiera_large", imgID.String(), 0.91)

	_, err := uc.Execute(context.Background(), jobs.ApplyCallbackInput{
		JobID: repo.job.ID, State: domain.JobSucceeded, Result: result,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !anns.writeCalled {
		t.Fatal("expected WriteAIResult to be called")
	}
	if anns.lastWrite.AnnotationSetID != setID {
		t.Errorf("set ID: got %v want %v", anns.lastWrite.AnnotationSetID, setID)
	}
	if *anns.lastWrite.MaskStorageKey != "jobs/abc/mask.png" {
		t.Errorf("mask key: got %q", *anns.lastWrite.MaskStorageKey)
	}
	if *anns.lastWrite.ModelUsed != "sam2.1_hiera_large" {
		t.Errorf("model: got %q", *anns.lastWrite.ModelUsed)
	}
}

func TestApplyCallback_Running_SkipsAIResult(t *testing.T) {
	imgID := uuid.New()
	repo := &cbJobsRepo{job: domain.Job{ID: uuid.New(), OrgID: uuid.New(), ImageID: &imgID}}
	anns := &cbAnnotations{}
	sets := &cbAnnotationSets{notFound: true}

	uc := newCBUseCase(repo, anns, sets)
	_, err := uc.Execute(context.Background(), jobs.ApplyCallbackInput{
		JobID: repo.job.ID, State: domain.JobRunning,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if anns.writeCalled {
		t.Fatal("WriteAIResult must not be called for running state")
	}
}

func TestApplyCallback_Succeeded_EmptyResult_SkipsAIResult(t *testing.T) {
	imgID := uuid.New()
	repo := &cbJobsRepo{job: domain.Job{ID: uuid.New(), OrgID: uuid.New(), ImageID: &imgID}}
	anns := &cbAnnotations{}
	sets := &cbAnnotationSets{notFound: true}

	uc := newCBUseCase(repo, anns, sets)
	_, err := uc.Execute(context.Background(), jobs.ApplyCallbackInput{
		JobID: repo.job.ID, State: domain.JobSucceeded, Result: nil,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if anns.writeCalled {
		t.Fatal("WriteAIResult must not be called when result is empty")
	}
}

// TestApplyCallback_WriteAIResult_ErrorDoesNotFailJob verifies fix #1:
// a WriteAIResult failure is logged but does NOT cause Execute to return
// an error — the job is still marked succeeded in the database.
func TestApplyCallback_WriteAIResult_ErrorDoesNotFailJob(t *testing.T) {
	imgID := uuid.New()
	orgID := uuid.New()
	setID := uuid.New()

	repo := &cbJobsRepo{job: domain.Job{ID: uuid.New(), OrgID: orgID, ImageID: &imgID}}
	anns := &cbAnnotations{writeErr: errors.New("storage unavailable")}
	sets := &cbAnnotationSets{set: domain.AnnotationSet{ID: setID, ImageID: imgID, OrgID: orgID}}

	uc := newCBUseCase(repo, anns, sets)
	result := makeResult("jobs/abc/mask.png", "sam2.1_hiera_large", imgID.String(), 0.91)

	out, err := uc.Execute(context.Background(), jobs.ApplyCallbackInput{
		JobID: repo.job.ID, State: domain.JobSucceeded, Result: result,
	})
	if err != nil {
		t.Fatalf("Execute must not propagate WriteAIResult error, got: %v", err)
	}
	if out.Job.State != domain.JobSucceeded {
		t.Errorf("job state: got %q want %q", out.Job.State, domain.JobSucceeded)
	}
	if !anns.writeCalled {
		t.Fatal("WriteAIResult should still have been attempted")
	}
}

// TestApplyCallback_Succeeded_UsesFallbackMaskKey verifies that when the
// worker result contains no mask_storage_key, the callback synthesises a
// default key from the job ID instead of leaving it empty.
func TestApplyCallback_Succeeded_UsesFallbackMaskKey(t *testing.T) {
	imgID := uuid.New()
	orgID := uuid.New()
	setID := uuid.New()
	jobID := uuid.New()

	repo := &cbJobsRepo{job: domain.Job{ID: jobID, OrgID: orgID, ImageID: &imgID}}
	anns := &cbAnnotations{}
	sets := &cbAnnotationSets{set: domain.AnnotationSet{ID: setID, ImageID: imgID, OrgID: orgID}}

	uc := newCBUseCase(repo, anns, sets)
	// result has no mask_storage_key field → fallback should kick in
	result := makeResult("", "sam2.1_hiera_large", imgID.String(), 0.75)

	_, err := uc.Execute(context.Background(), jobs.ApplyCallbackInput{
		JobID: jobID, State: domain.JobSucceeded, Result: result,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !anns.writeCalled {
		t.Fatal("WriteAIResult should have been called")
	}
	wantKey := "jobs/" + jobID.String() + "/mask.png"
	if *anns.lastWrite.MaskStorageKey != wantKey {
		t.Errorf("fallback mask key: got %q want %q", *anns.lastWrite.MaskStorageKey, wantKey)
	}
}

// TestApplyCallback_Succeeded_AnnotationSetNotFound_SkipsWrite verifies
// that a missing annotation set (e.g. image deleted mid-job) does not
// cause Execute to fail — the AI result is silently skipped.
func TestApplyCallback_Succeeded_AnnotationSetNotFound_SkipsWrite(t *testing.T) {
	imgID := uuid.New()
	repo := &cbJobsRepo{job: domain.Job{ID: uuid.New(), OrgID: uuid.New(), ImageID: &imgID}}
	anns := &cbAnnotations{}
	sets := &cbAnnotationSets{notFound: true}

	uc := newCBUseCase(repo, anns, sets)
	result := makeResult("jobs/abc/mask.png", "yolov11x", imgID.String(), 0.6)

	_, err := uc.Execute(context.Background(), jobs.ApplyCallbackInput{
		JobID: repo.job.ID, State: domain.JobSucceeded, Result: result,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if anns.writeCalled {
		t.Fatal("WriteAIResult must not be called when annotation set is not found")
	}
}
