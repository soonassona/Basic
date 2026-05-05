package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/url"
	"testing"
)

func signedCookie(secret, token string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(token))
	sig := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return url.QueryEscape(token + "." + sig)
}

func TestUnwrap_AcceptsValidCookie(t *testing.T) {
	t.Parallel()
	secret := "0123456789abcdef0123456789abcdef"
	token := "NoTgb0LxOpo2REedeO90HqR80jbZXd8T"
	s := &SessionLookup{secret: []byte(secret)}

	got, ok := s.unwrap(signedCookie(secret, token))
	if !ok {
		t.Fatal("expected signature to verify")
	}
	if got != token {
		t.Fatalf("token mismatch: %q", got)
	}
}

func TestUnwrap_RejectsTamperedSignature(t *testing.T) {
	t.Parallel()
	secret := "0123456789abcdef0123456789abcdef"
	token := "abcdefghijklmnop"
	s := &SessionLookup{secret: []byte(secret)}

	bad := signedCookie(secret, token) + "x"
	if _, ok := s.unwrap(bad); ok {
		t.Fatal("expected tampered cookie to be rejected")
	}
}

func TestUnwrap_RejectsWrongSecret(t *testing.T) {
	t.Parallel()
	good := signedCookie("right-secret", "tok")
	s := &SessionLookup{secret: []byte("wrong-secret")}
	if _, ok := s.unwrap(good); ok {
		t.Fatal("expected wrong-secret cookie to be rejected")
	}
}

func TestUnwrap_RejectsMalformed(t *testing.T) {
	t.Parallel()
	s := &SessionLookup{secret: []byte("any")}
	for _, in := range []string{"", "no-dot", ".no-token", "no-sig.", "."} {
		if _, ok := s.unwrap(in); ok {
			t.Fatalf("accepted malformed cookie %q", in)
		}
	}
}

func TestUnwrap_AcceptsAlreadyDecoded(t *testing.T) {
	// Some intermediaries pre-decode cookie values. unwrap must accept
	// either the encoded or already-decoded form.
	t.Parallel()
	secret := "s"
	token := "tok+with/special=chars"
	s := &SessionLookup{secret: []byte(secret)}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(token))
	sig := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	raw := token + "." + sig

	if _, ok := s.unwrap(raw); !ok {
		t.Fatal("expected unencoded cookie to verify")
	}
	if _, ok := s.unwrap(url.QueryEscape(raw)); !ok {
		t.Fatal("expected URL-encoded cookie to verify")
	}
}
