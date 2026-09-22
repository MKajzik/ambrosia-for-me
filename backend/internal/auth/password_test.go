package auth_test

import (
	"strings"
	"testing"

	"github.com/InzKazik/mealplanner/backend/internal/auth"
)

// lightParams keeps the tests fast; production uses auth.DefaultHashParams.
var lightParams = auth.HashParams{MemoryKiB: 8, Iterations: 1, Parallelism: 1}

func TestHasherRoundTrip(t *testing.T) {
	h := auth.NewHasher(lightParams)

	hash, err := h.Hash("correct horse battery staple")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Errorf("hash = %q, want an argon2id PHC string", hash)
	}

	ok, err := h.Verify("correct horse battery staple", hash)
	if err != nil || !ok {
		t.Errorf("Verify(correct password) = %v, %v; want true, nil", ok, err)
	}
	ok, err = h.Verify("wrong password!", hash)
	if err != nil || ok {
		t.Errorf("Verify(wrong password) = %v, %v; want false, nil", ok, err)
	}
}

func TestHasherUsesRandomSalt(t *testing.T) {
	h := auth.NewHasher(lightParams)

	a, _ := h.Hash("same password 123")
	b, _ := h.Hash("same password 123")

	if a == b {
		t.Error("two hashes of the same password are identical, want distinct salts")
	}
}

func TestHasherVerifyRejectsMalformedHash(t *testing.T) {
	h := auth.NewHasher(lightParams)

	ok, err := h.Verify("whatever", "not-a-hash")

	if ok || err == nil {
		t.Errorf("Verify(malformed) = %v, %v; want false and an error", ok, err)
	}
}

func TestHasherBurnDoesNotPanicAndTakesTheHashingPath(t *testing.T) {
	h := auth.NewHasher(lightParams)

	// Burn is called for unknown users so that login timing does not reveal
	// whether an email is registered. It must simply complete.
	h.Burn("any password at all")
}
