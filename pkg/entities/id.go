package entities

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
)

// NewID creates an application-owned identifier before persistence.
func NewID(prefix string) string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil { panic(err) }
	return prefix + "_" + hex.EncodeToString(b)
}

func NewSequence() int64 {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil { panic(err) }
	return int64(binary.BigEndian.Uint64(b[:]) & ((1 << 63) - 1)) + 1
}
