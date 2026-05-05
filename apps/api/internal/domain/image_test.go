package domain_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/visionloop/api/internal/domain"
)

func TestValidateUploadRequest(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		contentType string
		size        int64
		wantErr     error
	}{
		{"jpeg ok", "image/jpeg", 100_000, nil},
		{"png ok", "image/png", 1, nil},
		{"webp ok mixed case", "Image/WebP", 10, nil},
		{"zero size", "image/jpeg", 0, domain.ErrInvalidInput},
		{"negative size", "image/jpeg", -1, domain.ErrInvalidInput},
		{"too large", "image/jpeg", domain.MaxImageBytes + 1, domain.ErrTooLarge},
		{"gif blocked", "image/gif", 100, domain.ErrUnsupportedCT},
		{"text rejected", "text/plain", 100, domain.ErrUnsupportedCT},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := domain.ValidateUploadRequest(tc.contentType, tc.size)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("want nil, got %v", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("want %v, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestStorageKeyFor(t *testing.T) {
	t.Parallel()
	org := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	img := uuid.MustParse("00000000-0000-0000-0000-000000000002")

	got := domain.StorageKeyFor(org, img, "image/png")
	want := "orgs/00000000-0000-0000-0000-000000000001/images/00000000-0000-0000-0000-000000000002.png"
	if got != want {
		t.Fatalf("want %s, got %s", want, got)
	}

	// Unknown content types still produce a deterministic key (.bin) so a
	// failed validation does not leave a presigned URL with an empty path.
	bin := domain.StorageKeyFor(org, img, "application/octet-stream")
	if !strings.HasSuffix(bin, ".bin") {
		t.Fatalf("expected .bin extension fallback, got %s", bin)
	}
}

func TestNormalizeSlug(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in      string
		out     string
		wantErr bool
	}{
		{"acme", "acme", false},
		{"  ACME  ", "acme", false},
		{"my-org-1", "my-org-1", false},
		{"ab", "", true},     // too short
		{"-abc", "", true},   // leading hyphen
		{"abc-", "", true},   // trailing hyphen
		{"a b", "", true},    // space
		{"a_b_c", "", true},  // underscore
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			got, err := domain.NormalizeSlug(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.out {
				t.Fatalf("want %q, got %q", tc.out, got)
			}
		})
	}
}

func TestRolePermissions(t *testing.T) {
	t.Parallel()
	if !domain.RoleOwner.CanManageMembers() {
		t.Fatal("owner should manage members")
	}
	if domain.RoleViewer.CanWriteImages() {
		t.Fatal("viewer must not write images")
	}
	if !domain.RoleAnnotator.CanWriteImages() {
		t.Fatal("annotator must write images")
	}
	if domain.RoleAnnotator.CanManageMembers() {
		t.Fatal("annotator must not manage members")
	}
}
