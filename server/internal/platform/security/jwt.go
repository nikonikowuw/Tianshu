package security

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	jwtAlgorithmHS256 = "HS256"
	jwtIssuer         = "tianshu"
	jwtAudience       = "tianshu-web"
	jwtClockSkew      = 30 * time.Second
)

var (
	// ErrInvalidToken indicates a malformed, tampered, or unsupported JWT access token.
	ErrInvalidToken = errors.New("invalid access token")
	// ErrExpiredToken indicates an expired JWT access token beyond allowed clock skew.
	ErrExpiredToken = errors.New("access token expired")
)

// AccessTokenClaims holds the verified claims of a Tianshu HS256 access token.
// Roles and permissions are intentionally excluded so RBAC updates take effect immediately.
type AccessTokenClaims struct {
	UserID    uuid.UUID
	SessionID uuid.UUID
	IssuedAt  time.Time
	ExpiresAt time.Time
}

type jwtHeader struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
}

type jwtPayload struct {
	Sub string `json:"sub"`
	Sid string `json:"sid"`
	Iss string `json:"iss"`
	Aud string `json:"aud"`
	Iat int64  `json:"iat"`
	Exp int64  `json:"exp"`
}

// JWTManager signs and verifies HS256 access tokens with fixed algorithm, issuer, and audience.
type JWTManager struct {
	secret []byte
	ttl    time.Duration
	now    func() time.Time
}

// NewJWTManager constructs a JWTManager enforcing a minimum 32-byte HS256 secret.
func NewJWTManager(secret string, ttl time.Duration) (*JWTManager, error) {
	if len(secret) < 32 {
		return nil, errors.New("jwt secret must be at least 32 bytes")
	}
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	return &JWTManager{
		secret: []byte(secret),
		ttl:    ttl,
		now:    time.Now,
	}, nil
}

// Sign issues a signed HS256 JWT for userID and sessionID.
func (m *JWTManager) Sign(userID, sessionID uuid.UUID) (string, time.Time, error) {
	if userID == uuid.Nil || sessionID == uuid.Nil {
		return "", time.Time{}, errors.New("userID and sessionID must not be nil")
	}

	now := m.now().UTC()
	exp := now.Add(m.ttl)

	headerBytes, err := json.Marshal(jwtHeader{
		Alg: jwtAlgorithmHS256,
		Typ: "JWT",
	})
	if err != nil {
		return "", time.Time{}, fmt.Errorf("marshal jwt header: %w", err)
	}
	payloadBytes, err := json.Marshal(jwtPayload{
		Sub: userID.String(),
		Sid: sessionID.String(),
		Iss: jwtIssuer,
		Aud: jwtAudience,
		Iat: now.Unix(),
		Exp: exp.Unix(),
	})
	if err != nil {
		return "", time.Time{}, fmt.Errorf("marshal jwt payload: %w", err)
	}

	signingInput := base64.RawURLEncoding.EncodeToString(headerBytes) +
		"." +
		base64.RawURLEncoding.EncodeToString(payloadBytes)
	sig := m.computeMAC(signingInput)
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig), exp, nil
}

// Verify parses and validates an HS256 access token, rejecting alg=none or non-HS256 headers.
func (m *JWTManager) Verify(token string) (AccessTokenClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return AccessTokenClaims{}, ErrInvalidToken
	}

	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return AccessTokenClaims{}, ErrInvalidToken
	}
	var hdr jwtHeader
	if err := json.Unmarshal(headerBytes, &hdr); err != nil {
		return AccessTokenClaims{}, ErrInvalidToken
	}
	if hdr.Alg != jwtAlgorithmHS256 || (hdr.Typ != "" && hdr.Typ != "JWT") {
		return AccessTokenClaims{}, ErrInvalidToken
	}

	sigBytes, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return AccessTokenClaims{}, ErrInvalidToken
	}
	expectedSig := m.computeMAC(parts[0] + "." + parts[1])
	if !hmac.Equal(sigBytes, expectedSig) {
		return AccessTokenClaims{}, ErrInvalidToken
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return AccessTokenClaims{}, ErrInvalidToken
	}
	var payload jwtPayload
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return AccessTokenClaims{}, ErrInvalidToken
	}

	if payload.Iss != jwtIssuer || payload.Aud != jwtAudience {
		return AccessTokenClaims{}, ErrInvalidToken
	}

	userID, err := uuid.Parse(payload.Sub)
	if err != nil || userID == uuid.Nil {
		return AccessTokenClaims{}, ErrInvalidToken
	}
	sessionID, err := uuid.Parse(payload.Sid)
	if err != nil || sessionID == uuid.Nil {
		return AccessTokenClaims{}, ErrInvalidToken
	}

	now := m.now().UTC()
	exp := time.Unix(payload.Exp, 0).UTC()
	iat := time.Unix(payload.Iat, 0).UTC()

	if now.After(exp.Add(jwtClockSkew)) {
		return AccessTokenClaims{}, ErrExpiredToken
	}
	if iat.After(now.Add(jwtClockSkew)) {
		return AccessTokenClaims{}, ErrInvalidToken
	}

	return AccessTokenClaims{
		UserID:    userID,
		SessionID: sessionID,
		IssuedAt:  iat,
		ExpiresAt: exp,
	}, nil
}

func (m *JWTManager) computeMAC(signingInput string) []byte {
	mac := hmac.New(sha256.New, m.secret)
	_, _ = mac.Write([]byte(signingInput))
	return mac.Sum(nil)
}
