package domain_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/visionloop/api/internal/domain"
)

func TestComputeDedupKey_StableAcrossKeyOrder(t *testing.T) {
	t.Parallel()

	orgID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	imgID := uuid.MustParse("22222222-2222-2222-2222-222222222222")

	a, err := domain.CanonicalisePayload([]byte(`{"b":2,"a":1,"nested":{"y":2,"x":1}}`))
	if err != nil {
		t.Fatalf("canon a: %v", err)
	}
	b, err := domain.CanonicalisePayload([]byte(`{"nested":{"x":1,"y":2},"a":1,"b":2}`))
	if err != nil {
		t.Fatalf("canon b: %v", err)
	}

	keyA := domain.ComputeDedupKey(orgID, &imgID, domain.JobTypeAuto, a)
	keyB := domain.ComputeDedupKey(orgID, &imgID, domain.JobTypeAuto, b)

	if keyA != keyB {
		t.Fatalf("dedup keys diverge for equivalent JSON:\n  a=%s\n  b=%s", keyA, keyB)
	}
	if len(keyA) != 64 {
		t.Fatalf("dedup key length: got %d want 64", len(keyA))
	}
}

func TestComputeDedupKey_DiffersOnAnyFieldChange(t *testing.T) {
	t.Parallel()

	orgID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	otherOrg := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	imgID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	otherImg := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	payload, _ := domain.CanonicalisePayload([]byte(`{"score":0.5}`))
	other, _ := domain.CanonicalisePayload([]byte(`{"score":0.6}`))

	base := domain.ComputeDedupKey(orgID, &imgID, domain.JobTypeAuto, payload)

	cases := []struct {
		name string
		key  string
	}{
		{"different org", domain.ComputeDedupKey(otherOrg, &imgID, domain.JobTypeAuto, payload)},
		{"different image", domain.ComputeDedupKey(orgID, &otherImg, domain.JobTypeAuto, payload)},
		{"different type", domain.ComputeDedupKey(orgID, &imgID, domain.JobTypeBox, payload)},
		{"different payload", domain.ComputeDedupKey(orgID, &imgID, domain.JobTypeAuto, other)},
		{"nil image vs uuid", domain.ComputeDedupKey(orgID, nil, domain.JobTypeAuto, payload)},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if c.key == base {
				t.Fatalf("expected key to differ for %s", c.name)
			}
		})
	}
}

func TestCanonicalisePayload_RejectsInvalidJSON(t *testing.T) {
	t.Parallel()
	if _, err := domain.CanonicalisePayload([]byte(`{not json`)); err == nil {
		t.Fatal("expected invalid JSON to error")
	}
}

func TestCanonicalisePayload_EmptyDefaultsToObject(t *testing.T) {
	t.Parallel()
	out, err := domain.CanonicalisePayload(nil)
	if err != nil {
		t.Fatalf("canon nil: %v", err)
	}
	if string(out) != "{}" {
		t.Fatalf("nil payload: got %q want {}", out)
	}
}

func TestValidateJobSubmission(t *testing.T) {
	t.Parallel()

	ready := domain.Image{Status: domain.ImageReady}
	uploading := domain.Image{Status: domain.ImageUploading}

	cases := []struct {
		name    string
		typ     domain.JobType
		img     domain.Image
		wantMsg string // substring of err.Error(); empty = no error
	}{
		{"auto on ready image", domain.JobTypeAuto, ready, ""},
		{"box on ready image", domain.JobTypeBox, ready, ""},
		{"polygon on ready image", domain.JobTypePolygon, ready, ""},
		{"detect on ready image", domain.JobTypeDetect, ready, ""},
		{"train rejected at boundary", domain.JobTypeTrain, ready, "not submittable"},
		{"export rejected at boundary", domain.JobTypeExport, ready, "not submittable"},
		{"unknown type", domain.JobType("frobnicate"), ready, "not submittable"},
		{"image not ready", domain.JobTypeAuto, uploading, "ready"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			err := domain.ValidateJobSubmission(c.typ, c.img)
			switch {
			case c.wantMsg == "" && err != nil:
				t.Fatalf("expected ok, got %v", err)
			case c.wantMsg != "" && err == nil:
				t.Fatalf("expected error containing %q, got nil", c.wantMsg)
			case c.wantMsg != "" && err != nil && !strings.Contains(err.Error(), c.wantMsg):
				t.Fatalf("error %q missing %q", err.Error(), c.wantMsg)
			}
		})
	}
}
