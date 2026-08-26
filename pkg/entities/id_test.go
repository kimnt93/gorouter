package entities

import "testing"

func TestBackendIdentityGenerators(t *testing.T) {
	ids := map[string]bool{}
	for i := 0; i < 1000; i++ {
		id := NewID("test")
		if id == "" || ids[id] {
			t.Fatalf("invalid or duplicate ID %q", id)
		}
		ids[id] = true
	}
}
