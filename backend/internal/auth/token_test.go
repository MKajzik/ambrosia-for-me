package auth_test

import (
	"encoding/base64"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/InzKazik/mealplanner/backend/internal/auth"
)

var testSecret = []byte("0123456789abcdef0123456789abcdef") // 32 bytes

func newIssuer(now func() time.Time) *auth.TokenIssuer {
	return auth.NewTokenIssuer(testSecret, 15*time.Minute, now)
}

func TestAccessTokenRoundTrip(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	iss := newIssuer(func() time.Time { return now })
	id := uuid.New()

	token, ttl, err := iss.IssueAccess(id)
	if err != nil {
		t.Fatalf("IssueAccess: %v", err)
	}
	if ttl != 15*time.Minute {
		t.Errorf("ttl = %v, want 15m", ttl)
	}

	got, err := iss.ParseAccess(token)
	if err != nil {
		t.Fatalf("ParseAccess: %v", err)
	}
	if got != id {
		t.Errorf("ParseAccess subject = %v, want %v", got, id)
	}
}

func TestParseAccessRejectsBadTokens(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	iss := newIssuer(func() time.Time { return now })
	id := uuid.New()
	good, _, _ := iss.IssueAccess(id)

	sign := func(method jwt.SigningMethod, key any, claims jwt.RegisteredClaims) string {
		s, err := jwt.NewWithClaims(method, claims).SignedString(key)
		if err != nil {
			t.Fatalf("sign: %v", err)
		}
		return s
	}
	valid := jwt.RegisteredClaims{
		Issuer: "mealplanner", Subject: id.String(),
		IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(time.Minute)),
	}
	expired := valid
	expired.ExpiresAt = jwt.NewNumericDate(now.Add(-time.Second))
	noExpiry := valid
	noExpiry.ExpiresAt = nil
	wrongIssuer := valid
	wrongIssuer.Issuer = "someone-else"
	badSubject := valid
	badSubject.Subject = "not-a-uuid"

	tests := []struct {
		name  string
		token string
	}{
		{"empty", ""},
		{"garbage", "not.a.jwt"},
		{"tampered signature", good[:len(good)-2] + "xx"},
		{"expired", sign(jwt.SigningMethodHS256, testSecret, expired)},
		{"no expiry", sign(jwt.SigningMethodHS256, testSecret, noExpiry)},
		{"wrong issuer", sign(jwt.SigningMethodHS256, testSecret, wrongIssuer)},
		{"subject not a uuid", sign(jwt.SigningMethodHS256, testSecret, badSubject)},
		{"signed with another key", sign(jwt.SigningMethodHS256, []byte("another-secret-another-secret-00"), valid)},
		{"alg none", sign(jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType, valid)},
		{"wrong algorithm HS512", sign(jwt.SigningMethodHS512, testSecret, valid)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := iss.ParseAccess(tt.token); err == nil {
				t.Errorf("ParseAccess(%q) succeeded, want an error", tt.name)
			}
		})
	}
}

func TestParseAccessRejectsExpiredTokenAfterTimePasses(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	clock := now
	iss := newIssuer(func() time.Time { return clock })
	token, _, _ := iss.IssueAccess(uuid.New())

	clock = now.Add(15*time.Minute + time.Second)

	if _, err := iss.ParseAccess(token); err == nil {
		t.Error("token accepted after its lifetime, want an error")
	}
}

func TestNewRefreshTokenIsRandomAndHashable(t *testing.T) {
	raw1, hash1, err := auth.NewRefreshToken()
	if err != nil {
		t.Fatalf("NewRefreshToken: %v", err)
	}
	raw2, _, _ := auth.NewRefreshToken()

	if raw1 == raw2 {
		t.Error("two refresh tokens are identical")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw1)
	if err != nil || len(decoded) != 32 {
		t.Errorf("raw token = %q: want 32 random bytes in base64url (decode err %v, len %d)", raw1, err, len(decoded))
	}
	if string(auth.HashRefreshToken(raw1)) != string(hash1) {
		t.Error("HashRefreshToken(raw) differs from the hash returned with the token")
	}
	if len(hash1) != 32 {
		t.Errorf("hash length = %d, want 32 (SHA-256)", len(hash1))
	}
}

func TestNewTokenIssuerPanicsOnShortSecret(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("NewTokenIssuer accepted a secret shorter than 32 bytes, want a panic")
		}
	}()
	auth.NewTokenIssuer([]byte("too short"), time.Minute, time.Now)
}
