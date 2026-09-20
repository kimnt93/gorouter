package llm

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kimnt93/gorouter/pkg/entities"
)

// The child is a strict protocol fixture, not a simulated provider key.
// Reject the old string version, nonstandard session model parameter, missing
// selection confirmation and any attempt to use `models list` or a probe prompt.
func TestDevinACPHelperProcess(t *testing.T) {
	if len(os.Args) < 3 || os.Args[len(os.Args)-2] != "devin-helper" {
		return
	}
	scenario := os.Args[len(os.Args)-1]
	home := os.Getenv("HOME")
	if home != os.Getenv("TMPDIR") || !strings.HasPrefix(os.Getenv("XDG_CONFIG_HOME"), home+string(os.PathSeparator)) {
		os.Exit(90)
	}
	model, level := "future-model", "low"
	options := func() []devinOption {
		values := []devinValue{{Value: "low", Name: "Low"}, {Value: "ultra-next", Name: "Next effort"}}
		if model == "next-model" {
			values = []devinValue{{Value: "off", Name: "Off"}}
		}
		return []devinOption{{ID: "model-id", Type: "select", Category: "model", CurrentValue: model, Options: []devinValue{{Group: "future", Name: "Future", Options: []devinValue{{Value: "future-model", Name: "Future Model"}, {Value: "next-model", Name: "Next Model"}, {Value: "../unsafe", Name: "Unsafe"}}}}}, {ID: "effort-id", Type: "select", Category: "thought_level", CurrentValue: level, Options: values}}
	}
	emit := func(id json.RawMessage, result any) {
		_ = json.NewEncoder(os.Stdout).Encode(struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id"`
			Result  any             `json:"result"`
		}{"2.0", id, result})
	}
	fail := func(id json.RawMessage, code int, text string) {
		_ = json.NewEncoder(os.Stdout).Encode(devinRPC{JSONRPC: "2.0", ID: id, Error: &devinRPCError{code, text}})
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var msg devinRPC
		if json.Unmarshal(scanner.Bytes(), &msg) != nil {
			os.Exit(91)
		}
		switch msg.Method {
		case "initialize":
			var params devinInitialize
			if json.Unmarshal(msg.Params, &params) != nil || params.ProtocolVersion != 1 || params.ClientCapabilities.FS.ReadTextFile || params.ClientCapabilities.Terminal {
				os.Exit(92)
			}
			if scenario == "timeout" {
				time.Sleep(time.Minute)
			}
			emit(msg.ID, struct {
				ProtocolVersion int `json:"protocolVersion"`
			}{1})
		case "session/new":
			var params struct {
				CWD        string     `json:"cwd"`
				MCPServers []struct{} `json:"mcpServers"`
				Model      string     `json:"model"`
			}
			if json.Unmarshal(msg.Params, &params) != nil || params.CWD != home || params.MCPServers == nil || len(params.MCPServers) > 0 || params.Model != "" {
				os.Exit(93)
			}
			if key := os.Getenv("WINDSURF_API_KEY"); key != "apk_user_synthetic" && key != "cog_synthetic" {
				fail(msg.ID, -32603, "Authentication required: invalid api key SENSITIVE")
				continue
			}
			if scenario == "no-catalog" {
				emit(msg.ID, devinSessionResult{SessionID: "s1"})
				continue
			}
			emit(msg.ID, devinSessionResult{SessionID: "s1", ConfigOptions: options()})
		case "session/set_config_option":
			var params devinSetOption
			if json.Unmarshal(msg.Params, &params) != nil || params.SessionID != "s1" {
				os.Exit(94)
			}
			if params.ConfigID == "model-id" {
				model = params.Value
				level = "low"
				if model == "next-model" {
					level = "off"
				}
			}
			if params.ConfigID == "effort-id" {
				level = params.Value
			}
			emit(msg.ID, struct {
				Options []devinOption `json:"configOptions"`
			}{options()})
		case "session/prompt":
			if scenario == "no-prompt" {
				fail(msg.ID, -32602, "Probe must not send prompt")
				continue
			}
			var params devinPromptParams
			if json.Unmarshal(msg.Params, &params) != nil || params.SessionID != "s1" {
				os.Exit(95)
			}
			if scenario == "selected" && (model != "next-model" || level != "off") {
				os.Exit(96)
			}
			if scenario == "reasoning" && level != "ultra-next" {
				os.Exit(97)
			}
			if scenario == "rpc-error" {
				fail(msg.ID, -32603, "Provider rate limit exceeded SENSITIVE")
				continue
			}
			if scenario == "wrong-id" {
				emit(json.RawMessage(`99`), struct {
					StopReason string `json:"stopReason"`
				}{"end_turn"})
				continue
			}
			if scenario == "malformed" {
				fmt.Fprintln(os.Stdout, "not-json SENSITIVE")
				continue
			}
			update := func(kind, text string) {
				var u devinUpdate
				u.SessionID = "s1"
				u.Update.Kind = kind
				u.Update.Content = devinContent{Type: "text", Text: text}
				b, _ := json.Marshal(u)
				_ = json.NewEncoder(os.Stdout).Encode(devinRPC{JSONRPC: "2.0", Method: "session/update", Params: b})
			}
			if scenario == "agent-request" {
				b, _ := json.Marshal(struct {
					SessionID string `json:"sessionId"`
				}{"s1"})
				_ = json.NewEncoder(os.Stdout).Encode(devinRPC{JSONRPC: "2.0", ID: json.RawMessage(`"tool-request"`), Method: "fs/read_text_file", Params: b})
				if !scanner.Scan() {
					os.Exit(100)
				}
				var denied devinRPC
				if json.Unmarshal(scanner.Bytes(), &denied) != nil || string(denied.ID) != `"tool-request"` || denied.Error == nil || denied.Error.Code != -32601 {
					os.Exit(101)
				}
			}
			update("agent_thought_chunk", "synthetic thought")
			update("agent_message_chunk", "hello")
			if scenario == "slow" {
				time.Sleep(700 * time.Millisecond)
			}
			if scenario == "hang" {
				time.Sleep(time.Minute)
			}
			if scenario == "eof" {
				os.Exit(0)
			}
			stop := "end_turn"
			if scenario == "cancelled" {
				stop = "cancelled"
			}
			if scenario == "empty-stop" {
				stop = ""
			}
			if scenario == "usage" {
				emit(msg.ID, struct {
					StopReason string         `json:"stopReason"`
					Usage      devinTurnUsage `json:"usage"`
				}{stop, devinTurnUsage{Input: 10, Output: 20, Thought: 5, CacheRead: 30, CacheWrite: 40}})
			} else {
				emit(msg.ID, struct {
					StopReason string `json:"stopReason"`
				}{stop})
			}
		default:
			// Client safely rejects an agent-originated method; no shell or file work.
			if msg.Error == nil {
				os.Exit(98)
			}
		}
	}
	os.Exit(0)
}
func mockDevinBinary(t *testing.T, scenario string) string {
	t.Helper()
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "devin")
	// Values come exclusively from this test, not a user's request/credential.
	script := fmt.Sprintf("#!/bin/sh\n[ \"$1\" = acp ] || exit 3\nexec '%s' -test.run='^TestDevinACPHelperProcess$' -- devin-helper '%s'\n", bin, scenario)
	if err = os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}
