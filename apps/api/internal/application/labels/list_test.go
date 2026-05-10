package labels_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/visionloop/api/internal/application/labels"
	"github.com/visionloop/api/internal/domain"
)

type fakeLabels struct {
	called bool
	gotOrg uuid.UUID
	items  []domain.Label
	err    error
}

func (f *fakeLabels) ListByOrg(_ context.Context, orgID uuid.UUID) ([]domain.Label, error) {
	f.called = true
	f.gotOrg = orgID
	return f.items, f.err
}

func TestListLabels_HappyPath(t *testing.T) {
	caller := domain.Caller{UserID: uuid.New(), OrgID: uuid.New(), Role: domain.RoleAnnotator}
	repo := &fakeLabels{
		items: []domain.Label{
			{ID: uuid.New(), Name: "car", Color: "#4a8ff5"},
			{ID: uuid.New(), Name: "person", Color: "#3ecf8e"},
		},
	}
	uc := labels.ListLabels{Labels: repo}

	out, err := uc.Execute(context.Background(), labels.ListLabelsInput{Caller: caller})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.Items) != 2 {
		t.Fatalf("items: got %d want 2", len(out.Items))
	}
	if !repo.called {
		t.Fatal("expected ListByOrg to be called")
	}
	if repo.gotOrg != caller.OrgID {
		t.Errorf("org_id passed: got %v want %v", repo.gotOrg, caller.OrgID)
	}
}

func TestListLabels_AllowsViewer(t *testing.T) {
	// Read-only endpoint per spec §10 — no role gate at the use-case level.
	caller := domain.Caller{UserID: uuid.New(), OrgID: uuid.New(), Role: domain.RoleViewer}
	repo := &fakeLabels{items: []domain.Label{{ID: uuid.New(), Name: "x", Color: "#000000"}}}
	uc := labels.ListLabels{Labels: repo}

	out, err := uc.Execute(context.Background(), labels.ListLabelsInput{Caller: caller})
	if err != nil {
		t.Fatalf("viewer must be allowed to read labels: %v", err)
	}
	if len(out.Items) != 1 {
		t.Errorf("items: got %d want 1", len(out.Items))
	}
}

func TestListLabels_BubblesRepoError(t *testing.T) {
	repo := &fakeLabels{err: errors.New("db down")}
	uc := labels.ListLabels{Labels: repo}

	_, err := uc.Execute(context.Background(), labels.ListLabelsInput{
		Caller: domain.Caller{OrgID: uuid.New(), Role: domain.RoleAnnotator},
	})
	if err == nil {
		t.Fatal("expected error to bubble")
	}
}

func TestListLabels_EmptyResultIsNotAnError(t *testing.T) {
	repo := &fakeLabels{items: nil}
	uc := labels.ListLabels{Labels: repo}

	out, err := uc.Execute(context.Background(), labels.ListLabelsInput{
		Caller: domain.Caller{OrgID: uuid.New(), Role: domain.RoleAnnotator},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.Items) != 0 {
		t.Errorf("expected empty list, got %d items", len(out.Items))
	}
}
