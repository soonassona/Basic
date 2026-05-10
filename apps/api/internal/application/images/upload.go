// Package images contains the use-cases backing /v1/images.
package images

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/visionloop/api/internal/application"
	"github.com/visionloop/api/internal/domain"
)

// PresignUpload registers a pending image row, signs an R2 PUT URL, and
// returns both. The image lands as `uploading`; FinalizeUpload promotes it
// to `ready` once the client confirms the upload.
type PresignUpload struct {
	Images       application.ImageRepository
	Storage      application.ObjectStore
	Audit        application.AuditRecorder
	PresignTTL   time.Duration
	Clock        application.Clock
}

type PresignUploadInput struct {
	Caller      domain.Caller
	ContentType string
	ByteSize    int64
}

type PresignUploadOutput struct {
	Image  domain.Image
	Upload application.PresignedURL
}

func (u PresignUpload) Execute(ctx context.Context, in PresignUploadInput) (PresignUploadOutput, error) {
	if err := domain.ValidateUploadRequest(in.ContentType, in.ByteSize); err != nil {
		return PresignUploadOutput{}, err
	}

	id := uuid.Must(uuid.NewRandom())
	key := domain.StorageKeyFor(in.Caller.OrgID, id, in.ContentType)

	img, err := u.Images.Create(ctx, domain.Image{
		ID:          id,
		OrgID:       in.Caller.OrgID,
		UploadedBy:  in.Caller.UserID,
		StorageKey:  key,
		ContentType: in.ContentType,
		ByteSize:    in.ByteSize,
		Status:      domain.ImageUploading,
	})
	if err != nil {
		return PresignUploadOutput{}, fmt.Errorf("persist image: %w", err)
	}

	signed, err := u.Storage.PresignPut(ctx, key, in.ContentType, in.ByteSize, u.PresignTTL)
	if err != nil {
		return PresignUploadOutput{}, fmt.Errorf("presign: %w", err)
	}

	_ = u.Audit.Record(ctx, application.AuditEntry{
		OrgID:      &in.Caller.OrgID,
		ActorID:    &in.Caller.UserID,
		ActorKind:  "user",
		Action:     "image.presign",
		Resource:   "image",
		ResourceID: &img.ID,
		Metadata: map[string]any{
			"content_type": in.ContentType,
			"byte_size":    in.ByteSize,
		},
	})

	return PresignUploadOutput{Image: img, Upload: signed}, nil
}

// FinalizeUpload reconciles a successful client upload with the database.
// It re-reads the object from storage to confirm presence and size, then
// flips the row to `ready`. As a side effect (so the studio always has a
// parent set to PATCH against) we also EnsureForImage on the
// AnnotationSetRepository — best-effort: an error here is logged via the
// returned image's metadata audit but does NOT fail the upload.
type FinalizeUpload struct {
	Images         application.ImageRepository
	Storage        application.ObjectStore
	Audit          application.AuditRecorder
	AnnotationSets application.AnnotationSetRepository
}

type FinalizeUploadInput struct {
	Caller  domain.Caller
	ImageID uuid.UUID
	Width   int32
	Height  int32
	SHA256  string
}

func (u FinalizeUpload) Execute(ctx context.Context, in FinalizeUploadInput) (domain.Image, error) {
	pending, err := u.Images.Get(ctx, in.ImageID, in.Caller.OrgID)
	if err != nil {
		return domain.Image{}, err
	}
	if pending.Status != domain.ImageUploading {
		return domain.Image{}, fmt.Errorf("%w: image already finalized", domain.ErrConflict)
	}

	info, err := u.Storage.HeadObject(ctx, pending.StorageKey)
	if err != nil {
		return domain.Image{}, fmt.Errorf("head object: %w", err)
	}
	if info.ByteSize != pending.ByteSize {
		return domain.Image{}, fmt.Errorf("%w: byte size mismatch (expected %d, found %d)",
			domain.ErrInvalidInput, pending.ByteSize, info.ByteSize)
	}

	final, err := u.Images.Finalize(ctx, in.ImageID, in.Caller.OrgID, info.ETag, in.Width, in.Height, in.SHA256)
	if err != nil {
		return domain.Image{}, err
	}

	// Provision the annotation_set so the studio can PATCH against it
	// without a separate setup call. Best-effort by design: a failure here
	// surfaces through metadata so ops can spot it, but the image still
	// counts as ready (the user can re-trigger via a no-op PATCH later).
	annotationSetProvisioned := true
	if u.AnnotationSets != nil {
		if _, err := u.AnnotationSets.EnsureForImage(ctx, in.Caller.OrgID, final.ID, in.Caller.UserID); err != nil {
			annotationSetProvisioned = false
		}
	}

	_ = u.Audit.Record(ctx, application.AuditEntry{
		OrgID:      &in.Caller.OrgID,
		ActorID:    &in.Caller.UserID,
		ActorKind:  "user",
		Action:     "image.finalize",
		Resource:   "image",
		ResourceID: &final.ID,
		Metadata: map[string]any{
			"width":                       in.Width,
			"height":                      in.Height,
			"annotation_set_provisioned":  annotationSetProvisioned,
		},
	})
	return final, nil
}

// ListImages is the read query backing GET /v1/images.
type ListImages struct {
	Images application.ImageRepository
}

type ListImagesInput struct {
	Caller domain.Caller
	Status domain.ImageStatus
	Limit  int32
	Offset int32
}

type ListImagesOutput struct {
	Items []domain.Image
	Total int64
}

func (u ListImages) Execute(ctx context.Context, in ListImagesInput) (ListImagesOutput, error) {
	if in.Limit <= 0 || in.Limit > 200 {
		in.Limit = 50
	}
	items, total, err := u.Images.List(ctx, in.Caller.OrgID, in.Status, in.Limit, in.Offset)
	if err != nil {
		return ListImagesOutput{}, err
	}
	return ListImagesOutput{Items: items, Total: total}, nil
}

// GetImage is the read query backing GET /v1/images/:id. Returns the
// image record + a freshly minted presigned download URL the studio can
// load into the canvas without proxying through the API.
type GetImage struct {
	Images     application.ImageRepository
	Storage    application.ObjectStore
	PresignTTL time.Duration
}

type GetImageInput struct {
	Caller domain.Caller
	ID     uuid.UUID
}

type GetImageOutput struct {
	Image       domain.Image
	DownloadURL application.PresignedURL
}

func (u GetImage) Execute(ctx context.Context, in GetImageInput) (GetImageOutput, error) {
	img, err := u.Images.Get(ctx, in.ID, in.Caller.OrgID)
	if err != nil {
		return GetImageOutput{}, err
	}
	url, err := u.Storage.PresignGet(ctx, img.StorageKey, u.PresignTTL)
	if err != nil {
		return GetImageOutput{}, fmt.Errorf("presign download: %w", err)
	}
	return GetImageOutput{Image: img, DownloadURL: url}, nil
}
