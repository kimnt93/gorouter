package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/kimnt93/gorouter/pkg/credential"
	"github.com/kimnt93/gorouter/pkg/entities"
)

// DevinCLIAdapter runs the official Devin CLI over ACP stdio. CLI execution is
// confined to a dedicated empty directory; never use the router's working tree.
// The binary must be installed separately. No shell, prompts in logs, or
// process-local token cache is used.
type DevinCLIAdapter struct {
	Binary  string
	WorkDir string
}

var devinModelID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)

func (a *DevinCLIAdapter) binary() string {
	if a.Binary != "" {
		return a.Binary
	}
	return "devin"
}
func (a *DevinCLIAdapter) directory() string {
	if a.WorkDir != "" {
		return a.WorkDir
	}
	return os.TempDir()
}
func (a *DevinCLIAdapter) command(ctx context.Context, key string, args ...string) (*exec.Cmd, func(), error) {
	if !strings.HasPrefix(key, "apk_user_") {
		return nil, nil, errors.New("Devin CLI requires an apk_user key; Devin Desktop keys use a separate connection")
	}
	dir := a.directory()
	if !filepath.IsAbs(dir) {
		return nil, nil, errors.New("Devin CLI work directory must be absolute")
	}
	// Each credential invocation gets an isolated home and cwd. A shared HOME
	// would allow the CLI to read/write a different user's cached login.
	home, err := os.MkdirTemp(dir, "gorouter-devin-*")
	if err != nil {
		return nil, nil, errors.New("Devin CLI work directory unavailable")
	}
	_ = os.Chmod(home, 0700)
	cmd := exec.CommandContext(ctx, a.binary(), args...)
	cmd.Dir = home
	// Do not inherit other users' Devin tokens or process credentials.
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "WINDSURF_API_KEY=" + key}
	return cmd, func() { _ = os.RemoveAll(home) }, nil
}
func (a *DevinCLIAdapter) Probe(ctx context.Context, cr *entities.CredentialRuntime) (int, error) {
	if cr == nil {
		return 0, errors.New("missing Devin credential")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	// Listing alone may use cached login, not the supplied key. Exercise the
	// same ACP path as chat with a bounded, read-only summarizer request.
	_, err := a.exchange(ctx, cr.APIKey, "", "Reply with exactly: connected")
	if err != nil {
		return 0, err
	}
	return http.StatusOK, nil
}
func parseDevinCLIModels(raw []byte) ([]credential.ProviderModel, error) {
	var doc struct {
		Models []struct {
			ID              string                         `json:"id"`
			Name            string                         `json:"name"`
			Model           string                         `json:"model"`
			ReasoningLevels []entities.ModelReasoningLevel `json:"supported_reasoning_levels"`
		} `json:"models"`
	}
	var records []struct {
		ID              string                         `json:"id"`
		Name            string                         `json:"name"`
		Model           string                         `json:"model"`
		ReasoningLevels []entities.ModelReasoningLevel `json:"supported_reasoning_levels"`
	}
	if json.Unmarshal(raw, &doc) == nil && len(doc.Models) > 0 {
		records = doc.Models
	} else if json.Unmarshal(raw, &records) != nil {
		return nil, errors.New("invalid Devin model list")
	}
	seen := map[string]bool{}
	out := make([]credential.ProviderModel, 0, len(records))
	for _, v := range records {
		id := strings.TrimSpace(v.ID)
		if id == "" {
			id = strings.TrimSpace(v.Model)
		}
		if !devinModelID.MatchString(id) || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, credential.ProviderModel{ID: id, Name: v.Name, Root: id, Object: "model", OwnedBy: "devin-cli", APIFormat: "chat/completions", SupportedEndpoints: []string{"chat/completions"}, SupportedReasoningLevels: v.ReasoningLevels})
	}
	if len(out) == 0 {
		return nil, errors.New("Devin CLI returned no models")
	}
	return out, nil
}
func (a *DevinCLIAdapter) DiscoverModels(ctx context.Context, cr *entities.CredentialRuntime) ([]credential.ProviderModel, error) {
	if cr == nil {
		return nil, errors.New("missing Devin credential")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd, cleanup, err := a.command(ctx, cr.APIKey, "models", "list", "--format", "json")
	if err != nil {
		return nil, err
	}
	defer cleanup()
	var buffer boundedDevinOutput
	cmd.Stdout = &buffer
	if err = cmd.Run(); err != nil || buffer.exceeded {
		return nil, errors.New("Devin CLI model discovery failed")
	}
	return parseDevinCLIModels(buffer.Bytes())
}

type boundedDevinOutput struct {
	bytes.Buffer
	exceeded bool
}

func (b *boundedDevinOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 2<<20 {
		b.exceeded = true
		return 0, errors.New("Devin CLI catalog too large")
	}
	return b.Buffer.Write(p)
}

type acpMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Code int `json:"code"`
	} `json:"error,omitempty"`
}

