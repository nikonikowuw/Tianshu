package security_test

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"tianshu/internal/platform/security"
)

const testJWTSecret = "unit-test-secret-must-be-at-least-32-bytes"

func TestJWTManagerSignAndVerify(t *testing.T) {
	if _, err := security.NewJWTManager("short-secret", time.Minute); err == nil {
		t.Fatal("expected error for JWT secret shorter than 32 bytes")
	}

	mgr, err := security.NewJWTManager(testJWTSecret, 15*time.Minute)
	if err != nil {
		t.Fatalf("NewJWTManager() error = %v", err)
	}

	userID := uuid.Must(uuid.NewV7())
	sessionID := uuid.Must(uuid.NewV7())

	if _, _, err := mgr.Sign(uuid.Nil, sessionID); err == nil {
		t.Fatal("expected error when signing nil userID")
	}
	if _, _, err := mgr.Sign(userID, uuid.Nil); err == nil {
		t.Fatal("expected error when signing nil sessionID")
	}

	token, exp, err := mgr.Sign(userID, sessionID)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	if exp.Before(time.Now()) {
		t.Fatalf("expected future expiration, got %v", exp)
	}

	claims, err := mgr.Verify(token)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if claims.UserID != userID || claims.SessionID != sessionID {
		t.Fatalf("claims = %+v, want user=%s session=%s", claims, userID, sessionID)
	}

	// Malformed token segment counts must fail.
	for _, malformed := range []string{"", "one", "one.two", "a.b.c.d"} {
		if _, err := mgr.Verify(malformed); !errors.Is(err, security.ErrInvalidToken) {
			t.Fatalf("malformed token %q error = %v, want ErrInvalidToken", malformed, err)
		}
	}

	// Tampered signature must fail.
	parts := strings.Split(token, ".")
	tampered := parts[0] + "." + parts[1] + ".AAAA"
	if _, err := mgr.Verify(tampered); !errors.Is(err, security.ErrInvalidToken) {
		t.Fatalf("tampered token error = %v, want ErrInvalidToken", err)
	}

	// alg=none header must always be rejected.
	noneHeader := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	if _, err := mgr.Verify(noneHeader + "." + parts[1] + "."); !errors.Is(err, security.ErrInvalidToken) {
		t.Fatalf("alg=none token error = %v, want ErrInvalidToken", err)
	}

	// Token signed with another secret must fail.
	otherMgr, err := security.NewJWTManager("different-secret-at-least-32-bytes-long!!", 15*time.Minute)
	if err != nil {
		t.Fatalf("NewJWTManager(other) error = %v", err)
	}
	if _, err := otherMgr.Verify(token); !errors.Is(err, security.ErrInvalidToken) {
		t.Fatalf("wrong secret error = %v, want ErrInvalidToken", err)
	}

	// Wrong issuer/audience claims must be rejected even if signed with the same secret? (Verified internally too, here ensure non-JSON payload fails).
	badPayload := base64.RawURLEncoding.EncodeToString([]byte(`not-json`))
	if _, err := mgr.Verify(parts[0] + "." + badPayload + "." + parts[2]); !errors.Is(err, security.ErrInvalidToken) {
		t.Fatalf("invalid payload error = %v, want ErrInvalidToken", err)
	}
}
