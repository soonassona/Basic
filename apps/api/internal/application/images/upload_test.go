package images_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/visionloop/api/internal/application"
	"github.com/visionloop/api/internal/application/images"
	"github.com/visionloop/api/internal/domain"
)

// fakeRepo is an in-memory ImageRepository for use-case tests.
type fakeRepo struct {
	stored map[uuid.UUID]domain.Image
}

func newFakeRepo() *fakeRepo { return &fakeRepo{stored: map[uuid.UUID]domain.Image{}} }

func (f *fakeRepo) Create(_ context.Context, img domain.Image) (domain.Image, error) {
	img.CreatedAt = time.Now().UTC()
	f.stored[img.ID] = img
	return img, nil
}

func (f *fakeRepo) Finalize(_ context.Context, id, org uuid.UUID, etag string, w, h int32, sha string) (domain.Image, error) {
	img, ok := f.stored[id]
	if !ok || img.OrgID != org {
		return domain.Image{}, domain.ErrNotFound
	}
	img.Status = domain.ImageReady
	img.Width = w
	img.Height = h
	img.SHA256 = sha
	f.stored[id] = img
	return img, nil
}

func (f *fakeRepo) Get(_ context.Context, id, org uuid.UUID) (domain.Image, error) {
	img, ok := f.stored[id]
	if !ok || img.OrgID != org {
		return domain.Image{}, domain.ErrNotFound
	}
	return img, nil
}

func (f *fakeRepo) List(context.Context, uuid.UUID, domain.ImageStatus, int32, int32) ([]domain.Image, int64, error) {
	return nil, 0, nil
}

type fakeStorage struct {
	objects map[string]application.ObjectInfo
	signed  application.PresignedURL
}

func (f *fakeStorage) PresignPut(_ context.Context, key, _ string, _ int64, _ time.Duration) (application.PresignedURL, error) {
	f.signed.URL = "https://example.test/" + key
	f.signed.Method = "PUT"
	f.signed.Expires = time.Now().Add(15 * time.Minute)
	return f.signed, nil
}

func (f *fakeStorage) PresignGet(_ context.Context, key string, _ time.Duration) (application.PresignedURL, error) {
	return application.PresignedURL{
		URL:     "https://example.test/" + key + "?sig=download",
		Method:  "GET",
		Headers: map[string]string{},
		Expires: time.Now().Add(15 * time.Minute),
	}, nil
}

func (f *fakeStorage) HeadObject(_ context.Context, key string) (application.ObjectInfo, error) {
	if v, ok := f.objects[key]; ok {
		return v, nil
	}
	return application.ObjectInfo{}, domain.ErrNotFound
}

type fakeAudit struct{ entries []application.AuditEntry }

func (f *fakeAudit) Record(_ context.Context, e application.AuditEntry) error {
	f.entries = append(f.entries, e)
	return nil
}

func caller() domain.Caller {
	return domain.Caller{
		UserID: uuid.MustParse("00000000-0000-0000-0000-000000000010"),
		OrgID:  uuid.MustParse("00000000-0000-0000-0000-000000000020"),
		Role:   domain.RoleAnnotator,
	}
}

