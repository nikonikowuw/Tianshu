package security_test

import (
	"testing"

	"tianshu/internal/platform/security"
)

func TestOpaqueTokenAndHash(t *testing.T) {
	tok1, err := security.GenerateOpaqueToken(0)
	if err != nil {
		t.Fatalf("GenerateOpaqueToken() error = %v", err)
	}
	tok2, err := security.GenerateOpaqueToken(32)
	if err != nil {
		t.Fatalf("GenerateOpaqueToken() error = %v", err)
	}

	if len(tok1) < 40 {
		t.Fatalf("token length = %d, want >= 40 base64url chars for 32 bytes", len(tok1))
	}
	if tok1 == tok2 {
		t.Fatal("two generated tokens collided")
	}

	h1 := security.HashTokenSHA256(tok1)
	if len(h1) != 64 {
		t.Fatalf("sha256 hex length = %d, want 64", len(h1))
	}
	if !security.ConstantTimeEqual(h1, security.HashTokenSHA256(tok1)) {
		t.Fatal("ConstantTimeEqual returned false for identical digests")
	}
	if security.ConstantTimeEqual(h1, security.HashTokenSHA256(tok2)) {
		t.Fatal("ConstantTimeEqual returned true for different digests")
	}
}
