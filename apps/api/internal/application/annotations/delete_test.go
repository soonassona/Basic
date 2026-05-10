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

type deleteFakeAnns struct {
	called      bool
	gotID       uuid.UUID
	gotOrg      uuid.UUID
	gotIfMatch  int64
	deleteErr   error
	conflictAt  int64
	newVersion  int64
}

func (f *deleteFakeAnns) Patch(context.Context, uuid.UUID, uuid.UUID, int64, domain.AnnotationPatch) (domain.Annotation, int64, error) {
	return domain.Annotation{}, 0, errors.New("not used")
}
func (f *deleteFakeAnns) WriteAIResult(context.Context, application.AIResultWrite) error {
	return nil
}
func (f *deleteFakeAnns) Create(context.Context, uuid.UUID, uuid.UUID, int64, domain.AnnotationCreate) (domain.Annotation, int64, error) {
	return domain.Annotation{}, 0, errors.New("not used")
}
func (f *deleteFakeAnns) SoftDelete(_ context.Context, id, orgID uuid.UUID, ifMatch int64) (int64, error) {
	f.called = true
	f.gotID = id
	f.gotOrg = orgID
	f.gotIfMatch = ifMatch
	if f.conflictAt != 0 {
		return f.conflictAt, domain.ErrConflict
	}
	if f.deleteErr != nil {
		return 0, f.deleteErr
	}
	return f.newVersion, nil
}

// ── happy path ───────────────────────────────────────────────────────────────

func TestDeleteAnnotation_HappyPath(t *testing.T) {
	caller := annotator()
	annID := uuid.New()
	repo := &deleteFakeAnns{newVersion: 12}
	audit := &recordingAudit{}
	uc := annotations.DeleteAnnotation{Annotations: repo, Audit: audit}

	out, err := uc.Execute(context.Background(), annotations.DeleteAnnotationInput{
		Caller: caller, ID: annID, IfMatch: 11,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.NewVersion != 12 {
		t.Errorf("new_version: got %d want 12", out.NewVersion)
	}
	if !repo.called {
		t.Error("expected SoftDelete to be called")
	}
	if repo.gotID != annID {
		t.Errorf("annotation id: got %v want %v", repo.gotID, annID)
	}
	if repo.gotOrg != caller.OrgID {
		t.Errorf("org_id: got %v want %v", repo.gotOrg, caller.OrgID)
	}
	if repo.gotIfMatch != 11 {
		t.Errorf("if_match: got %d want 11", repo.gotIfMatch)
	}
	if len(audit.entries) != 1 || audit.entries[0].Action != "annotation.deleted" {
		t.Errorf("audit: got %+v", audit.entries)
	}
}

// ── RBAC ─────────────────────────────────────────────────────────────────────

func TestDeleteAnnotation_ViewerForbidden(t *testing.T) {
	caller := domain.Caller{UserID: uuid.New(), OrgID: uuid.New(), Role: domain.RoleViewer}
	repo := &deleteFakeAnns{}
	uc := annotations.DeleteAnnotation{Annotations: repo, Audit: &recordingAudit{}}

	_, err := uc.Execute(context.Background(), annotations.DeleteAnnotationInput{
		Caller: caller, ID: uuid.New(), IfMatch: 1,
	})
	if err == nil || !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got: %v", err)
	}
	if repo.called {
		t.Error("SoftDelete must not run for viewers")
	}
}

// ── Error paths ──────────────────────────────────────────────────────────────

func TestDeleteAnnotation_BubblesConflictWithCurrentVersion(t *testing.T) {
	repo := &deleteFakeAnns{conflictAt: 42}
	uc := annotations.DeleteAnnotation{Annotations: repo, Audit: &recordingAudit{}}

	out, err := uc.Execute(context.Background(), annotations.DeleteAnnotationInput{
		Caller: annotator(), ID: uuid.New(), IfMatch: 5,
	})
	if err == nil || !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict, got: %v", err)
	}
	if out.NewVersion != 42 {
		t.Errorf("expected 42 to bubble up, got %d", out.NewVersion)
	}
}

func TestDeleteAnnotation_BubblesNotFound(t *testing.T) {
	repo := &deleteFakeAnns{deleteErr: domain.ErrNotFound}
	uc := annotations.DeleteAnnotation{Annotations: repo, Audit: &recordingAudit{}}

	_, err := uc.Execute(context.Background(), annotations.DeleteAnnotationInput{
		Caller: annotator(), ID: uuid.New(), IfMatch: 1,
	})
	if err == nil || !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
}
