// Package auth bridges Better Auth's Postgres-managed session table into
// the API's authentication layer (ADR-0003).
//
// Cookie format produced by `better-call`'s setSignedCookie helper:
//
//	urlEncode( token + "." + base64(HMAC-SHA-256(secret, token)) )
//
// `base64` is the standard alphabet (with padding), not URL-safe — the
// outer URL-encode escapes `+`, `/`, `=` to `%2B`, `%2F`, `%3D`. We
// reverse that here, verify the HMAC, then look up the unsigned token
// in `better_auth_session`.
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/visionloop/api/internal/domain"
)

type SessionLookup struct {
	pool   *pgxpool.Pool
	secret []byte
}

// NewSessionLookup binds the lookup to the shared Better Auth secret.
// `secret` must match the web app's BETTER_AUTH_SECRET exactly; mismatch
// rejects every cookie and the boot-time check in config.go raises the
// alarm.
func NewSessionLookup(pool *pgxpool.Pool, secret string) *SessionLookup {
	return &SessionLookup{pool: pool, secret: []byte(secret)}
}

func (s *SessionLookup) LookupSession(ctx context.Context, signedCookieValue string) (uuid.UUID, time.Time, error) {
	token, ok := s.unwrap(signedCookieValue)
	if !ok {
		return uuid.Nil, time.Time{}, domain.ErrUnauthorized
	}

	const q = `
SELECT user_id, expires_at
FROM better_auth_session
WHERE token = $1 AND expires_at > now()`
	var (
		userID    uuid.UUID
		expiresAt time.Time
	)
	err := s.pool.QueryRow(ctx, q, token).Scan(&userID, &expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, time.Time{}, domain.ErrUnauthorized
	}
	if err != nil {
		return uuid.Nil, time.Time{}, fmt.Errorf("session lookup: %w", err)
	}
	return userID, expiresAt, nil
}

// unwrap parses Better Auth's signed cookie and returns the unsigned
// session token if and only if the HMAC signature verifies. ("", false)
// on any tampering or malformed value.
func (s *SessionLookup) unwrap(value string) (string, bool) {
	if value == "" || len(s.secret) == 0 {
		return "", false
	}

	// Browsers and curl forward the cookie value URL-encoded
	// (`+`→`%2B`, `/`→`%2F`, `=`→`%3D`). PathUnescape decodes %XX
	// sequences but, unlike QueryUnescape, leaves bare `+` alone — the
	// signature's base64 alphabet contains `+` and we must not mangle
	// it if a proxy already decoded the cookie.
	decoded, err := url.PathUnescape(value)
	if err != nil {
		decoded = value
	}

	idx := strings.LastIndex(decoded, ".")
	if idx <= 0 || idx == len(decoded)-1 {
		return "", false
	}
	token := decoded[:idx]
	givenSig := decoded[idx+1:]

	expected := computeSignature(s.secret, token)
	if subtle.ConstantTimeCompare([]byte(givenSig), []byte(expected)) != 1 {
		return "", false
	}
	return token, true
}

func computeSignature(secret []byte, token string) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(token))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}
