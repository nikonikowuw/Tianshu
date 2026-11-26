package security

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

const (
	// MinPasswordLength is the minimum allowed password length in Unicode runes.
	MinPasswordLength = 12
	// MaxPasswordLength is the maximum allowed password length in Unicode runes.
	MaxPasswordLength = 128

	// DefaultArgon2MemoryKiB is 64 MiB per security baseline.
	DefaultArgon2MemoryKiB uint32 = 64 * 1024
	// DefaultArgon2Iterations is the baseline Argon2id time cost.
	DefaultArgon2Iterations uint32 = 3
	// DefaultArgon2Parallelism is the baseline Argon2id lane count.
	DefaultArgon2Parallelism uint8 = 2
	// DefaultArgon2SaltLen is the baseline random salt length in bytes.
	DefaultArgon2SaltLen uint32 = 16
	// DefaultArgon2KeyLen is the baseline derived key length in bytes.
	DefaultArgon2KeyLen uint32 = 32
)

var (
	// ErrInvalidPasswordLength indicates a password outside the 12-128 rune bounds.
	ErrInvalidPasswordLength = errors.New("password length out of allowed range")
	// ErrInvalidHashFormat indicates a malformed or unsupported PHC argon2id hash string.
	ErrInvalidHashFormat = errors.New("invalid password hash format")
)

// Argon2Params configures Argon2id key derivation parameters.
type Argon2Params struct {
	MemoryKiB   uint32
	Iterations  uint32
	Parallelism uint8
	SaltLen     uint32
	KeyLen      uint32
}

// DefaultArgon2Params returns the security baseline Argon2id parameters.
func DefaultArgon2Params() Argon2Params {
	return Argon2Params{
		MemoryKiB:   DefaultArgon2MemoryKiB,
		Iterations:  DefaultArgon2Iterations,
		Parallelism: DefaultArgon2Parallelism,
		SaltLen:     DefaultArgon2SaltLen,
		KeyLen:      DefaultArgon2KeyLen,
	}
}

// PasswordHasher hashes and verifies user passwords using Argon2id PHC format.
type PasswordHasher struct {
	params    Argon2Params
	dummyHash string
}

// NewPasswordHasher creates a PasswordHasher with the given Argon2id parameters.
// Zero-valued fields fall back to DefaultArgon2Params.
func NewPasswordHasher(params Argon2Params) (*PasswordHasher, error) {
	defaults := DefaultArgon2Params()
	if params.MemoryKiB == 0 {
		params.MemoryKiB = defaults.MemoryKiB
	}
	if params.Iterations == 0 {
		params.Iterations = defaults.Iterations
	}
	if params.Parallelism == 0 {
		params.Parallelism = defaults.Parallelism
	}
	if params.SaltLen == 0 {
		params.SaltLen = defaults.SaltLen
	}
	if params.KeyLen == 0 {
		params.KeyLen = defaults.KeyLen
	}

	h := &PasswordHasher{params: params}
	dummy, err := h.hashRaw("tianshu-dummy-password-for-timing-equalization")
	if err != nil {
		return nil, fmt.Errorf("init dummy password hash: %w", err)
	}
	h.dummyHash = dummy
	return h, nil
}

// ValidatePasswordLength checks that password length is within 12-128 Unicode runes.
func ValidatePasswordLength(password string) error {
	n := utf8.RuneCountInString(password)
	if n < MinPasswordLength || n > MaxPasswordLength {
		return ErrInvalidPasswordLength
	}
	return nil
}

// Hash validates password length and returns a PHC-encoded Argon2id hash string.
func (h *PasswordHasher) Hash(password string) (string, error) {
	if err := ValidatePasswordLength(password); err != nil {
		return "", err
	}
	return h.hashRaw(password)
}

