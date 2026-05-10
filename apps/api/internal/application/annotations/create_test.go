package annotations_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/visionloop/api/internal/application"
	"github.com/visionloop/api/internal/application/annotations"
	"github.com/visionloop/api/internal/domain"
)

// Fake AnnotationRepository — tracks Create calls + lets tests inject errors.
type createFakeAnns struct {
	createCalled bool
	createErr    error
	conflictAt   int64 // when non-zero, returns ErrConflict + this current_version
	out          domain.Annotation
	gotIfMatch   int64
	gotIn        domain.AnnotationCreate
	gotOrg       uuid.UUID
	gotCreatedBy uuid.UUID
}

func (f *createFakeAnns) Patch(context.Context, uuid.UUID, uuid.UUID, int64, domain.AnnotationPatch) (domain.Annotation, int64, error) {
	return domain.Annotation{}, 0, errors.New("not used")
}
func (f *createFakeAnns) WriteAIResult(context.Context, application.AIResultWrite) error {
	return nil
}
func (f *createFakeAnns) SoftDelete(context.Context, uuid.UUID, uuid.UUID, int64) (int64, error) {
	return 0, errors.New("not used")
}
func (f *createFakeAnns) Create(_ context.Context, orgID, createdBy uuid.UUID, ifMatch int64, in domain.AnnotationCreate) (domain.Annotation, int64, error) {
	f.createCalled = true
	f.gotIfMatch = ifMatch
	f.gotIn = in
	f.gotOrg = orgID
	f.gotCreatedBy = createdBy
	if f.conflictAt != 0 {
		return domain.Annotation{}, f.conflictAt, domain.ErrConflict
	}
	if f.createErr != nil {
		return domain.Annotation{}, 0, f.createErr
	}
	return f.out, ifMatch + 1, nil
}

type recordingAudit struct {
	entries []application.AuditEntry
}

func (r *recordingAudit) Record(_ context.Context, e application.AuditEntry) error {
	r.entries = append(r.entries, e)
	return nil
}

func annotator() domain.Caller {
	return domain.Caller{UserID: uuid.New(), OrgID: uuid.New(), Role: domain.RoleAnnotator}
}

func validCreate(setID uuid.UUID) domain.AnnotationCreate {
	return domain.AnnotationCreate{
		AnnotationSetID: setID,
		Kind:            domain.AnnotationBBox,
		Geometry:        []byte(`{"x":0,"y":0,"w":1,"h":1}`),
	}
}

// ── happy paths ──────────────────────────────────────────────────────────────

