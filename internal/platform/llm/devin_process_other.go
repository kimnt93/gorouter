//go:build !unix

package llm

import "os/exec"

func configureDevinProcess(cmd *exec.Cmd) { cmd.Cancel = func() error { return cmd.Process.Kill() } }
