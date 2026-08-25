package seal

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
)

type Sealer struct {
	aead cipher.AEAD
}

func New(key string) (*Sealer, error) {
	if key == "" {
		return nil, errors.New("cryptoseal: empty key")
	}
	var raw []byte
	if b, err := base64.StdEncoding.DecodeString(key); err == nil && len(b) == 32 {
		raw = b
	} else {
		h := sha256.Sum256([]byte(key))
		raw = h[:]
	}
	block, err := aes.NewCipher(raw)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Sealer{aead: aead}, nil
}

func (s *Sealer) Seal(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return s.aead.Seal(nonce, nonce, plaintext, nil), nil
}

func (s *Sealer) Open(sealed []byte) ([]byte, error) {
	ns := s.aead.NonceSize()
	if len(sealed) < ns+1 {
		return nil, errors.New("cryptoseal: ciphertext too short")
	}
	return s.aead.Open(nil, sealed[:ns], sealed[ns:], nil)
}

func GenerateKey() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}
