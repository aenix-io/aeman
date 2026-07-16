package server

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// deriveSessionKey turns the configured secret into a 32-byte AES-256 key, or
// nil when no secret is set (sessions then stay in memory only).
func deriveSessionKey(secret string) []byte {
	if secret == "" {
		return nil
	}
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}

// encryptSessions serialises the sessions map and seals it with AES-256-GCM
// under key, returning base64(nonce || ciphertext). The random nonce makes each
// write distinct, so the file leaks nothing about unchanged sessions.
func encryptSessions(key []byte, sessions map[string]persistedSession) (string, error) {
	plain, err := json.Marshal(sessions)
	if err != nil {
		return "", err
	}
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, plain, nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// decryptSessions reverses encryptSessions. A wrong key or tampered file fails
// the GCM auth check and returns an error, so the caller falls back to an empty
// (memory-only) session set rather than trusting garbage.
func decryptSessions(key []byte, enc string) (map[string]persistedSession, error) {
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return nil, err
	}
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	if len(raw) < gcm.NonceSize() {
		return nil, errors.New("encrypted sessions too short")
	}
	nonce, ciphertext := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt sessions: %w", err)
	}
	var sessions map[string]persistedSession
	if err := json.Unmarshal(plain, &sessions); err != nil {
		return nil, err
	}
	return sessions, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
