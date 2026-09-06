package localcredential

import (
	"errors"
	"strings"
	"testing"
)

func TestYU32HPasswordCountsCodePointsAndPreservesBytes(t *testing.T) {
	for _, password := range []string{"", "short", strings.Repeat("a", 14), strings.Repeat("界", 14), strings.Repeat("😀", 14), string([]byte{0xff, 0xfe}), strings.Repeat("x", 4097)} {
		if !errors.Is(ValidateNewPassword([]byte(password)), ErrInvalidPassword) {
			t.Fatal("invalid length/encoding accepted")
		}
	}
	for _, password := range []string{strings.Repeat("a", 15), strings.Repeat("界", 15), strings.Repeat("😀", 15), strings.Repeat("x", 64), strings.Repeat("x", 4096), "  meaningful passphrase  "} {
		if err := ValidateNewPassword([]byte(password)); err != nil {
			t.Fatal("valid passphrase rejected")
		}
	}
	db := migratedDatabase(t)
	seedOrganizationAndUser(t, db, "org-a", "user-a")
	repo, err := NewSQLiteRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	secret := []byte("  byte-exact passphrase  ")
	if _, err := repo.SetPassword(t.Context(), "org-a", "user-a", secret, 0); err != nil {
		t.Fatal(err)
	}
	v, err := repo.VerifyPassword(t.Context(), "org-a", "user-a", secret)
	if err != nil || !v.Match {
		t.Fatal("exact password did not match")
	}
	v, err = repo.VerifyPassword(t.Context(), "org-a", "user-a", []byte(strings.TrimSpace(string(secret))))
	if err != nil || v.Match {
		t.Fatal("password was silently trimmed")
	}
}

func TestYU32HLegacyWeakCredentialVerifiesAndOnlySameSecretCanRehash(t *testing.T) {
	db := migratedDatabase(t)
	seedOrganizationAndUser(t, db, "org-a", "user-a")
	repo, err := NewSQLiteRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	// Reconstruct a historical credential with the private storage primitive.
	// Public enrollment must NOT expose this bypass.
	if _, err := repo.setPassword(t.Context(), "org-a", "user-a", []byte("legacy"), 0); err != nil {
		t.Fatal(err)
	}
	old := DefaultPolicy()
	next := old
	next.PolicyVersion = 2
	policies, err := NewPolicySet(2, old, next)
	if err != nil {
		t.Fatal(err)
	}
	upgraded, err := NewSQLiteRepository(db, WithPolicySet(policies))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := upgraded.SetPassword(t.Context(), "org-a", "user-a", []byte("legacy"), 1); !errors.Is(err, ErrInvalidPassword) {
		t.Fatal("weak enrollment bypass")
	}
	if _, err := upgraded.RehashPassword(t.Context(), "org-a", "user-a", []byte("forged"), 1); !errors.Is(err, ErrInvalidPassword) {
		t.Fatal("unauthenticated rehash bypass")
	}
	if _, err := upgraded.RehashPassword(t.Context(), "org-a", "user-a", []byte("legacy"), 9); !errors.Is(err, ErrRevisionConflict) {
		t.Fatal("stale rehash bypass")
	}
	metadata, err := upgraded.RehashPassword(t.Context(), "org-a", "user-a", []byte("legacy"), 1)
	if err != nil || metadata.PolicyVersion != 2 || metadata.Revision != 2 {
		t.Fatalf("legacy same-secret rehash: %#v %v", metadata, err)
	}
	v, err := upgraded.VerifyPassword(t.Context(), "org-a", "user-a", []byte("legacy"))
	if err != nil || !v.Match || v.NeedsRehash {
		t.Fatal("legacy verification changed")
	}
	if _, err := upgraded.RehashPassword(t.Context(), "org-a", "user-a", []byte("legacy"), 2); !errors.Is(err, ErrUnsupportedCredential) {
		t.Fatal("rehash may not bypass current enrollment policy")
	}
}
