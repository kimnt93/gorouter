package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Packaging is part of provider availability: the normal build/release must
// include required runtimes, not just expose cards for absent executables.
func TestStandardImageIncludesDevinRuntime(t *testing.T) {
	root := filepath.Join("..", "..")
	read := func(name string) string {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	dockerfile := read("Dockerfile")
	lastStage := strings.LastIndex(dockerfile, "\nFROM ")
	if lastStage < 0 {
		t.Fatal("missing runtime stage")
	}
	runtime := dockerfile[lastStage:]
	for _, required := range []string{"COPY --from=build /out/gorouter /usr/local/bin/gorouter", "COPY --from=devin-download /out/devin /usr/local/bin/devin", "USER 65532:65532", "RUN devin --version", "mkdir -p /var/lib/gorouter/provider-runtimes", "chown -R 65532:65532 /var/lib/gorouter"} {
		if !strings.Contains(runtime, required) {
			t.Errorf("standard image missing %s", required)
		}
	}
	if strings.Index(runtime, "RUN devin --version") < strings.Index(runtime, "USER 65532:65532") {
		t.Error("CLI must be smoke tested as the non-root runtime user")
	}
	if !strings.Contains(dockerfile, `RUN sh /install-devin-cli.sh "$TARGETARCH" /out`) {
		t.Error("standard build must use architecture-specific verified installer")
	}
	installer := read("scripts/install-devin-cli.sh")
	for _, required := range []string{"amd64)", "arm64)", "sha256sum -c -"} {
		if !strings.Contains(installer, required) {
			t.Errorf("installer missing %s", required)
		}
	}
	workflow := read(".github/workflows/release-image.yml")
	for _, obsolete := range []string{"matrix.target", "matrix.suffix", "-devin-cli", "          target:"} {
		if strings.Contains(workflow, obsolete) {
			t.Errorf("release still requires provider variant: %s", obsolete)
		}
	}
	for _, required := range []string{"linux/amd64,linux/arm64", "type=raw,value=latest", "type=raw,value=${{ github.event.release.tag_name }}"} {
		if !strings.Contains(workflow, required) {
			t.Errorf("release missing %s", required)
		}
	}
	for _, profile := range []string{"local", "postgres", "clickhouse"} {
		cfg := read("docker-compose." + profile + ".yml")
		if !strings.Contains(cfg, "build: .") || strings.Contains(cfg, "target:") {
			t.Errorf("%s profile does not use the standard image", profile)
		}
		if profile != "local" && (!strings.Contains(cfg, "provider_runtimes:/var/lib/gorouter/provider-runtimes") || !strings.Contains(cfg, "  provider_runtimes:")) {
			t.Errorf("%s profile does not persist verified provider runtimes", profile)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "docker-compose.devin-cli.yml")); !os.IsNotExist(err) {
		t.Error("provider-specific Compose overlay should not exist")
	}
}
