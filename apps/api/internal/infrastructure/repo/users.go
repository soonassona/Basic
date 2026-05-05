package repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/visionloop/api/internal/domain"
)

type UserDirectory struct {
	pool *pgxpool.Pool
}

func NewUserDirectory(pool *pgxpool.Pool) *UserDirectory {
	return &UserDirectory{pool: pool}
}

func (d *UserDirectory) GetUser(ctx context.Context, id uuid.UUID) (domain.User, error) {
	const q = `
SELECT id, email, email_verified, COALESCE(display_name, ''), COALESCE(locale, 'en')
FROM users
WHERE id = $1 AND deleted_at IS NULL`
	var u domain.User
	err := d.pool.QueryRow(ctx, q, id).Scan(&u.ID, &u.Email, &u.EmailVerified, &u.DisplayName, &u.Locale)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.User{}, fmt.Errorf("get user: %w", err)
	}
	return u, nil
}

// PrimaryMembership returns the oldest membership for the given user. v1
// supports a single active org per user; multi-org switching arrives later.
func (d *UserDirectory) PrimaryMembership(ctx context.Context, userID uuid.UUID) (domain.Membership, error) {
	const q = `
SELECT org_id, user_id, role
FROM memberships
WHERE user_id = $1
ORDER BY created_at ASC
LIMIT 1`
	var m domain.Membership
	var role string
	err := d.pool.QueryRow(ctx, q, userID).Scan(&m.OrgID, &m.UserID, &role)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Membership{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Membership{}, fmt.Errorf("primary membership: %w", err)
	}
	m.Role = domain.Role(role)
	return m, nil
}
