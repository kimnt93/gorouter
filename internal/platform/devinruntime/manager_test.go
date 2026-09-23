package devinruntime

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestManagerValidatesStagesAndActivatesUpdate(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("shell fixture")
	}
	target, _ := platformTarget()
	script := []byte("#!/bin/sh\necho 'devin 3000.11.1 (synthetic)'\n")
	bundle := archive(t, script, "bin/devin", tar.TypeReg)
	sum := sha256.Sum256(bundle)
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/manifest.json":
			_ = json.NewEncoder(w).Encode(manifest{Version: "3000.11.1", Platforms: map[string]artifact{target: {URL: "https://static.devin.ai/cli/3000.11.1/devin-3000.11.1-" + target + ".tar.gz", SHA256: hex.EncodeToString(sum[:])}}})
		case "/bundle.tar.gz":
			_, _ = w.Write(bundle)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	m := New(server.Client(), t.TempDir(), "/fallback/devin", "3000.10.31")
	m.ManifestURL = server.URL + "/manifest.json"
	// Redirect expected official URL through test transport.
	m.HTTP = &http.Client{Transport: rewriteTransport{base: server.URL, inner: server.Client().Transport}}
	m.ManifestURL = manifestURL
	updated, err := m.Check(context.Background())
	if err != nil || !updated {
		t.Fatalf("updated=%v err=%v", updated, err)
	}
	path, version := m.Current()
	if version != "3000.11.1" || !strings.HasPrefix(path, m.Directory) || !validInstalledBinary(context.Background(), path, version) {
		t.Fatalf("path=%s version=%s", path, version)
	}
	if updated, err = m.Check(context.Background()); err != nil || updated {
		t.Fatal("redownloaded same version")
	}
}

type rewriteTransport struct {
	base  string
	inner http.RoundTripper
}

func (r rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	switch req.URL.Host {
	case "static.devin.ai":
		if strings.HasSuffix(req.URL.Path, "manifest.json") {
			clone.URL, _ = clone.URL.Parse(r.base + "/manifest.json")
		} else {
			clone.URL, _ = clone.URL.Parse(r.base + "/bundle.tar.gz")
		}
	}
	return r.inner.RoundTrip(clone)
}
func archive(t *testing.T, data []byte, name string, kind byte) []byte {
	t.Helper()
	file := filepath.Join(t.TempDir(), "bundle")
	out, _ := os.Create(file)
	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: name, Mode: 0500, Size: int64(len(data)), Typeflag: kind})
	_, _ = tw.Write(data)
	tw.Close()
	gz.Close()
	out.Close()
	b, _ := os.ReadFile(file)
	return b
}
func TestRejectsUntrustedMetadataAndArchive(t *testing.T) {
	target, _ := platformTarget()
	good := artifact{URL: "https://static.devin.ai/cli/3000.11.1/devin-3000.11.1-" + target + ".tar.gz", SHA256: strings.Repeat("a", 64)}
	for _, tc := range []struct {
		version string
		a       artifact
	}{{"../bad", good}, {"3000.11.1", artifact{URL: "https://evil.invalid/x", SHA256: good.SHA256}}, {"3000.11.1", artifact{URL: good.URL, SHA256: "bad"}}} {
		if validateArtifact(tc.version, target, tc.a) == nil {
			t.Fatal("trusted invalid metadata")
		}
	}
	if officialManifestURL("http://static.devin.ai/cli/current/manifest.json") == nil || officialManifestURL("https://evil.invalid/x") == nil {
		t.Fatal("trusted origin")
	}
	bundle := archive(t, []byte("x"), "../../devin", tar.TypeReg)
	if extractBinary(writeTemp(t, bundle), filepath.Join(t.TempDir(), "devin")) == nil {
		t.Fatal("accepted traversal")
	}
}
func writeTemp(t *testing.T, b []byte) string {
	p := filepath.Join(t.TempDir(), "b.tar.gz")
	os.WriteFile(p, b, 0600)
	return p
}
func TestTamperedInstalledBinaryIsReplaced(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("shell fixture")
	}
	target, _ := platformTarget()
	script := []byte("#!/bin/sh\necho 'devin 3000.11.1 (synthetic)'\n")
	bundle := archive(t, script, "bin/devin", tar.TypeReg)
	sum := sha256.Sum256(bundle)
	downloads := 0
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "manifest.json") {
			_ = json.NewEncoder(w).Encode(manifest{Version: "3000.11.1", Platforms: map[string]artifact{target: {URL: "https://static.devin.ai/cli/3000.11.1/devin-3000.11.1-" + target + ".tar.gz", SHA256: hex.EncodeToString(sum[:])}}})
			return
		}
		downloads++
		_, _ = w.Write(bundle)
	}))
	defer server.Close()
	m := New(&http.Client{Transport: rewriteTransport{base: server.URL, inner: server.Client().Transport}}, t.TempDir(), "/fallback", "3000.10.31")
	if _, err := m.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	path, _ := m.Current()
	if os.Chmod(path, 0700) != nil || os.WriteFile(path, []byte("tampered"), 0500) != nil {
		t.Fatal("tamper")
	}
	m.current = Selection{m.FallbackPath, m.FallbackVersion}
	if _, err := m.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if downloads != 2 || !validInstalledBinary(context.Background(), path, "3000.11.1") {
		t.Fatal("tampered runtime reused")
	}
}
func TestVersionComparison(t *testing.T) {
	if compareVersions("3000.11.1", "3000.10.31") <= 0 || compareVersions("3000.11.1", "3000.11.1") != 0 {
		t.Fatal("comparison")
	}
}

func TestConcurrentChecksDownloadOnce(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("shell fixture")
	}
	target, _ := platformTarget()
	script := []byte("#!/bin/sh\necho 'devin 3000.11.1 (synthetic)'\n")
	bundle := archive(t, script, "bin/devin", tar.TypeReg)
	sum := sha256.Sum256(bundle)
	downloads := 0
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "manifest.json") {
			_ = json.NewEncoder(w).Encode(manifest{Version: "3000.11.1", Platforms: map[string]artifact{target: {URL: "https://static.devin.ai/cli/3000.11.1/devin-3000.11.1-" + target + ".tar.gz", SHA256: hex.EncodeToString(sum[:])}}})
			return
		}
		downloads++
		_, _ = w.Write(bundle)
	}))
	defer server.Close()
	m := New(&http.Client{Transport: rewriteTransport{base: server.URL, inner: server.Client().Transport}}, t.TempDir(), "/fallback", "3000.10.31")
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			_, err := m.Check(context.Background())
			if err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if downloads != 1 {
		t.Fatalf("downloads=%d", downloads)
	}
}
