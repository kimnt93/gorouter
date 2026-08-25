package seal

import (
	"bytes"
	"testing"
)

func TestSealOpenRoundTrip(t *testing.T) {
	s, err := New("top-secret-passphrase")
	if err != nil {
		t.Fatal(err)
	}
	plain := []byte(`{"access_token":"sk-ant-oat-abc"}`)
	sealed, err := s.Seal(plain)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("sk-ant-oat")) {
		t.Fatal("sealed output leaks plaintext")
	}
	got, err := s.Open(sealed)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("round trip mismatch: %q", got)
	}
}

func TestWrongKeyFails(t *testing.T) {
	s1, _ := New("one")
	s2, _ := New("two")
	sealed, _ := s1.Seal([]byte("data"))
	if _, err := s2.Open(sealed); err == nil {
		t.Fatal("expected decrypt failure with wrong key")
	}
}

func TestSealUsesUniqueNonces(t *testing.T) {
	sealer, _ := New("nonce-test-key")
	first, err := sealer.Seal([]byte("same plaintext"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := sealer.Seal([]byte("same plaintext"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first, second) {
		t.Fatal("AES-GCM ciphertext was reused for identical plaintext")
	}
}

func TestEmptyKeyRejected(t *testing.T) {
	if _, err := New(""); err == nil {
		t.Fatal("expected error for empty key")
	}
}