func (a *DevinCLIAdapter) Send(ctx context.Context, cr *entities.CredentialRuntime, model string, raw []byte) (*entities.UpstreamResult, error) {
	if cr == nil {
		return nil, errors.New("missing Devin credential")
	}
	var req ChatRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, err
	}
	if len(req.Tools) > 0 {
		return nil, errors.New("Devin CLI chat adapter does not support tools")
	}
	if !devinModelID.MatchString(model) {
		return nil, errors.New("invalid Devin CLI model")
	}
	if req.Reasoning != nil && req.Reasoning.Effort != "" {
		return nil, errors.New("Devin CLI ACP does not accept request-scoped reasoning effort; select an available model variant")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	prompt := devinPrompt(req.Messages)
	text, err := a.exchange(ctx, cr.APIKey, model, prompt)
	if err != nil {
		return nil, err
	}
	output := bytes.NewBufferString(text)
	id, _ := randomUUID()
	usage := Usage{PromptTokens: EstimateTextTokens(prompt), CompletionTokens: EstimateTextTokens(output.String())}
	if req.Stream {
		chunk, _ := json.Marshal(Chunk{ID: "chatcmpl-" + id, Object: "chat.completion.chunk", Created: time.Now().Unix(), Model: model, Choices: []ChunkChoice{{Delta: Delta{Role: "assistant", Content: output.String()}}}})
		final, _ := json.Marshal(Chunk{ID: "chatcmpl-" + id, Object: "chat.completion.chunk", Created: time.Now().Unix(), Model: model, Choices: []ChunkChoice{{FinishReason: "stop"}}, Usage: &usage})
		return &entities.UpstreamResult{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(fmt.Sprintf("data: %s\n\ndata: %s\n\ndata: [DONE]\n\n", chunk, final)))}, nil
	}
	response, _ := json.Marshal(Response{ID: "chatcmpl-" + id, Object: "chat.completion", Created: time.Now().Unix(), Model: model, Choices: []Choice{{Message: &ResponseMessage{Role: "assistant", Content: output.String()}, FinishReason: "stop"}}, Usage: usage})
	return &entities.UpstreamResult{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewReader(response))}, nil
}

// exchange never exposes CLI diagnostics (which could contain account or prompt
// data) to the router. The CLI is cancelled after one complete ACP turn.
func (a *DevinCLIAdapter) exchange(ctx context.Context, key, model, prompt string) (string, error) {
	cmd, cleanup, err := a.command(ctx, key, "acp", "--agent-type", "summarizer")
	if err != nil {
		return "", err
	}
	defer cleanup()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return "", err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	if err = cmd.Start(); err != nil {
		return "", errors.New("Devin CLI is not installed or could not start")
	}
	// The caller bounds ctx. Kill on completion too, in case the CLI stays alive.
	var output bytes.Buffer
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	send := func(id int, method string, params any) error {
		line, e := json.Marshal(struct {
			JSONRPC string `json:"jsonrpc"`
			ID      int    `json:"id"`
			Method  string `json:"method"`
			Params  any    `json:"params"`
		}{"2.0", id, method, params})
		if e != nil {
			return e
		}
		_, e = fmt.Fprintln(stdin, string(line))
		return e
	}
	err = send(1, "initialize", map[string]any{"protocolVersion": "0.3", "clientInfo": map[string]string{"name": "gorouter", "version": "1"}, "capabilities": map[string]any{}})
	state := 0
	session := ""

	for err == nil && scanner.Scan() {
		var msg acpMessage
		if json.Unmarshal(scanner.Bytes(), &msg) != nil {
			continue
		}
		if msg.Error != nil {
			err = errors.New("Devin CLI ACP request failed")
			break
		}
		switch {
		case msg.ID == 1 && len(msg.Result) > 0 && state == 0:
			state = 1
			params := map[string]any{"cwd": cmd.Dir, "mcpServers": []any{}}
			if model != "" {
				params["model"] = model
			}
			err = send(2, "session/new", params)
		case msg.ID == 2 && len(msg.Result) > 0 && state == 1:
			var result struct {
				SessionID string `json:"sessionId"`
			}
			if json.Unmarshal(msg.Result, &result) != nil || result.SessionID == "" {
				err = errors.New("Devin CLI omitted session ID")
				break
			}
			session = result.SessionID
			state = 2
			err = send(3, "session/prompt", map[string]any{"sessionId": session, "prompt": []map[string]string{{"type": "text", "text": prompt}}})
		case msg.Method == "session/update" && state == 2:
			var params struct {
				Update struct {
					SessionUpdate string `json:"sessionUpdate"`
					Content       struct {
						Text string `json:"text"`
					} `json:"content"`
				} `json:"update"`
			}
			if json.Unmarshal(msg.Params, &params) == nil && params.Update.SessionUpdate == "agent_message_chunk" {
				if output.Len()+len(params.Update.Content.Text) > 4<<20 {
					err = errors.New("Devin CLI output too large")
					break
				}
				output.WriteString(params.Update.Content.Text)
			}
		case msg.ID == 3 && len(msg.Result) > 0 && state == 2:
			var result struct {
				StopReason string `json:"stopReason"`
			}
			if json.Unmarshal(msg.Result, &result) != nil || result.StopReason == "cancelled" {
				err = errors.New("Devin CLI turn did not finish")
				break
			}
			state = 3
		}
		if state == 3 {
			break
		}
	}
	if err == nil {
		err = scanner.Err()
	}
	if state != 3 && err == nil {
		err = errors.New("Devin CLI did not complete the turn")
	}
	if ctx.Err() != nil {
		err = errors.New("Devin CLI request timed out or was canceled")
	}
	stdin.Close()
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	_ = cmd.Wait()
	if err != nil {
		return "", err
	}
	return output.String(), nil
}
func devinPrompt(messages []Message) string {
	var lines []string
	for _, m := range messages {
		if text := rawText(m.Content); text != "" {
			lines = append(lines, "["+m.Role+"]\n"+text)
		}
	}
	return strings.Join(lines, "\n\n")
}
