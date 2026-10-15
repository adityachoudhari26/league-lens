package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
)

const keySize = 32

type Box struct {
	aead cipher.AEAD
}

// LoadOrCreateKey loads a 32-byte encryption key from the environment or app data.
func LoadOrCreateKey(dataDir string) ([]byte, error) {
	if encoded, ok := os.LookupEnv("LEAGUE_LENS_SECRET_KEY"); ok {
		key, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || len(key) != keySize {
			return nil, errors.New("LEAGUE_LENS_SECRET_KEY must be standard base64 for a 32-byte key")
		}
		return key, nil
	}

	path := filepath.Join(dataDir, "secret.key")
	key, err := readKeyFile(path)
	if err == nil {
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("cannot read secret key")
	}
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, errors.New("cannot create secret key directory")
	}

	key = make([]byte, keySize)
	if _, err := rand.Read(key); err != nil {
		return nil, errors.New("cannot generate secret key")
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		return readKeyFile(path)
	}
	if err != nil {
		return nil, errors.New("cannot create secret key")
	}
	if _, err = io.WriteString(file, base64.StdEncoding.EncodeToString(key)); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return nil, errors.New("cannot write secret key")
	}
	if err = file.Close(); err != nil {
		return nil, errors.New("cannot close secret key")
	}
	return key, nil
}

func readKeyFile(path string) ([]byte, error) {
	encoded, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	key, err := base64.StdEncoding.DecodeString(string(encoded))
	if err != nil || len(key) != keySize {
		return nil, errors.New("invalid secret key file")
	}
	return key, nil
}

// NewBox creates an AES-256-GCM credential box.
func NewBox(key []byte) (*Box, error) {
	if len(key) != keySize {
		return nil, errors.New("AES-256 key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, errors.New("cannot initialize AES-GCM")
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, errors.New("cannot initialize AES-GCM")
	}
	return &Box{aead: aead}, nil
}

// Seal authenticates plaintext and associated data, prefixing the ciphertext with its nonce.
func (b *Box) Seal(plaintext, associatedData []byte) ([]byte, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, errors.New("cannot generate encryption nonce")
	}
	return b.aead.Seal(nonce, nonce, plaintext, associatedData), nil
}

// Open authenticates and decrypts a nonce-prefixed credential ciphertext.
func (b *Box) Open(sealed, associatedData []byte) ([]byte, error) {
	nonceSize := b.aead.NonceSize()
	if len(sealed) < nonceSize+b.aead.Overhead() {
		return nil, errors.New("invalid encrypted credentials")
	}
	plaintext, err := b.aead.Open(nil, sealed[:nonceSize], sealed[nonceSize:], associatedData)
	if err != nil {
		return nil, errors.New("invalid encrypted credentials")
	}
	return plaintext, nil
}
