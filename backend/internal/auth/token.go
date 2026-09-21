package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	issuer = "mealplanner"
	// MinSecretLength is the shortest accepted HS256 signing secret, in bytes.
	MinSecretLength = 32
)

// ErrInvalidAccessToken is returned by ParseAccess for any token that is not a
// currently valid access token. Callers must not distinguish the reason.
var ErrInvalidAccessToken = errors.New("invalid access token")

// TokenIssuer issues and parses short-lived HS256 access tokens.
type TokenIssuer struct {
	secret []byte
	ttl    time.Duration
	now    func() time.Time
}

// NewTokenIssuer returns an issuer. It panics when secret is shorter than
// MinSecretLength: configuration is validated before this is reached.
func NewTokenIssuer(secret []byte, ttl time.Duration, now func() time.Time) *TokenIssuer {
	if len(secret) < MinSecretLength {
		panic(fmt.Sprintf("auth: token secret must be at least %d bytes", MinSecretLength))
	}
	return &TokenIssuer{secret: secret, ttl: ttl, now: now}
}

// IssueAccess returns a signed access token for userID and its lifetime.
func (t *TokenIssuer) IssueAccess(userID uuid.UUID) (string, time.Duration, error) {
	now := t.now()
	claims := jwt.RegisteredClaims{
		Issuer:    issuer,
		Subject:   userID.String(),
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(t.ttl)),
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(t.secret)
	if err != nil {
		return "", 0, fmt.Errorf("sign access token: %w", err)
	}
	return signed, t.ttl, nil
}

// ParseAccess validates token (signature, algorithm, issuer, expiry) and
// returns the user ID it was issued for.
func (t *TokenIssuer) ParseAccess(token string) (uuid.UUID, error) {
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(issuer),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(t.now),
	)
	claims := &jwt.RegisteredClaims{}
	_, err := parser.ParseWithClaims(token, claims, func(*jwt.Token) (any, error) { return t.secret, nil })
	if err != nil {
		return uuid.Nil, ErrInvalidAccessToken
	}
	id, err := uuid.Parse(claims.Subject)
	if err != nil {
		return uuid.Nil, ErrInvalidAccessToken
	}
	return id, nil
}

// NewRefreshToken returns a random opaque refresh token (32 bytes,
// base64url) and the SHA-256 hash that is stored in the database.
func NewRefreshToken() (raw string, hash []byte, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", nil, fmt.Errorf("generate refresh token: %w", err)
	}
	raw = base64.RawURLEncoding.EncodeToString(b)
	return raw, HashRefreshToken(raw), nil
}

// HashRefreshToken returns the SHA-256 hash under which a refresh token is stored.
func HashRefreshToken(raw string) []byte {
	sum := sha256.Sum256([]byte(raw))
	return sum[:]
}
