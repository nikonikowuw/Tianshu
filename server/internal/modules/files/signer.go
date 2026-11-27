package files

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
)

const signerDomainLabel = "tianshu:files:signed-url:v1"

type signedPayload struct {
	FileID      string `json:"fid"`
	Disposition string `json:"disp"`
	ExpiresAt   int64  `json:"exp"`
}

type urlSigner struct {
	key []byte
}

func newURLSigner(secret string) *urlSigner {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(signerDomainLabel))
	return &urlSigner{key: mac.Sum(nil)}
}

func (s *urlSigner) Sign(fileID uuid.UUID, disposition string, expiresAt time.Time) (string, error) {
	raw, err := json.Marshal(signedPayload{
		FileID:      fileID.String(),
		Disposition: disposition,
		ExpiresAt:   expiresAt.UTC().Unix(),
	})
	if err != nil {
		return "", err
	}
	encodedPayload := base64.RawURLEncoding.EncodeToString(raw)
	sig := s.computeMAC(encodedPayload)
	return encodedPayload + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func (s *urlSigner) Verify(token string, now time.Time) (uuid.UUID, string, error) {
	encodedPayload, encodedSig, ok := strings.Cut(strings.TrimSpace(token), ".")
	if !ok || encodedPayload == "" || encodedSig == "" || strings.Contains(encodedSig, ".") {
		return uuid.Nil, "", errInvalidSignedURL()
	}

	gotSig, err := base64.RawURLEncoding.DecodeString(encodedSig)
	if err != nil {
		return uuid.Nil, "", errInvalidSignedURL()
	}
	expectedSig := s.computeMAC(encodedPayload)
	if !hmac.Equal(gotSig, expectedSig) {
		return uuid.Nil, "", errInvalidSignedURL()
	}

	rawPayload, err := base64.RawURLEncoding.DecodeString(encodedPayload)
	if err != nil {
		return uuid.Nil, "", errInvalidSignedURL()
	}
	var payload signedPayload
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		return uuid.Nil, "", errInvalidSignedURL()
	}
	if payload.ExpiresAt <= 0 || now.UTC().Unix() > payload.ExpiresAt {
		return uuid.Nil, "", errInvalidSignedURL()
	}
	fileID, err := uuid.Parse(payload.FileID)
	if err != nil || fileID == uuid.Nil {
		return uuid.Nil, "", errInvalidSignedURL()
	}
	if payload.Disposition != DispositionInline && payload.Disposition != DispositionAttachment {
		return uuid.Nil, "", errInvalidSignedURL()
	}
	return fileID, payload.Disposition, nil
}

func (s *urlSigner) computeMAC(encodedPayload string) []byte {
	mac := hmac.New(sha256.New, s.key)
	_, _ = mac.Write([]byte(encodedPayload))
	return mac.Sum(nil)
}
