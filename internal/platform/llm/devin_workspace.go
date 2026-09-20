package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Per-call identity: never read a host login or share writable CLI files across
// accounts/replicas. The encrypted credential repository remains authoritative.
type devinWorkspace struct {
	ctx               context.Context
	cancel            context.CancelFunc
	binary, home, key string
	once              sync.Once
	release           func()
}

func (a *DevinCLIAdapter) workspace(parent context.Context, key string) (*devinWorkspace, error) {
	key = strings.TrimSpace(key)
	if (!strings.HasPrefix(key, "apk_user_") && !strings.HasPrefix(key, "cog_")) || key == "apk_user_" || key == "cog_" || strings.ContainsAny(key, "\r\n\x00") {
		return nil, devinFailure(400, "Devin requires an apk_user_ key or cog_ personal access token")
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
		return nil, devinFailure(503, "Devin CLI is missing; use the standard GoRouter image")
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
	home, err := os.MkdirTemp(root, "gorouter-devin-")
	if err != nil {
		release()
		return nil, devinFailure(503, "Devin CLI temporary directory unavailable")
	}
	work := &devinWorkspace{ctx: ctx, cancel: cancel, binary: executable, home: home, key: key, release: release}
	if err := work.configure(); err != nil {
		work.Close()
		return nil, devinFailure(503, "Devin private configuration unavailable")
	}
	return work, nil
}

// Exact pinned-CLI configuration: disable every tool (including future tool
// names), deny every tool, disable subagents, imported config and updates.
// This is defense in depth, not an OS sandbox. ACP client methods also fail closed.
type devinConfig struct {
	AutoUpdate    bool     `json:"auto_update"`
	Subagents     bool     `json:"subagents_enabled"`
	DisabledTools []string `json:"disabled_tools"`
	Permissions   struct {
		Deny []string `json:"deny"`
	} `json:"permissions"`
	ReadConfig struct {
		Cursor   bool `json:"cursor"`
		Windsurf bool `json:"windsurf"`
		Claude   bool `json:"claude"`
		Agents   bool `json:"agents_standard"`
	} `json:"read_config_from"`
}

func (w *devinWorkspace) configure() error {
	dataDir := filepath.Join(w.home, ".local", "share", "devin")
	configDir := filepath.Join(w.home, ".config", "devin")
	for _, dir := range []string{dataDir, configDir} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
	}
	// The CLI models subcommand ignores WINDSURF_API_KEY. Its TOML file is needed
	// even for API tokens. Quote the value; never write unescaped user input.
	if err := os.WriteFile(filepath.Join(dataDir, "credentials.toml"), []byte("windsurf_api_key = "+strconv.Quote(w.key)+"\n"), 0600); err != nil {
		return err
	}
	config := devinConfig{DisabledTools: []string{"*"}}
	config.Permissions.Deny = []string{"*"}
	body, err := json.Marshal(config)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(configDir, "config.json"), body, 0600)
}
func (w *devinWorkspace) Close() {
	w.once.Do(func() { w.cancel(); _ = os.RemoveAll(w.home); w.release() })
}
func (w *devinWorkspace) command(args ...string) *exec.Cmd {
	cmd := exec.CommandContext(w.ctx, w.binary, args...)
	cmd.Dir = w.home
	cmd.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=" + w.home, "TMPDIR=" + w.home, "XDG_CONFIG_HOME=" + w.home + "/.config", "XDG_DATA_HOME=" + w.home + "/.local/share", "XDG_CACHE_HOME=" + w.home + "/.cache", "WINDSURF_API_KEY=" + w.key, "DEVIN_PERMISSION_MODE=auto", "DO_NOT_TRACK=1"}
	configureDevinProcess(cmd)
	cmd.WaitDelay = time.Second
	return cmd
}

// Bounded writer drains excess diagnostics without retaining them. Stderr is
// classified in memory, never logged, persisted, or passed to the client.
type devinOutput struct {
	buffer   bytes.Buffer
	limit    int
	overflow bool
}

func (b *devinOutput) Write(p []byte) (int, error) {
	n := len(p)
	remaining := b.limit - b.buffer.Len()
	if n > remaining {
		b.overflow = true
		p = p[:remaining]
	}
	_, _ = b.buffer.Write(p)
	return n, nil
}
func (w *devinWorkspace) catalog() ([]devinModel, error) {
	cmd := w.command("models", "list", "--format", "json")
	stdout := &devinOutput{limit: 4 << 20}
	stderr := &devinOutput{limit: 64 << 10}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Stdin = nil
	if err := cmd.Run(); err != nil {
		if w.ctx.Err() != nil {
			return nil, devinFailure(504, "Devin model discovery timed out or was canceled")
		}
		return nil, classifyDevinError(&devinRPCError{Message: stderr.buffer.String()})
	}
	if stdout.overflow {
		return nil, devinFailure(502, "Devin model catalog exceeds limit")
	}
	var catalog devinCatalog
	decoder := json.NewDecoder(&stdout.buffer)
	if decoder.Decode(&catalog) != nil {
		return nil, devinFailure(502, "Invalid Devin model catalog")
	}
	if decoder.Decode(new(json.RawMessage)) != io.EOF {
		return nil, devinFailure(502, "Invalid Devin model catalog")
	}
	return normalizeDevinCatalog(catalog)
}