func TestCreateAnnotation_HappyPath(t *testing.T) {
	caller := annotator()
	setID := uuid.New()
	annID := uuid.New()
	repo := &createFakeAnns{
		out: domain.Annotation{ID: annID, AnnotationSetID: setID, Kind: domain.AnnotationBBox},
	}
	audit := &recordingAudit{}
	uc := annotations.CreateAnnotation{Annotations: repo, Audit: audit}

	out, err := uc.Execute(context.Background(), annotations.CreateAnnotationInput{
		Caller: caller, IfMatch: 7, Create: validCreate(setID),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.NewVersion != 8 {
		t.Errorf("new_version: got %d want 8", out.NewVersion)
	}
	if out.Annotation.ID != annID {
		t.Errorf("annotation id: got %v want %v", out.Annotation.ID, annID)
	}
	if !repo.createCalled {
		t.Error("expected repo.Create to be called")
	}
	if repo.gotIfMatch != 7 {
		t.Errorf("if_match passed: got %d want 7", repo.gotIfMatch)
	}
	if repo.gotOrg != caller.OrgID {
		t.Errorf("org_id passed: got %v want %v", repo.gotOrg, caller.OrgID)
	}
	if repo.gotCreatedBy != caller.UserID {
		t.Errorf("created_by passed: got %v want %v", repo.gotCreatedBy, caller.UserID)
	}
}

func TestCreateAnnotation_AuditRecorded(t *testing.T) {
	caller := annotator()
	setID := uuid.New()
	repo := &createFakeAnns{out: domain.Annotation{ID: uuid.New(), Kind: domain.AnnotationBBox}}
	audit := &recordingAudit{}
	uc := annotations.CreateAnnotation{Annotations: repo, Audit: audit}

	_, err := uc.Execute(context.Background(), annotations.CreateAnnotationInput{
		Caller: caller, IfMatch: 1, Create: validCreate(setID),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(audit.entries) != 1 {
		t.Fatalf("audit entries: got %d want 1", len(audit.entries))
	}
	got := audit.entries[0]
	if got.Action != "annotation.created" {
		t.Errorf("audit action: got %q want %q", got.Action, "annotation.created")
	}
	if got.ActorKind != "user" {
		t.Errorf("audit actor_kind: got %q want %q", got.ActorKind, "user")
	}
	if got.Metadata["new_version"] != int64(2) {
		t.Errorf("audit new_version metadata: got %v want 2", got.Metadata["new_version"])
	}
}

// ── RBAC ─────────────────────────────────────────────────────────────────────

func TestCreateAnnotation_ViewerForbidden(t *testing.T) {
	caller := domain.Caller{UserID: uuid.New(), OrgID: uuid.New(), Role: domain.RoleViewer}
	repo := &createFakeAnns{}
	uc := annotations.CreateAnnotation{Annotations: repo, Audit: &recordingAudit{}}

	_, err := uc.Execute(context.Background(), annotations.CreateAnnotationInput{
		Caller: caller, IfMatch: 1, Create: validCreate(uuid.New()),
	})
	if err == nil || !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got: %v", err)
	}
	if repo.createCalled {
		t.Error("repo.Create must not be called when forbidden")
	}
}

// ── Validation ───────────────────────────────────────────────────────────────

func TestCreateAnnotation_RejectsEmptyGeometry(t *testing.T) {
	repo := &createFakeAnns{}
	uc := annotations.CreateAnnotation{Annotations: repo, Audit: &recordingAudit{}}
	in := validCreate(uuid.New())
	in.Geometry = nil

	_, err := uc.Execute(context.Background(), annotations.CreateAnnotationInput{
		Caller: annotator(), IfMatch: 1, Create: in,
	})
	if err == nil || !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got: %v", err)
	}
	if repo.createCalled {
		t.Error("repo.Create must not be called for invalid input")
	}
}

func TestCreateAnnotation_RejectsUnknownKind(t *testing.T) {
	repo := &createFakeAnns{}
	uc := annotations.CreateAnnotation{Annotations: repo, Audit: &recordingAudit{}}
	in := validCreate(uuid.New())
	in.Kind = "trapezoid"

	_, err := uc.Execute(context.Background(), annotations.CreateAnnotationInput{
		Caller: annotator(), IfMatch: 1, Create: in,
	})
	if err == nil || !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got: %v", err)
	}
}

func TestCreateAnnotation_RejectsNilSetID(t *testing.T) {
	repo := &createFakeAnns{}
	uc := annotations.CreateAnnotation{Annotations: repo, Audit: &recordingAudit{}}
	in := validCreate(uuid.New())
	in.AnnotationSetID = uuid.Nil

	_, err := uc.Execute(context.Background(), annotations.CreateAnnotationInput{
		Caller: annotator(), IfMatch: 1, Create: in,
	})
	if err == nil || !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got: %v", err)
	}
}

// ── Error paths ──────────────────────────────────────────────────────────────

func TestCreateAnnotation_BubblesConflictWithCurrentVersion(t *testing.T) {
	repo := &createFakeAnns{conflictAt: 99}
	uc := annotations.CreateAnnotation{Annotations: repo, Audit: &recordingAudit{}}

	out, err := uc.Execute(context.Background(), annotations.CreateAnnotationInput{
		Caller: annotator(), IfMatch: 1, Create: validCreate(uuid.New()),
	})
	if err == nil || !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict, got: %v", err)
	}
	if out.NewVersion != 99 {
		t.Errorf("expected current_version 99 to bubble up, got %d", out.NewVersion)
	}
}

func TestCreateAnnotation_BubblesNotFound(t *testing.T) {
	repo := &createFakeAnns{createErr: domain.ErrNotFound}
	uc := annotations.CreateAnnotation{Annotations: repo, Audit: &recordingAudit{}}

	_, err := uc.Execute(context.Background(), annotations.CreateAnnotationInput{
		Caller: annotator(), IfMatch: 1, Create: validCreate(uuid.New()),
	})
	if err == nil || !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
}
