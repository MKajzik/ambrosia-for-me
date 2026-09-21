package auth

import (
	"testing"

	"github.com/alexedwards/argon2id"
)

// Burn must cost the same as a real Verify, so the dummy hash has to carry the
// hasher's own parameters.
func TestDummyHashUsesTheHasherParameters(t *testing.T) {
	p := HashParams{MemoryKiB: 8, Iterations: 1, Parallelism: 1}
	h := NewHasher(p)

	params, _, _, err := argon2id.DecodeHash(h.dummyHash)
	if err != nil {
		t.Fatalf("DecodeHash(dummyHash): %v", err)
	}
	if params.Memory != p.MemoryKiB || params.Iterations != p.Iterations || params.Parallelism != p.Parallelism {
		t.Errorf("dummy hash params = m=%d t=%d p=%d, want m=%d t=%d p=%d",
			params.Memory, params.Iterations, params.Parallelism, p.MemoryKiB, p.Iterations, p.Parallelism)
	}
}
