// Package auth holds the pure credential logic: password hashing and token
// issuing/parsing. It has no database or HTTP dependency.
package auth

import (
	"fmt"

	"github.com/alexedwards/argon2id"
)

// HashParams are the argon2id cost parameters.
type HashParams struct {
	MemoryKiB   uint32
	Iterations  uint32
	Parallelism uint8
}

// DefaultHashParams are the production parameters: 64 MiB, 3 passes, 2 lanes.
var DefaultHashParams = HashParams{MemoryKiB: 64 * 1024, Iterations: 3, Parallelism: 2}

// Hasher hashes and verifies passwords with argon2id.
type Hasher struct {
	params    *argon2id.Params
	dummyHash string
}

// NewHasher returns a Hasher using p.
func NewHasher(p HashParams) *Hasher {
	params := &argon2id.Params{
		Memory:      p.MemoryKiB,
		Iterations:  p.Iterations,
		Parallelism: p.Parallelism,
		SaltLength:  16,
		KeyLength:   32,
	}
	// A hash computed with the same parameters, used by Burn so that checking a
	// password for an unknown user costs the same as for a known one.
	dummy, err := argon2id.CreateHash("dummy-password-for-timing", params)
	if err != nil {
		panic(fmt.Sprintf("auth: create dummy hash: %v", err)) // only fails if the system RNG is broken
	}
	return &Hasher{params: params, dummyHash: dummy}
}

// Hash returns the PHC-encoded argon2id hash of password with a random salt.
func (h *Hasher) Hash(password string) (string, error) {
	hash, err := argon2id.CreateHash(password, h.params)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return hash, nil
}

// Verify reports whether password matches hash. It returns an error only when
// hash is malformed.
func (h *Hasher) Verify(password, hash string) (bool, error) {
	ok, err := argon2id.ComparePasswordAndHash(password, hash)
	if err != nil {
		return false, fmt.Errorf("verify password: %w", err)
	}
	return ok, nil
}

// Burn spends the same time as Verify without a real hash. Call it when the
// account does not exist, so response time does not reveal which emails exist.
func (h *Hasher) Burn(password string) {
	_, _ = argon2id.ComparePasswordAndHash(password, h.dummyHash)
}
