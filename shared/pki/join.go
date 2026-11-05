package pki

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	// JoinPrefix marks a join string.
	JoinPrefix = "ulj_"
	// JoinTTL is how long a join secret is valid.
	JoinTTL = 15 * time.Minute

	joinVersion    byte = 1
	joinSecretSize      = 32
	maxJoinAddrLen      = 255
)

// JoinInfo is what a join string carries: where the manager is, the
// fingerprint of its CA (so the node can authenticate the manager before
// sending anything), and the single-use secret.
type JoinInfo struct {
	Addr        string // host:port of the cluster listener
	Fingerprint [32]byte
	Secret      []byte
	ExpiresAt   time.Time // informational; the manager is authoritative
}

// NewJoinSecret returns a fresh random secret.
func NewJoinSecret() ([]byte, error) {
	s := make([]byte, joinSecretSize)
	if _, err := rand.Read(s); err != nil {
		return nil, fmt.Errorf("pki: join secret: %w", err)
	}
	return s, nil
}

// HashSecret is what the manager stores. The secret is 256 random bits, so a
// plain SHA-256 is sufficient (there is nothing to brute-force) and lets the
// manager look the row up by hash.
func HashSecret(secret []byte) string {
	h := sha256.Sum256(secret)
	return hex.EncodeToString(h[:])
}

// Encode renders the join string: "ulj_" + base64url(version | expiry |
// fingerprint | secret | address).
func (j JoinInfo) Encode() (string, error) {
	if len(j.Secret) != joinSecretSize {
		return "", errors.New("pki: join secret has the wrong size")
	}
	if j.Addr == "" || len(j.Addr) > maxJoinAddrLen {
		return "", errors.New("pki: join address is empty or too long")
	}
	buf := make([]byte, 0, 1+8+32+joinSecretSize+len(j.Addr))
	buf = append(buf, joinVersion)
	buf = binary.BigEndian.AppendUint64(buf, uint64(j.ExpiresAt.Unix()))
	buf = append(buf, j.Fingerprint[:]...)
	buf = append(buf, j.Secret...)
	buf = append(buf, j.Addr...)
	return JoinPrefix + base64.RawURLEncoding.EncodeToString(buf), nil
}

// ParseJoinString decodes a join string. Whitespace around it is ignored (it is
// pasted from a terminal).
func ParseJoinString(s string) (JoinInfo, error) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, JoinPrefix) {
		return JoinInfo{}, fmt.Errorf("pki: join string must start with %q", JoinPrefix)
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(s, JoinPrefix))
	if err != nil {
		return JoinInfo{}, errors.New("pki: join string is not valid (bad encoding; was it truncated when pasted?)")
	}
	const fixed = 1 + 8 + 32 + joinSecretSize
	if len(raw) <= fixed || len(raw) > fixed+maxJoinAddrLen {
		return JoinInfo{}, errors.New("pki: join string is not valid (wrong length; was it truncated when pasted?)")
	}
	if raw[0] != joinVersion {
		return JoinInfo{}, fmt.Errorf("pki: unsupported join string version %d", raw[0])
	}
	var j JoinInfo
	j.ExpiresAt = time.Unix(int64(binary.BigEndian.Uint64(raw[1:9])), 0)
	copy(j.Fingerprint[:], raw[9:41])
	j.Secret = append([]byte(nil), raw[41:41+joinSecretSize]...)
	j.Addr = string(raw[41+joinSecretSize:])
	return j, nil
}
