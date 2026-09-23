// Package devinruntime manages optional checksum-verified Devin CLI updates.
// The standard image binary is always retained as a process-local fallback.
package devinruntime

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

const manifestURL = "https://static.devin.ai/cli/current/manifest.json"
const maxBundleBytes int64 = 256 << 20

var versionPattern = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+){2,3}(?:-[A-Za-z0-9.-]+)?$`)
var checksumPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type manifest struct {
	Version   string              `json:"version"`
	Platforms map[string]artifact `json:"platforms"`
}
type artifact struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}
type Selection struct{ Path, Version string }
type Manager struct {
	HTTP                                                  *http.Client
	ManifestURL, Directory, FallbackPath, FallbackVersion string
	mu                                                    sync.RWMutex
	current                                               Selection
}

func New(client *http.Client, directory, fallbackPath, fallbackVersion string) *Manager {
	m := &Manager{HTTP: client, Directory: directory, FallbackPath: fallbackPath, FallbackVersion: fallbackVersion}
	m.current = Selection{fallbackPath, fallbackVersion}
	return m
}
func (m *Manager) Current() (string, string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current.Path, m.current.Version
}
func (m *Manager) Check(ctx context.Context) (bool, error) { return m.check(ctx) }
func (m *Manager) check(ctx context.Context) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	target, err := platformTarget()
	if err != nil {
		return false, err
	}
	mf, err := m.loadManifest(ctx)
	if err != nil {
		return false, err
	}
	a, ok := mf.Platforms[target]
	if !ok {
		return false, errors.New("Devin manifest has no compatible platform")
	}
	if err = validateArtifact(mf.Version, target, a); err != nil {
		return false, err
	}
	currentVersion := m.current.Version
	if compareVersions(mf.Version, currentVersion) <= 0 {
		return false, nil
	}
	path, err := m.download(ctx, mf.Version, a)
	if err != nil {
		return false, err
	}
	m.current = Selection{path, mf.Version}
	return true, nil
}
func (m *Manager) Start(ctx context.Context, interval time.Duration, report func(bool, string, error)) {
	if interval <= 0 {
		interval = 12 * time.Hour
	}
	go func() {
		run := func() {
			checkCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
			defer cancel()
			updated, err := m.Check(checkCtx)
			_, version := m.Current()
			if report != nil {
				report(updated, version, err)
			}
		}
		run()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				run()
			}
		}
	}()
}
func (m *Manager) loadManifest(ctx context.Context) (manifest, error) {
	endpoint := m.ManifestURL
	if endpoint == "" {
		endpoint = manifestURL
	}
	if err := officialManifestURL(endpoint); err != nil {
		return manifest{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return manifest{}, err
	}
	client := m.HTTP
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	response, err := client.Do(req)
	if err != nil {
		return manifest{}, errors.New("Devin update manifest unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return manifest{}, fmt.Errorf("Devin update manifest returned HTTP %d", response.StatusCode)
	}
	var mf manifest
	if json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&mf) != nil {
		return manifest{}, errors.New("invalid Devin update manifest")
	}
	return mf, nil
}
func validateArtifact(version, target string, a artifact) error {
	if !versionPattern.MatchString(version) || !checksumPattern.MatchString(a.SHA256) {
		return errors.New("invalid Devin update metadata")
	}
	expected := "https://static.devin.ai/cli/" + version + "/devin-" + version + "-" + target + ".tar.gz"
	if a.URL != expected {
		return errors.New("untrusted Devin update URL")
	}
	return nil
}
func officialManifestURL(value string) error {
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || u.Host != "static.devin.ai" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("untrusted Devin manifest URL")
	}
	return nil
}
func platformTarget() (string, error) {
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "linux/amd64":
		return "x86_64-unknown-linux", nil
	case "linux/arm64":
		return "aarch64-unknown-linux", nil
	default:
		return "", errors.New("unsupported Devin runtime platform")
	}
}
func (m *Manager) download(ctx context.Context, version string, a artifact) (string, error) {
	if m.Directory == "" || !filepath.IsAbs(m.Directory) {
		return "", errors.New("Devin runtime directory must be absolute")
	}
	if err := os.MkdirAll(m.Directory, 0700); err != nil {
		return "", errors.New("Devin runtime directory unavailable")
	}
	versionDir := filepath.Join(m.Directory, version)
	binary := filepath.Join(versionDir, "devin")
	if validInstalledBinary(ctx, binary, version) {
		return binary, nil
	}
	if _, err := os.Stat(versionDir); err == nil {
		if err = os.RemoveAll(versionDir); err != nil {
			return "", errors.New("invalid Devin version directory cannot be replaced")
		}
	}
	temporary, err := os.MkdirTemp(m.Directory, ".download-")
	if err != nil {
		return "", errors.New("Devin update staging unavailable")
	}
	defer os.RemoveAll(temporary)
	bundle := filepath.Join(temporary, "bundle.tar.gz")
	if err = m.fetchBundle(ctx, a.URL, bundle, a.SHA256); err != nil {
		return "", err
	}
	staged := filepath.Join(temporary, "devin")
	if err = extractBinary(bundle, staged); err != nil {
		return "", err
	}
	if !validBinary(ctx, staged, version) {
		return "", errors.New("Devin update binary failed version check")
	}
	if err = os.MkdirAll(versionDir, 0700); err != nil {
		return "", errors.New("Devin version directory unavailable")
	}
	finalTemporary := binary + ".new"
	_ = os.Remove(finalTemporary)
	if err = os.Rename(staged, finalTemporary); err != nil {
		return "", errors.New("Devin update staging failed")
	}
	if err = os.Rename(finalTemporary, binary); err != nil {
		return "", errors.New("Devin update activation failed")
	}
	digest, err := fileDigest(binary)
	if err != nil {
		return "", errors.New("Devin update digest unavailable")
	}
	if err = os.WriteFile(binary+".sha256", []byte(digest+"\n"), 0400); err != nil {
		return "", errors.New("Devin update digest unavailable")
	}
	m.cleanupVersions(version)
	return binary, nil
}
func (m *Manager) fetchBundle(ctx context.Context, address, path, checksum string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return err
	}
	client := m.HTTP
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Minute}
	}
	response, err := client.Do(req)
	if err != nil {
		return errors.New("Devin update download unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return fmt.Errorf("Devin update download returned HTTP %d", response.StatusCode)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(file, hash), io.LimitReader(response.Body, maxBundleBytes+1))
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil || written > maxBundleBytes {
		return errors.New("invalid Devin update download")
	}
	if hex.EncodeToString(hash.Sum(nil)) != checksum {
		return errors.New("Devin update checksum mismatch")
	}
	return nil
}
func extractBinary(bundle, destination string) error {
	file, err := os.Open(bundle)
	if err != nil {
		return err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return errors.New("invalid Devin update archive")
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	found := false
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return errors.New("invalid Devin update archive")
		}
		if header.Name != "bin/devin" {
			continue
		}
		if found || header.Typeflag != tar.TypeReg || header.Size <= 0 || header.Size > maxBundleBytes {
			return errors.New("invalid Devin binary archive entry")
		}
		out, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0500)
		if err != nil {
			return err
		}
		written, copyErr := io.Copy(out, io.LimitReader(reader, maxBundleBytes+1))
		closeErr := out.Close()
		if copyErr != nil || closeErr != nil || written != header.Size {
			return errors.New("invalid Devin binary archive data")
		}
		found = true
	}
	if !found {
		return errors.New("Devin update archive omitted binary")
	}
	return nil
}

func validInstalledBinary(ctx context.Context, path, version string) bool {
	expected, err := os.ReadFile(path + ".sha256")
	if err != nil {
		return false
	}
	actual, err := fileDigest(path)
	return err == nil && strings.TrimSpace(string(expected)) == actual && validBinary(ctx, path, version)
}
func fileDigest(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err = io.Copy(hash, io.LimitReader(file, maxBundleBytes+1)); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
func (m *Manager) cleanupVersions(active string) {
	entries, err := os.ReadDir(m.Directory)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == active || strings.HasPrefix(entry.Name(), ".download-") {
			continue
		}
		if versionPattern.MatchString(entry.Name()) {
			_ = os.RemoveAll(filepath.Join(m.Directory, entry.Name()))
		}
	}
}
func validBinary(parent context.Context, path, version string) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 {
		return false
	}
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "--version").Output()
	return err == nil && strings.Contains(string(out), version)
}
func compareVersions(a, b string) int {
	pa, pb := strings.FieldsFunc(a, func(r rune) bool { return r == '.' || r == '-' }), strings.FieldsFunc(b, func(r rune) bool { return r == '.' || r == '-' })
	for i := 0; i < len(pa) && i < len(pb); i++ {
		var x, y int
		if _, e := fmt.Sscanf(pa[i], "%d", &x); e != nil {
			break
		}
		if _, e := fmt.Sscanf(pb[i], "%d", &y); e != nil {
			break
		}
		if x > y {
			return 1
		}
		if x < y {
			return -1
		}
	}
	return strings.Compare(a, b)
}