func (h *PasswordHasher) hashRaw(password string) (string, error) {
	salt := make([]byte, h.params.SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}

	derived := argon2.IDKey(
		[]byte(password),
		salt,
		h.params.Iterations,
		h.params.MemoryKiB,
		h.params.Parallelism,
		h.params.KeyLen,
	)

	encoded := fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		h.params.MemoryKiB,
		h.params.Iterations,
		h.params.Parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(derived),
	)
	return encoded, nil
}

// Verify compares password against a PHC-encoded Argon2id hash in constant time.
// It returns matched=true when valid and needsRehash=true when stored parameters differ from current config.
func (h *PasswordHasher) Verify(password, encodedHash string) (matched bool, needsRehash bool, err error) {
	parsed, salt, expectedKey, err := decodePHC(encodedHash)
	if err != nil {
		return false, false, err
	}

	actualKey := argon2.IDKey(
		[]byte(password),
		salt,
		parsed.Iterations,
		parsed.MemoryKiB,
		parsed.Parallelism,
		parsed.KeyLen,
	)

	if subtle.ConstantTimeCompare(actualKey, expectedKey) != 1 {
		return false, false, nil
	}

	rehash := parsed.MemoryKiB != h.params.MemoryKiB ||
		parsed.Iterations != h.params.Iterations ||
		parsed.Parallelism != h.params.Parallelism ||
		parsed.KeyLen != h.params.KeyLen ||
		uint32(len(salt)) != h.params.SaltLen

	return true, rehash, nil
}

// VerifyDummy executes a real Argon2id verification against a precomputed dummy hash
// to reduce timing side-channel differences when a username does not exist.
func (h *PasswordHasher) VerifyDummy(password string) {
	_, _, _ = h.Verify(password, h.dummyHash)
}

func decodePHC(encoded string) (Argon2Params, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return Argon2Params{}, nil, nil, ErrInvalidHashFormat
	}

	if !strings.HasPrefix(parts[2], "v=") {
		return Argon2Params{}, nil, nil, ErrInvalidHashFormat
	}
	ver, err := strconv.Atoi(strings.TrimPrefix(parts[2], "v="))
	if err != nil || ver != argon2.Version {
		return Argon2Params{}, nil, nil, ErrInvalidHashFormat
	}

	var (
		mem, iter, par      uint64
		seenM, seenT, seenP bool
	)
	for item := range strings.SplitSeq(parts[3], ",") {
		k, v, ok := strings.Cut(item, "=")
		if !ok {
			return Argon2Params{}, nil, nil, ErrInvalidHashFormat
		}
		switch k {
		case "m":
			if seenM {
				return Argon2Params{}, nil, nil, ErrInvalidHashFormat
			}
			seenM = true
			mem, err = strconv.ParseUint(v, 10, 32)
		case "t":
			if seenT {
				return Argon2Params{}, nil, nil, ErrInvalidHashFormat
			}
			seenT = true
			iter, err = strconv.ParseUint(v, 10, 32)
		case "p":
			if seenP {
				return Argon2Params{}, nil, nil, ErrInvalidHashFormat
			}
			seenP = true
			par, err = strconv.ParseUint(v, 10, 8)
		default:
			return Argon2Params{}, nil, nil, ErrInvalidHashFormat
		}
		if err != nil {
			return Argon2Params{}, nil, nil, ErrInvalidHashFormat
		}
	}
	if !seenM || !seenT || !seenP || mem == 0 || mem > 1024*1024 || iter == 0 || iter > 20 || par == 0 || par > 16 {
		return Argon2Params{}, nil, nil, ErrInvalidHashFormat
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) == 0 || len(salt) > 128 {
		return Argon2Params{}, nil, nil, ErrInvalidHashFormat
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(key) == 0 || len(key) > 128 {
		return Argon2Params{}, nil, nil, ErrInvalidHashFormat
	}

	return Argon2Params{
		MemoryKiB:   uint32(mem),
		Iterations:  uint32(iter),
		Parallelism: uint8(par),
		SaltLen:     uint32(len(salt)),
		KeyLen:      uint32(len(key)),
	}, salt, key, nil
}
