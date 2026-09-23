package devinruntime

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOfficialManifestAndBinary(t *testing.T) {
	if os.Getenv("TEST_DEVIN_RUNTIME_LIVE") != "1" {
		t.Skip("live update test opt-in")
	}
	dir := t.TempDir()
	m := New(nil, dir, "/usr/local/bin/devin", "3000.10.31")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	updated, err := m.Check(ctx)
	if err != nil {
		t.Fatal(err)
	}
	path, version := m.Current()
	if !updated || path == m.FallbackPath || filepath.Dir(path) == dir {
		t.Fatalf("updated=%v path=%s version=%s", updated, path, version)
	}
	t.Logf("verified version=%s", version)
}
