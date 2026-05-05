package domain

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// MaxImageBytes mirrors the file validation cap from section 9.
const MaxImageBytes int64 = 50 * 1024 * 1024

// AllowedImageContentTypes is the MIME allowlist for image uploads.
// Magic-byte verification happens server-side at finalize time.
var AllowedImageContentTypes = map[string]string{
	"image/jpeg": "jpg",
	"image/png":  "png",
	"image/webp": "webp",
}

type ImageStatus string

const (
	ImageUploading ImageStatus = "uploading"
	ImageReady     ImageStatus = "ready"
	ImageErrored   ImageStatus = "errored"
	ImageArchived  ImageStatus = "archived"
)

type Image struct {
	ID          uuid.UUID
	OrgID       uuid.UUID
	UploadedBy  uuid.UUID
	StorageKey  string
	ContentType string
	ByteSize    int64
	Width       int32
	Height      int32
	SHA256      string
	Status      ImageStatus
	CreatedAt   time.Time
}

// ValidateUploadRequest checks the byte size and content type of a pending
// upload. Returning ErrInvalidInput / ErrTooLarge / ErrUnsupportedCT lets
// the handler map cleanly to HTTP status codes.
func ValidateUploadRequest(contentType string, byteSize int64) error {
	if byteSize <= 0 {
		return fmt.Errorf("%w: byte_size must be positive", ErrInvalidInput)
	}
	if byteSize > MaxImageBytes {
		return ErrTooLarge
	}
	if _, ok := AllowedImageContentTypes[strings.ToLower(contentType)]; !ok {
		return ErrUnsupportedCT
	}
	return nil
}

// StorageKeyFor returns the canonical R2 key for an image. Layout:
// orgs/<org_id>/images/<image_id>.<ext>
func StorageKeyFor(orgID, imageID uuid.UUID, contentType string) string {
	ext := AllowedImageContentTypes[strings.ToLower(contentType)]
	if ext == "" {
		ext = "bin"
	}
	return fmt.Sprintf("orgs/%s/images/%s.%s", orgID, imageID, ext)
}
