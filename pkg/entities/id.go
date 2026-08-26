package entities

import (
	"crypto/rand"
	"encoding/hex"
)

// NewID creates an application-owned identifier before persistence.
func NewID(prefix string) string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return prefix + "_" + hex.EncodeToString(b)
}