func TestPresignUpload_HappyPath(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	store := &fakeStorage{}
	audit := &fakeAudit{}

	uc := images.PresignUpload{
		Images:     repo,
		Storage:    store,
		Audit:      audit,
		PresignTTL: 15 * time.Minute,
		Clock:      application.SystemClock{},
	}
	out, err := uc.Execute(context.Background(), images.PresignUploadInput{
		Caller:      caller(),
		ContentType: "image/png",
		ByteSize:    12345,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Image.Status != domain.ImageUploading {
		t.Fatalf("expected uploading, got %s", out.Image.Status)
	}
	if len(audit.entries) != 1 || audit.entries[0].Action != "image.presign" {
		t.Fatalf("expected one presign audit entry, got %+v", audit.entries)
	}
}

func TestPresignUpload_RejectsTooLarge(t *testing.T) {
	t.Parallel()
	uc := images.PresignUpload{
		Images:  newFakeRepo(),
		Storage: &fakeStorage{},
		Audit:   &fakeAudit{},
	}
	_, err := uc.Execute(context.Background(), images.PresignUploadInput{
		Caller:      caller(),
		ContentType: "image/jpeg",
		ByteSize:    domain.MaxImageBytes + 1,
	})
	if !errors.Is(err, domain.ErrTooLarge) {
		t.Fatalf("expected ErrTooLarge, got %v", err)
	}
}

func TestPresignUpload_RejectsBadContentType(t *testing.T) {
	t.Parallel()
	uc := images.PresignUpload{
		Images:  newFakeRepo(),
		Storage: &fakeStorage{},
		Audit:   &fakeAudit{},
	}
	_, err := uc.Execute(context.Background(), images.PresignUploadInput{
		Caller:      caller(),
		ContentType: "image/svg+xml",
		ByteSize:    100,
	})
	if !errors.Is(err, domain.ErrUnsupportedCT) {
		t.Fatalf("expected ErrUnsupportedCT, got %v", err)
	}
}

func TestFinalizeUpload_DetectsByteSizeMismatch(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	store := &fakeStorage{objects: map[string]application.ObjectInfo{}}
	audit := &fakeAudit{}

	pre := images.PresignUpload{
		Images: repo, Storage: store, Audit: audit,
		PresignTTL: time.Minute, Clock: application.SystemClock{},
	}
	out, err := pre.Execute(context.Background(), images.PresignUploadInput{
		Caller: caller(), ContentType: "image/png", ByteSize: 100,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Storage reports a different size than what the client declared.
	store.objects[out.Image.StorageKey] = application.ObjectInfo{
		ContentType: "image/png", ByteSize: 99, ETag: "x",
	}

	fin := images.FinalizeUpload{Images: repo, Storage: store, Audit: audit}
	_, err = fin.Execute(context.Background(), images.FinalizeUploadInput{
		Caller: caller(), ImageID: out.Image.ID, Width: 10, Height: 10,
	})
	if !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput on byte mismatch, got %v", err)
	}
}

func TestFinalizeUpload_HappyPath(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	store := &fakeStorage{objects: map[string]application.ObjectInfo{}}
	audit := &fakeAudit{}

	pre := images.PresignUpload{
		Images: repo, Storage: store, Audit: audit,
		PresignTTL: time.Minute, Clock: application.SystemClock{},
	}
	out, _ := pre.Execute(context.Background(), images.PresignUploadInput{
		Caller: caller(), ContentType: "image/png", ByteSize: 200,
	})
	store.objects[out.Image.StorageKey] = application.ObjectInfo{
		ContentType: "image/png", ByteSize: 200, ETag: "etag-x",
	}

	fin := images.FinalizeUpload{Images: repo, Storage: store, Audit: audit}
	final, err := fin.Execute(context.Background(), images.FinalizeUploadInput{
		Caller: caller(), ImageID: out.Image.ID, Width: 800, Height: 600, SHA256: "abc",
	})
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != domain.ImageReady {
		t.Fatalf("expected ready, got %s", final.Status)
	}
	if got := repo.stored[final.ID]; got.Width != 800 || got.Height != 600 {
		t.Fatalf("dimensions not persisted: %+v", got)
	}
}

// ── GetImage (Slice B5) ────────────────────────────────────────────────────

func TestGetImage_HappyPath_ReturnsImageAndPresignedURL(t *testing.T) {
	repo := newFakeRepo()
	store := &fakeStorage{objects: map[string]application.ObjectInfo{}}
	cl := caller()

	img := domain.Image{
		ID:          uuid.New(),
		OrgID:       cl.OrgID,
		Status:      domain.ImageReady,
		StorageKey:  "orgs/x/images/y.png",
		ContentType: "image/png",
	}
	repo.stored[img.ID] = img

	uc := images.GetImage{Images: repo, Storage: store, PresignTTL: 5 * time.Minute}
	out, err := uc.Execute(context.Background(), images.GetImageInput{Caller: cl, ID: img.ID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Image.ID != img.ID {
		t.Errorf("image id: got %v want %v", out.Image.ID, img.ID)
	}
	if out.DownloadURL.Method != "GET" {
		t.Errorf("expected GET method on presign, got %q", out.DownloadURL.Method)
	}
	if out.DownloadURL.URL == "" {
		t.Error("presign URL must be non-empty")
	}
}

func TestGetImage_NotFoundForOtherOrg(t *testing.T) {
	repo := newFakeRepo()
	store := &fakeStorage{objects: map[string]application.ObjectInfo{}}

	owner := caller()
	img := domain.Image{ID: uuid.New(), OrgID: owner.OrgID, StorageKey: "k"}
	repo.stored[img.ID] = img

	intruder := domain.Caller{UserID: uuid.New(), OrgID: uuid.New(), Role: domain.RoleAnnotator}
	uc := images.GetImage{Images: repo, Storage: store, PresignTTL: 5 * time.Minute}
	_, err := uc.Execute(context.Background(), images.GetImageInput{Caller: intruder, ID: img.ID})
	if err == nil || !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-org access, got: %v", err)
	}
}

func TestGetImage_NotFoundForUnknownID(t *testing.T) {
	repo := newFakeRepo()
	store := &fakeStorage{objects: map[string]application.ObjectInfo{}}
	uc := images.GetImage{Images: repo, Storage: store, PresignTTL: 5 * time.Minute}

	_, err := uc.Execute(context.Background(), images.GetImageInput{Caller: caller(), ID: uuid.New()})
	if err == nil || !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
}
