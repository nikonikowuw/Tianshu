package files

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"tianshu/internal/shared/apperr"
)

func TestURLSignerSignAndVerify(t *testing.T) {
	signer := newURLSigner("super-secret-signing-key-at-least-32-bytes")
	fileID := uuid.Must(uuid.NewV7())
	now := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)

	token, err := signer.Sign(fileID, DispositionInline, now.Add(5*time.Minute))
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}

	gotID, gotDisp, err := signer.Verify(token, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if gotID != fileID || gotDisp != DispositionInline {
		t.Fatalf("Verify() = (%s, %q), want (%s, %q)", gotID, gotDisp, fileID, DispositionInline)
	}

	// Tampered token fails verification.
	if _, _, err := signer.Verify(token+"x", now); err == nil {
		t.Fatal("expected tampered token to fail")
	}

	// Expired token fails verification.
	_, _, err = signer.Verify(token, now.Add(6*time.Minute))
	appErr, ok := apperr.As(err)
	if !ok || appErr.Code != ErrCodeInvalidSignedURL {
		t.Fatalf("expired token error = %v, want %s", err, ErrCodeInvalidSignedURL)
	}

	// Different secret fails verification.
	other := newURLSigner("different-secret-key-at-least-32-bytes")
	if _, _, err := other.Verify(token, now); err == nil {
		t.Fatal("expected token verified with wrong secret to fail")
	}
}
