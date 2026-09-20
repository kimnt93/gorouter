package llm

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const devinMaxFrame = 4 << 20

// Errors are classified from provider diagnostics but never expose their body.
type devinError struct {
	status  int
	message string
}

func (e *devinError) Error() string                 { return e.message }
func devinFailure(status int, message string) error { return &devinError{status, message} }

type devinRPC struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *devinRPCError  `json:"error,omitempty"`
}
type devinRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
type devinInitialize struct {
	ProtocolVersion int `json:"protocolVersion"`
	ClientInfo      struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"clientInfo"`
	ClientCapabilities struct {
		FS struct {
			ReadTextFile  bool `json:"readTextFile"`
			WriteTextFile bool `json:"writeTextFile"`
		} `json:"fs"`
		Terminal bool `json:"terminal"`
	} `json:"clientCapabilities"`
}
type devinNewSession struct {
	CWD        string     `json:"cwd"`
	MCPServers []struct{} `json:"mcpServers"`
}
type devinSessionResult struct {
	SessionID     string        `json:"sessionId"`
	ConfigOptions []devinOption `json:"configOptions"`
}
type devinOption struct {
	ID           string       `json:"id"`
	Name         string       `json:"name"`
	Category     string       `json:"category"`
	Type         string       `json:"type"`
	CurrentValue string       `json:"currentValue"`
	Options      []devinValue `json:"options"`
}

// Other ACP selectors may be boolean; they must not make the model selector
// undecodable. Only string current values are used for our select operations.
func (o *devinOption) UnmarshalJSON(b []byte) error {
	type option devinOption
	var wire struct {
		*option
		Current json.RawMessage `json:"currentValue"`
	}
	wire.option = (*option)(o)
	if err := json.Unmarshal(b, &wire); err != nil {
		return err
	}
	if len(wire.Current) > 0 && wire.Current[0] == '"' {
		return json.Unmarshal(wire.Current, &o.CurrentValue)
	}
	o.CurrentValue = ""
	return nil
}

// ACP permits a flat list or groups of select values.
type devinValue struct {
	Value       string       `json:"value"`
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Group       string       `json:"group"`
	Options     []devinValue `json:"options"`
}
type devinSetOption struct {
	SessionID string `json:"sessionId"`
	ConfigID  string `json:"configId"`
	Value     string `json:"value"`
}
type devinContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}
type devinPromptParams struct {
	SessionID string         `json:"sessionId"`
	Prompt    []devinContent `json:"prompt"`
}
type devinUpdate struct {
	SessionID string `json:"sessionId"`
	Update    struct {
		Kind          string        `json:"sessionUpdate"`
		Content       devinContent  `json:"content"`
		ConfigOptions []devinOption `json:"configOptions"`
	} `json:"update"`
}

type devinSession struct {
	ctx       context.Context
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	scanner   *bufio.Scanner
	cancel    context.CancelFunc
	cleanup   func()
	once      sync.Once
	id        int
	sessionID string
	options   []devinOption
}

func (a *DevinCLIAdapter) open(parent context.Context, key string) (*devinSession, error) {
	if !strings.HasPrefix(key, "apk_user_") || strings.TrimSpace(key) == "apk_user_" {
		return nil, devinFailure(400, "Devin CLI requires an apk_user key")
	}
	ctx, cancel := context.WithTimeout(parent, 3*time.Minute)
	a.once.Do(func() {
		limit := a.MaxProcesses
		if limit <= 0 {
			limit = 4
		}
		a.slots = make(chan struct{}, limit)
	})
	select {
	case a.slots <- struct{}{}:
	case <-ctx.Done():
		cancel()
		return nil, devinFailure(503, "Devin CLI capacity unavailable")
	}
	release := func() { cancel(); <-a.slots }
	binary := a.Binary
	if binary == "" {
		binary = "devin"
	}
	executable, err := exec.LookPath(binary)
	if err != nil {
		release()
		return nil, devinFailure(503, "Devin CLI is missing; update to the standard GoRouter Docker image or install devin for a standalone binary deployment")
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		release()
		return nil, devinFailure(503, "Devin CLI executable unavailable")
	}
	root := a.WorkDir
	if root == "" {
		root = os.TempDir()
	}
	if !filepath.IsAbs(root) {
		release()
		return nil, devinFailure(503, "Devin CLI temporary directory must be absolute")
	}
	home, err := os.MkdirTemp(root, "gorouter-devin-*")
	if err != nil {
		release()
		return nil, devinFailure(503, "Devin CLI temporary directory unavailable")
	}
	clean := func() { _ = os.RemoveAll(home); release() }
	cmd := exec.CommandContext(ctx, executable, "acp", "--agent-type", "summarizer")
	cmd.Dir = home
	// No inherited config, login, proxy credential, plugin, or system prompt.
	cmd.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=" + home, "XDG_CONFIG_HOME=" + home + "/.config", "XDG_DATA_HOME=" + home + "/.local/share", "XDG_CACHE_HOME=" + home + "/.cache", "TMPDIR=" + home, "WINDSURF_API_KEY=" + key, "DO_NOT_TRACK=1"}
	configureDevinProcess(cmd)
	cmd.WaitDelay = time.Second
	stdin, err := cmd.StdinPipe()
	if err != nil {
		clean()
		return nil, devinFailure(503, "Devin CLI pipe unavailable")
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		clean()
		return nil, devinFailure(503, "Devin CLI pipe unavailable")
	}
	if err = cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		clean()
		return nil, devinFailure(503, "Devin CLI could not start")
	}
	s := &devinSession{ctx: ctx, cmd: cmd, stdin: stdin, scanner: bufio.NewScanner(stdout), cancel: cancel, cleanup: clean}
	s.scanner.Buffer(make([]byte, 4096), devinMaxFrame)
	init := devinInitialize{ProtocolVersion: 1}
	init.ClientInfo.Name = "gorouter"
	init.ClientInfo.Version = "1"
	var initialized struct {
		ProtocolVersion int `json:"protocolVersion"`
	}
	if err = s.call("initialize", init, &initialized, nil); err == nil && initialized.ProtocolVersion != 1 {
		err = devinFailure(502, "unsupported Devin ACP version")
	}
	if err != nil {
		s.Close()
		return nil, err
	}
	var session devinSessionResult
	if err = s.call("session/new", devinNewSession{CWD: home, MCPServers: []struct{}{}}, &session, nil); err != nil {
		s.Close()
		return nil, err
	}
	if session.SessionID == "" {
		s.Close()
		return nil, devinFailure(502, "Devin CLI omitted session ID")
	}
	s.sessionID = session.SessionID
	s.options = session.ConfigOptions
	return s, nil
}
func (s *devinSession) Close() {
	s.once.Do(func() { s.cancel(); _ = s.stdin.Close(); _ = s.cmd.Cancel(); _ = s.cmd.Wait(); s.cleanup() })
}
func (s *devinSession) write(msg devinRPC) error {
	msg.JSONRPC = "2.0"
	b, err := json.Marshal(msg)
	if err != nil {
		return devinFailure(502, "invalid ACP request")
	}
	if _, err = s.stdin.Write(append(b, '\n')); err != nil {
		return devinFailure(502, "Devin CLI disconnected")
	}
	return nil
}
func (s *devinSession) call(method string, params any, result any, visit func(devinUpdate) error) error {
	s.id++
	id := strconv.Itoa(s.id)
	raw, err := json.Marshal(params)
	if err != nil {
		return devinFailure(400, "invalid Devin request")
	}
	if err = s.write(devinRPC{ID: json.RawMessage(id), Method: method, Params: raw}); err != nil {
		return err
	}
	// Bound notifications/noise, in addition to frame size and process timeout.
	total := 0
	for s.scanner.Scan() {
		line := s.scanner.Bytes()
		total += len(line)
		if total > 32<<20 {
			return devinFailure(502, "Devin response exceeds limit")
		}
		if err = s.ctx.Err(); err != nil {
			return devinFailure(504, "Devin CLI request canceled or timed out")
		}
		var msg devinRPC
		if json.Unmarshal(line, &msg) != nil || msg.JSONRPC != "2.0" {
			return devinFailure(502, "invalid Devin ACP frame")
		}
		if msg.Method != "" {
			if len(msg.ID) > 0 { // Never allow a provider to run tools or read the host.
				if err = s.write(devinRPC{ID: msg.ID, Error: &devinRPCError{Code: -32601, Message: "client method unavailable"}}); err != nil {
					return err
				}
				continue
			}
			if msg.Method == "session/update" {
				var u devinUpdate
				if json.Unmarshal(msg.Params, &u) != nil {
					return devinFailure(502, "invalid Devin update")
				}
				if s.sessionID != "" && u.SessionID != s.sessionID {
					continue
				}
				if u.Update.Kind == "config_option_update" || u.Update.Kind == "config_options_update" {
					s.options = u.Update.ConfigOptions
				}
				if visit != nil {
					if err = visit(u); err != nil {
						return err
					}
				}
			}
			continue
		}
		if string(msg.ID) != id {
			return devinFailure(502, "unexpected Devin ACP response ID")
		}
		if msg.Error != nil {
			return classifyDevinError(msg.Error)
		}
		if len(msg.Result) == 0 || string(msg.Result) == "null" || json.Unmarshal(msg.Result, result) != nil {
			return devinFailure(502, "invalid Devin ACP result")
		}
		return nil
	}
	if s.ctx.Err() != nil {
		return devinFailure(504, "Devin CLI request canceled or timed out")
	}
	return devinFailure(502, "Devin CLI exited without a complete response")
}
func classifyDevinError(e *devinRPCError) error {
	text := strings.ToLower(e.Message)
	switch {
	case e.Code == -32000 || strings.Contains(text, "authentication required") || strings.Contains(text, "invalid api key") || strings.Contains(text, "not logged in"):
		return devinFailure(401, "Devin CLI authentication failed")
	case strings.Contains(text, "rate limit") || strings.Contains(text, "quota exceeded"):
		return devinFailure(429, "Devin CLI quota or rate limit reached")
	case e.Code == -32602:
		return devinFailure(400, "Devin CLI rejected request parameters")
	default:
		return devinFailure(502, "Devin CLI request failed")
	}
}
func (s *devinSession) selector(category string) *devinOption {
	for i := range s.options {
		o := &s.options[i]
		if o.Type == "select" && (o.Category == category || o.ID == category) {
			return o
		}
	}
	return nil
}
func devinValues(o *devinOption) []devinValue {
	if o == nil {
		return nil
	}
	out := []devinValue{}
	for _, v := range o.Options {
		if v.Group != "" {
			out = append(out, v.Options...)
		} else {
			out = append(out, v)
		}
	}
	return out
}
func (s *devinSession) selectValue(category, value string) error {
	o := s.selector(category)
	if o == nil {
		return devinFailure(400, "Devin CLI does not advertise the requested selector")
	}
	valid := false
	for _, v := range devinValues(o) {
		if v.Value == value {
			valid = true
			break
		}
	}
	if !valid {
		return devinFailure(400, "Devin CLI does not allow the selected model or reasoning level")
	}
	if o.CurrentValue == value {
		return nil
	}
	var result struct {
		Options []devinOption `json:"configOptions"`
	}
	if err := s.call("session/set_config_option", devinSetOption{s.sessionID, o.ID, value}, &result, nil); err != nil {
		return err
	}
	s.options = result.Options
	if selected := s.selector(category); selected == nil || selected.CurrentValue != value {
		return devinFailure(502, "Devin CLI did not confirm configuration change")
	}
	return nil
}

func devinStatus(err error) int {
	var e *devinError
	if errors.As(err, &e) {
		return e.status
	}
	return 502
}
func devinSafeError(err error) error {
	var e *devinError
	if errors.As(err, &e) {
		return e
	}
	return errors.New("Devin CLI response interrupted")
}
