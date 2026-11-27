package files

import (
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"tianshu/internal/shared/apperr"
)

func TestURLSignerSignAndVerify(t *testing.T) {
	signer := newURLSigner("super-secret-signing-key-at-least-32-bytes")
	fileID := uuid.Must(uuid.NewV7())
	now := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	expiresAt := now.Add(5 * time.Minute)

	token, err := signer.Sign(fileID, DispositionInline, expiresAt)
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

	// Exact expiry timestamp boundary (now == expiresAt) remains valid; 1 second after fails.
	if _, _, err := signer.Verify(token, expiresAt); err != nil {
		t.Fatalf("Verify() at exact expiresAt error = %v, want nil", err)
	}
	_, _, err = signer.Verify(token, expiresAt.Add(time.Second))
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

func TestURLSignerRejectsMalformedTokensAndPayloads(t *testing.T) {
	signer := newURLSigner("super-secret-signing-key-at-least-32-bytes")
	now := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	validID := uuid.Must(uuid.NewV7()).String()

	signRaw := func(raw []byte) string {
		encoded := base64.RawURLEncoding.EncodeToString(raw)
		sig := base64.RawURLEncoding.EncodeToString(signer.computeMAC(encoded))
		return encoded + "." + sig
	}

	marshalPayload := func(p signedPayload) []byte {
		t.Helper()
		b, err := json.Marshal(p)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}
		return b
	}

	validTok, err := signer.Sign(uuid.MustParse(validID), DispositionAttachment, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	cases := []struct {
		name  string
		token string
	}{
		{name: "empty token", token: ""},
		{name: "whitespace only", token: "   "},
		{name: "missing dot separator", token: "nodotseparator"},
		{name: "empty payload", token: ".signature"},
		{name: "empty signature", token: "payload."},
		{name: "extra dot separator", token: validTok + ".extra"},
		{name: "invalid signature base64", token: "abc.!!!notbase64!!!"},
		{name: "tampered signature", token: validTok + "x"},
		{name: "signed non-base64 payload", token: func() string {
			badPayload := "!!!not-base64!!!"
			sig := base64.RawURLEncoding.EncodeToString(signer.computeMAC(badPayload))
			return badPayload + "." + sig
		}()},
		{name: "signed invalid json", token: signRaw([]byte(`{not-json`))},
		{name: "zero exp", token: signRaw(marshalPayload(signedPayload{FileID: validID, Disposition: DispositionInline, ExpiresAt: 0}))},
		{name: "negative exp", token: signRaw(marshalPayload(signedPayload{FileID: validID, Disposition: DispositionInline, ExpiresAt: -10}))},
		{name: "nil uuid", token: signRaw(marshalPayload(signedPayload{FileID: uuid.Nil.String(), Disposition: DispositionInline, ExpiresAt: now.Add(time.Minute).Unix()}))},
		{name: "malformed uuid", token: signRaw(marshalPayload(signedPayload{FileID: "not-a-uuid", Disposition: DispositionInline, ExpiresAt: now.Add(time.Minute).Unix()}))},
		{name: "invalid disposition", token: signRaw(marshalPayload(signedPayload{FileID: validID, Disposition: "preview", ExpiresAt: now.Add(time.Minute).Unix()}))},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := signer.Verify(tc.token, now)
			appErr, ok := apperr.As(err)
			if !ok || appErr.Code != ErrCodeInvalidSignedURL {
				t.Fatalf("Verify(%q) error = %v, want %s", tc.token, err, ErrCodeInvalidSignedURL)
			}
		})
	}
}

func TestURLSignerFailsClosedOnEmptyOrWeakSecret(t *testing.T) {
	fileID := uuid.Must(uuid.NewV7())
	now := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)

	for _, secret := range []string{"", "   ", "too-short-secret"} {
		signer := newURLSigner(secret)
		if _, err := signer.Sign(fileID, DispositionInline, now.Add(time.Minute)); err == nil {
			t.Fatalf("Sign() with secret %q expected error, got nil", secret)
		}
		_, _, err := signer.Verify("abc.def", now)
		appErr, ok := apperr.As(err)
		if !ok || appErr.Code != ErrCodeInvalidSignedURL {
			t.Fatalf("Verify() with secret %q error = %v, want %s", secret, err, ErrCodeInvalidSignedURL)
		}
	}
}
