package security_test

import (
	"errors"
	"strings"
	"testing"

	"tianshu/internal/platform/security"
)

func fastTestParams() security.Argon2Params {
	return security.Argon2Params{
		MemoryKiB:   16 * 1024,
		Iterations:  1,
		Parallelism: 1,
		SaltLen:     16,
		KeyLen:      32,
	}
}

func TestPasswordHasherHashVerifyAndRehash(t *testing.T) {
	hasher, err := security.NewPasswordHasher(fastTestParams())
	if err != nil {
		t.Fatalf("NewPasswordHasher() error = %v", err)
	}

	raw := "correct horse battery staple 密码!"
	encoded, err := hasher.Hash(raw)
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}
	if !strings.HasPrefix(encoded, "$argon2id$v=19$m=16384,t=1,p=1$") {
		t.Fatalf("unexpected PHC format: %s", encoded)
	}

	matched, needsRehash, err := hasher.Verify(raw, encoded)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if !matched || needsRehash {
		t.Fatalf("Verify() = (%v, %v), want (true, false)", matched, needsRehash)
	}

	matchedWrong, _, err := hasher.Verify("wrong-password-value", encoded)
	if err != nil {
		t.Fatalf("Verify(wrong) error = %v", err)
	}
	if matchedWrong {
		t.Fatal("Verify(wrong) matched = true, want false")
	}

	// Upgrading parameters must signal needsRehash=true while still verifying legacy hashes.
	upgradedParams := fastTestParams()
	upgradedParams.Iterations = 2
	upgraded, err := security.NewPasswordHasher(upgradedParams)
	if err != nil {
		t.Fatalf("NewPasswordHasher(upgraded) error = %v", err)
	}

	matchedUp, rehashUp, err := upgraded.Verify(raw, encoded)
	if err != nil {
		t.Fatalf("upgraded.Verify() error = %v", err)
	}
	if !matchedUp || !rehashUp {
		t.Fatalf("upgraded.Verify() = (%v, %v), want (true, true)", matchedUp, rehashUp)
	}

	hasher.VerifyDummy("any-candidate")
}

func TestPasswordValidationAndInvalidHash(t *testing.T) {
	hasher, err := security.NewPasswordHasher(fastTestParams())
	if err != nil {
		t.Fatalf("NewPasswordHasher() error = %v", err)
	}

	if _, err := hasher.Hash("short"); !errors.Is(err, security.ErrInvalidPasswordLength) {
		t.Fatalf("short password error = %v, want ErrInvalidPasswordLength", err)
	}
	if _, err := hasher.Hash(strings.Repeat("a", 129)); !errors.Is(err, security.ErrInvalidPasswordLength) {
		t.Fatalf("overlong password error = %v, want ErrInvalidPasswordLength", err)
	}
	if _, err := hasher.Hash("十二个汉字组成的密码测试"); err != nil {
		t.Fatalf("12-rune unicode password rejected: %v", err)
	}

	for _, bad := range []string{
		"",
		"$bcrypt$10$abc",
		"$argon2id$v=99$m=1,t=1,p=1$aa$bb",
		"$argon2id$v=19$bad$aa$bb",
		"$argon2id$v=19$m=16384,m=16384,t=1,p=1$aa$bb",
		"$argon2id$v=19$m=2097152,t=1,p=1$aa$bb",
		"$argon2id$v=19$m=16384,t=1,p=32$aa$bb",
	} {
		if _, _, err := hasher.Verify("anything-1234", bad); !errors.Is(err, security.ErrInvalidHashFormat) {
			t.Errorf("Verify(%q) error = %v, want ErrInvalidHashFormat", bad, err)
		}
	}
}
