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
	mgr, err := security.NewJWTManager(testJWTSecret, 15*time.Minute)
	if err != nil {
		t.Fatalf("NewJWTManager() error = %v", err)
	}

	userID := uuid.Must(uuid.NewV7())
	sessionID := uuid.Must(uuid.NewV7())

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
	otherMgr, _ := security.NewJWTManager("different-secret-at-least-32-bytes-long!!", 15*time.Minute)
	if _, err := otherMgr.Verify(token); !errors.Is(err, security.ErrInvalidToken) {
		t.Fatalf("wrong secret error = %v, want ErrInvalidToken", err)
	}
}
