package domain

import (
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Role enumerates the four membership roles defined in section 9.
type Role string

const (
	RoleOwner     Role = "owner"
	RoleAdmin     Role = "admin"
	RoleAnnotator Role = "annotator"
	RoleViewer    Role = "viewer"
)

// CanWriteImages returns true when the role is allowed to upload or edit images.
func (r Role) CanWriteImages() bool {
	switch r {
	case RoleOwner, RoleAdmin, RoleAnnotator:
		return true
	default:
		return false
	}
}

// CanManageMembers returns true when the role can invite, update, or remove members.
func (r Role) CanManageMembers() bool {
	return r == RoleOwner || r == RoleAdmin
}

type Organization struct {
	ID            uuid.UUID
	Slug          string
	Name          string
	Plan          string
	CORSAllowlist []string
	CreatedAt     time.Time
}

type User struct {
	ID            uuid.UUID
	Email         string
	EmailVerified bool
	DisplayName   string
	Locale        string
}

type Membership struct {
	OrgID  uuid.UUID
	UserID uuid.UUID
	Role   Role
}

// Caller is the authenticated principal attached to every handler context.
type Caller struct {
	UserID uuid.UUID
	OrgID  uuid.UUID
	Role   Role
}

var slugRegex = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,38}[a-z0-9])?$`)

// NormalizeSlug lowercases and trims whitespace. Returns ErrInvalidInput if
// the result fails to match the slug regex (3–40 chars, alphanumeric/hyphen,
// no leading or trailing hyphen).
func NormalizeSlug(s string) (string, error) {
	v := strings.ToLower(strings.TrimSpace(s))
	if utf8.RuneCountInString(v) < 3 || utf8.RuneCountInString(v) > 40 {
		return "", ErrInvalidInput
	}
	if !slugRegex.MatchString(v) {
		return "", ErrInvalidInput
	}
	return v, nil
}