func devinTestAdapter(t *testing.T, scenario string) *DevinCLIAdapter {
	return &DevinCLIAdapter{Binary: mockDevinBinary(t, scenario), WorkDir: t.TempDir(), MaxProcesses: 2}
}
func devinTestCredential() *entities.CredentialRuntime {
	return &entities.CredentialRuntime{Provider: "devin-cli", Kind: entities.KindAPIKey, APIKey: "apk_user_synthetic"}
}
func TestDevinCLIAcceptsCurrentCognitionPAT(t *testing.T) {
	a := devinTestAdapter(t, "no-catalog")
	cr := devinTestCredential()
	cr.APIKey = "cog_synthetic"
	if status, err := a.Probe(context.Background(), cr); err != nil || status != http.StatusOK {
		t.Fatalf("status=%d err=%v", status, err)
	}
	models, err := a.DiscoverModels(context.Background(), cr)
	if err != nil || len(models) != 1 || models[0].ID != "adaptive" || len(models[0].SupportedReasoningLevels) != 0 {
		t.Fatalf("models=%+v err=%v", models, err)
	}
	r, err := a.Send(context.Background(), cr, "adaptive", []byte(`{"messages":[{"role":"user","content":"synthetic"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", r.StatusCode)
	}
}

func TestDevinCLIHealthAndDiscoveryWithoutInference(t *testing.T) {
	a := devinTestAdapter(t, "no-prompt")
	cr := devinTestCredential()
	if status, err := a.Probe(context.Background(), cr); err != nil || status != 200 {
		t.Fatalf("status=%d err=%v", status, err)
	}
	for range 2 {
		models, err := a.DiscoverModels(context.Background(), cr)
		if err != nil || len(models) != 2 {
			t.Fatalf("count=%d err=%v", len(models), err)
		}
		if models[0].ID != "future-model" || len(models[0].SupportedReasoningLevels) != 2 || models[0].SupportedReasoningLevels[1].Effort != "ultra-next" {
			t.Fatal("did not retain fresh upstream efforts")
		}
		if models[1].DefaultReasoningLevel != "off" || len(models[1].SupportedReasoningLevels) != 1 {
			t.Fatal("copied one model's capabilities to another")
		}
	}
	entries, _ := os.ReadDir(a.WorkDir)
	if len(entries) != 0 {
		t.Fatal("temporary homes leaked")
	}
}
func TestDevinCLIFailsClosed(t *testing.T) {
	for _, scenario := range []string{"no-catalog", "timeout"} {
		t.Run(scenario, func(t *testing.T) {
			a := devinTestAdapter(t, scenario)
			ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
			defer cancel()
			if _, err := a.DiscoverModels(ctx, devinTestCredential()); err == nil {
				t.Fatal("invented catalog")
			}
		})
	}
	a := &DevinCLIAdapter{Binary: filepath.Join(t.TempDir(), "missing"), WorkDir: t.TempDir()}
	if status, err := a.Probe(context.Background(), devinTestCredential()); status != 503 || err == nil {
		t.Fatalf("status=%d err=%v", status, err)
	}
}
func TestDevinCLIAuthStatusNoDiagnostics(t *testing.T) {
	a := devinTestAdapter(t, "normal")
	cr := devinTestCredential()
	cr.APIKey = "apk_user_invalid"
	status, err := a.Probe(context.Background(), cr)
	if status != 401 || err == nil || strings.Contains(err.Error(), "SENSITIVE") {
		t.Fatalf("status=%d err=%v", status, err)
	}
	r, err := a.Send(context.Background(), cr, "future-model", []byte(`{"messages":[{"role":"user","content":"synthetic"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	if r.StatusCode != 401 {
		t.Fatalf("status=%d", r.StatusCode)
	}
}
func TestDevinCLIChatSelectsModelAndReasoning(t *testing.T) {
	for _, scenario := range []string{"selected", "reasoning"} {
		t.Run(scenario, func(t *testing.T) {
			a := devinTestAdapter(t, scenario)
			model, effort := "future-model", "ultra-next"
			if scenario == "selected" {
				model, effort = "next-model", "off"
			}
			req := ChatRequest{Messages: []Message{{Role: "user", Content: []byte(`"synthetic"`)}}, Reasoning: &Reasoning{Effort: effort}}
			raw, _ := json.Marshal(req)
			r, err := a.Send(context.Background(), devinTestCredential(), model, raw)
			if err != nil {
				t.Fatal(err)
			}
			defer r.Body.Close()
			var out Response
			if json.NewDecoder(r.Body).Decode(&out) != nil || r.StatusCode != 200 || len(out.Choices) != 1 || out.Choices[0].Message.Content != "hello" || out.Choices[0].Message.ReasoningContent != "synthetic thought" {
				t.Fatal("invalid translated response")
			}
		})
	}
}
func TestDevinCLIIncrementalStream(t *testing.T) {
	a := devinTestAdapter(t, "slow")
	start := time.Now()
	r, err := a.Send(context.Background(), devinTestCredential(), "future-model", []byte(`{"stream":true,"messages":[{"role":"user","content":"synthetic"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	reader := bufio.NewReader(r.Body)
	line, err := reader.ReadString('\n')
	if err != nil || !strings.Contains(line, "reasoning_content") || !strings.Contains(line, `"finish_reason":null`) {
		t.Fatal("first delta not emitted")
	}
	if time.Since(start) > 600*time.Millisecond {
		t.Fatal("buffered entire turn before first delta")
	}
	rest, err := io.ReadAll(reader)
	if err != nil || !bytesContain(rest, "[DONE]") || !bytesContain(rest, "prompt_tokens") {
		t.Fatalf("stream completion err=%v", err)
	}
}
func bytesContain(b []byte, s string) bool { return strings.Contains(string(b), s) }
func TestDevinCLIStreamCancellationCleansUp(t *testing.T) {
	a := devinTestAdapter(t, "hang")
	r, err := a.Send(context.Background(), devinTestCredential(), "future-model", []byte(`{"stream":true,"messages":[{"role":"user","content":"synthetic"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 32)
	_, _ = r.Body.Read(buf)
	_ = r.Body.Close()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		entries, _ := os.ReadDir(a.WorkDir)
		if len(entries) == 0 && len(a.slots) == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("process/home/slot leaked on cancellation")
}
func TestDevinCLIStreamFailureNeverDone(t *testing.T) {
	for _, scenario := range []string{"eof", "malformed", "wrong-id", "empty-stop", "cancelled", "rpc-error"} {
		t.Run(scenario, func(t *testing.T) {
			a := devinTestAdapter(t, scenario)
			r, err := a.Send(context.Background(), devinTestCredential(), "future-model", []byte(`{"stream":true,"messages":[{"role":"user","content":"synthetic"}]}`))
			if err != nil {
				t.Fatal(err)
			}
			defer r.Body.Close()
			b, err := io.ReadAll(r.Body)
			if err == nil || bytesContain(b, "[DONE]") || bytesContain(b, "SENSITIVE") {
				t.Fatal("bad stream falsely completed or leaked diagnostics")
			}
		})
	}
}
func TestDevinCLIRejectsUnsupportedInputs(t *testing.T) {
	a := devinTestAdapter(t, "normal")
	for _, raw := range []string{`{"tools":[{"type":"function"}],"messages":[]}`, `{"messages":[{"role":"tool","content":"x"}]}`, `{"messages":[{"role":"user","content":[{"type":"image_url"}]}]}`, `{"messages":[{"role":"user","content":"x"}],"reasoning":{"effort":"absent"}}`} {
		r, err := a.Send(context.Background(), devinTestCredential(), "future-model", []byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		if r.StatusCode != http.StatusBadRequest {
			t.Fatalf("status=%d", r.StatusCode)
		}
	}
}
func TestDevinCLIParallelIsolation(t *testing.T) {
	a := devinTestAdapter(t, "normal")
	var wg sync.WaitGroup
	for range 6 {
		wg.Go(func() {
			if status, err := a.Probe(context.Background(), devinTestCredential()); err != nil || status != 200 {
				t.Errorf("status=%d err=%v", status, err)
			}
		})
	}
	wg.Wait()
	entries, _ := os.ReadDir(a.WorkDir)
	if len(entries) != 0 {
		t.Fatal("homes leaked")
	}
}

func TestDevinCLIProviderUsageComponents(t *testing.T) {
	a := devinTestAdapter(t, "usage")
	r, err := a.Send(context.Background(), devinTestCredential(), "future-model", []byte(`{"messages":[{"role":"user","content":"synthetic"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	var out Response
	if json.NewDecoder(r.Body).Decode(&out) != nil || out.Usage.PromptTokens != 10 || out.Usage.CompletionTokens != 20 || out.Usage.CacheReadTokens != 30 || out.Usage.CacheWriteTokens != 40 {
		t.Fatalf("usage=%+v", out.Usage)
	}
}
func TestDevinCLIBooleanConfigAlongsideModel(t *testing.T) {
	var option devinOption
	if err := json.Unmarshal([]byte(`{"id":"toggle","type":"boolean","currentValue":true}`), &option); err != nil {
		t.Fatal(err)
	}
	if option.CurrentValue != "" {
		t.Fatal("boolean is not a select value")
	}
}

// Optional compatibility check with the checksum-verified official binary. Only
// an invalid synthetic key is sent, and session/new must reject it before any
// inference. Never read account tokens from environment or local login files.
func TestDevinCLIRealBinaryProtocol(t *testing.T) {
	binary := os.Getenv("TEST_DEVIN_CLI_BINARY")
	if binary == "" {
		t.Skip("TEST_DEVIN_CLI_BINARY is not set")
	}
	a := &DevinCLIAdapter{Binary: binary, WorkDir: t.TempDir()}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	status, err := a.Probe(ctx, &entities.CredentialRuntime{APIKey: "apk_user_synthetic"})
	if status != 401 || err == nil {
		t.Fatalf("official CLI did not reject synthetic key at authenticated session setup: status=%d err=%v", status, err)
	}
}

func TestDevinCLICapacityWaitRespectsCancellation(t *testing.T) {
	a := devinTestAdapter(t, "hang")
	a.MaxProcesses = 1
	r, err := a.Send(context.Background(), devinTestCredential(), "future-model", []byte(`{"stream":true,"messages":[{"role":"user","content":"synthetic"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	status, err := a.Probe(ctx, devinTestCredential())
	if err == nil || status != 503 {
		t.Fatalf("capacity status=%d err=%v", status, err)
	}
}

func TestDevinRPCErrorClassification(t *testing.T) {
	for _, tc := range []struct {
		code    int
		message string
		status  int
	}{{-32000, "SENSITIVE", 401}, {-32603, "invalid api key SENSITIVE", 401}, {-32603, "quota exceeded SENSITIVE", 429}, {-32602, "SENSITIVE", 400}, {-32603, "SENSITIVE", 502}} {
		err := classifyDevinError(&devinRPCError{tc.code, tc.message})
		if devinStatus(err) != tc.status || strings.Contains(err.Error(), "SENSITIVE") {
			t.Fatalf("status=%d error=%v", devinStatus(err), err)
		}
	}
}

func TestDevinCLIDeniesAgentHostAccess(t *testing.T) {
	a := devinTestAdapter(t, "agent-request")
	r, err := a.Send(context.Background(), devinTestCredential(), "future-model", []byte(`{"messages":[{"role":"user","content":"synthetic"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		t.Fatalf("denial exchange failed: status=%d", r.StatusCode)
	}
}
