package security

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestJWTManagerClockSkewExpiryAndClaimsValidation(t *testing.T) {
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	mgr, err := NewJWTManager("internal-jwt-test-secret-at-least-32-bytes!", 0) // ttl<=0 defaults to 15m
	if err != nil {
		t.Fatalf("NewJWTManager() error = %v", err)
	}
	mgr.now = func() time.Time { return now }

	userID := uuid.Must(uuid.NewV7())
	sessionID := uuid.Must(uuid.NewV7())
	token, exp, err := mgr.Sign(userID, sessionID)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	if !exp.Equal(now.Add(15 * time.Minute)) {
		t.Fatalf("default TTL exp = %v, want %v", exp, now.Add(15*time.Minute))
	}

	// Within 30s clock skew after exp must still verify.
	now = exp.Add(jwtClockSkew)
	if _, err := mgr.Verify(token); err != nil {
		t.Fatalf("Verify() at clock skew boundary error = %v", err)
	}

	// Past exp + 30s clock skew must return ErrExpiredToken.
	now = exp.Add(jwtClockSkew + time.Second)
	if _, err := mgr.Verify(token); !errors.Is(err, ErrExpiredToken) {
		t.Fatalf("Verify() after expiry error = %v, want ErrExpiredToken", err)
	}

	// Helper to sign arbitrary payload with mgr's HS256 secret.
	signRawPayload := func(p jwtPayload) string {
		hdr, _ := json.Marshal(jwtHeader{Alg: jwtAlgorithmHS256, Typ: "JWT"})
		pl, _ := json.Marshal(p)
		input := base64.RawURLEncoding.EncodeToString(hdr) + "." + base64.RawURLEncoding.EncodeToString(pl)
		return input + "." + base64.RawURLEncoding.EncodeToString(mgr.computeMAC(input))
	}

	now = time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name    string
		payload jwtPayload
	}{
		{
			name:    "wrong issuer",
			payload: jwtPayload{Sub: userID.String(), Sid: sessionID.String(), Iss: "other", Aud: jwtAudience, Iat: now.Unix(), Exp: now.Add(time.Minute).Unix()},
		},
		{
			name:    "wrong audience",
			payload: jwtPayload{Sub: userID.String(), Sid: sessionID.String(), Iss: jwtIssuer, Aud: "other-aud", Iat: now.Unix(), Exp: now.Add(time.Minute).Unix()},
		},
		{
			name:    "nil user UUID",
			payload: jwtPayload{Sub: uuid.Nil.String(), Sid: sessionID.String(), Iss: jwtIssuer, Aud: jwtAudience, Iat: now.Unix(), Exp: now.Add(time.Minute).Unix()},
		},
		{
			name:    "invalid session UUID",
			payload: jwtPayload{Sub: userID.String(), Sid: "not-a-uuid", Iss: jwtIssuer, Aud: jwtAudience, Iat: now.Unix(), Exp: now.Add(time.Minute).Unix()},
		},
		{
			name:    "future iat beyond skew",
			payload: jwtPayload{Sub: userID.String(), Sid: sessionID.String(), Iss: jwtIssuer, Aud: jwtAudience, Iat: now.Add(jwtClockSkew + 5*time.Second).Unix(), Exp: now.Add(time.Minute).Unix()},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := signRawPayload(tc.payload)
			if _, err := mgr.Verify(raw); !errors.Is(err, ErrInvalidToken) {
				t.Fatalf("Verify(%s) error = %v, want ErrInvalidToken", tc.name, err)
			}
		})
	}
}
