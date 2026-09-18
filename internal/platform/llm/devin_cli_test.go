package llm

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kimnt93/gorouter/pkg/entities"
)

func mockDevinBinary(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "devin")
	script := `#!/bin/sh
[ "$WINDSURF_API_KEY" = 'apk_user_synthetic' ] || exit 2
if [ "$1" = 'models' ]; then
 printf '%s\n' '[{"id":"future-model","name":"Future Model","supported_reasoning_levels":[{"effort":"low"},{"effort":"high"}]},{"id":"next-model"}]'
 exit 0
fi
[ "$1" = 'acp' ] || exit 3
while IFS= read -r line; do
 case "$line" in
 *'"method":"initialize"'*) printf '%s\n' '{"jsonrpc":"2.0","id":1,"result":{}}';;
 *'"method":"session/new"'*) printf '%s\n' '{"jsonrpc":"2.0","id":2,"result":{"sessionId":"synthetic"}}';;
 *'"method":"session/prompt"'*) printf '%s\n' '{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"hello"}}}}' '{"jsonrpc":"2.0","id":3,"result":{"stopReason":"end_turn"}}';;
 esac
done
`
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}
func TestDevinCLIUsesACPAndDiscoversLatestModels(t *testing.T) {
	a := &DevinCLIAdapter{Binary: mockDevinBinary(t), WorkDir: t.TempDir()}
	cr := &entities.CredentialRuntime{Provider: "devin-cli", Kind: entities.KindAPIKey, APIKey: "apk_user_synthetic"}
	status, err := a.Probe(context.Background(), cr)
	if err != nil || status != 200 {
		t.Fatalf("probe=%d %v", status, err)
	}
	models, err := a.DiscoverModels(context.Background(), cr)
	if err != nil || len(models) != 2 || models[0].ID != "future-model" || len(models[0].SupportedReasoningLevels) != 2 {
		t.Fatalf("models=%+v err=%v", models, err)
	}
	for _, stream := range []bool{false, true} {
		payload, _ := json.Marshal(ChatRequest{Messages: []Message{{Role: "user", Content: []byte(`"hi"`)}}, Stream: stream})
		result, err := a.Send(context.Background(), cr, "future-model", payload)
		if err != nil {
			t.Fatal(err)
		}
		body, e := io.ReadAll(result.Body)
		result.Body.Close()
		if e != nil || result.StatusCode != 200 || !strings.Contains(string(body), "hello") {
			t.Fatalf("stream=%t status=%d err=%v", stream, result.StatusCode, e)
		}
		if stream && !strings.Contains(string(body), "[DONE]") {
			t.Fatal("missing stream terminator")
		}
	}
}
func TestDevinCLIRejectsWrongKeyBeforeSpawning(t *testing.T) {
	a := &DevinCLIAdapter{Binary: mockDevinBinary(t), WorkDir: t.TempDir()}
	_, err := a.Probe(context.Background(), &entities.CredentialRuntime{APIKey: "cog_synthetic"})
	if err == nil || !strings.Contains(err.Error(), "apk_user") {
		t.Fatalf("wrong key accepted: %v", err)
	}
}
func TestDevinCLICatalogRejectsUnsafeIDs(t *testing.T) {
	models, err := parseDevinCLIModels([]byte(`[{"id":"safe-next"},{"id":"../unsafe"},{"id":"safe-next"}]`))
	if err != nil || len(models) != 1 {
		t.Fatalf("models=%+v err=%v", models, err)
	}
}

func TestDevinCLIRejectsUnsupportedReasoningAndTools(t *testing.T) {
	a := &DevinCLIAdapter{Binary: mockDevinBinary(t), WorkDir: t.TempDir()}
	cr := &entities.CredentialRuntime{APIKey: "apk_user_synthetic"}
	for _, payload := range [][]byte{[]byte(`{"reasoning":{"effort":"high"},"messages":[{"role":"user","content":"hello"}]}`), []byte(`{"tools":[{"type":"function","function":{"name":"unsafe"}}],"messages":[{"role":"user","content":"hello"}]}`)} {
		if _, err := a.Send(context.Background(), cr, "future-model", payload); err == nil {
			t.Fatal("unsupported request accepted")
		}
	}
}

func TestDevinCLIMissingBinaryFailsClosed(t *testing.T) {
	a := &DevinCLIAdapter{Binary: filepath.Join(t.TempDir(), "missing-cli"), WorkDir: t.TempDir()}
	cr := &entities.CredentialRuntime{Provider: "devin-cli", APIKey: "apk_user_synthetic"}
	if status, err := a.Probe(context.Background(), cr); err == nil || status != 0 {
		t.Fatalf("probe=%d err=%v", status, err)
	}
	if _, err := a.DiscoverModels(context.Background(), cr); err == nil {
		t.Fatal("invented catalog without CLI")
	}
	if _, err := a.Send(context.Background(), cr, "future-model", []byte(`{"messages":[{"role":"user","content":"hello"}]}`)); err == nil {
		t.Fatal("claimed chat success without CLI")
	}
}

func TestDevinCLIProbeAndDiscoveryUseIsolatedHomes(t *testing.T) {
	a := &DevinCLIAdapter{Binary: mockDevinBinary(t), WorkDir: t.TempDir()}
	cr := &entities.CredentialRuntime{APIKey: "apk_user_synthetic"}
	_, err := a.Probe(context.Background(), cr)
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.DiscoverModels(context.Background(), cr)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(a.WorkDir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("left credential home on disk: count=%d err=%v", len(entries), err)
	}
}

func TestDevinCLIDeniesInvalidKeyWithoutClaimingHealthy(t *testing.T) {
	a := &DevinCLIAdapter{Binary: mockDevinBinary(t), WorkDir: t.TempDir()}
	cr := &entities.CredentialRuntime{Provider: "devin-cli", APIKey: "apk_user_invalid_synthetic"}
	if status, err := a.Probe(context.Background(), cr); err == nil || status != 0 {
		t.Fatalf("probe=%d err=%v", status, err)
	}
}
