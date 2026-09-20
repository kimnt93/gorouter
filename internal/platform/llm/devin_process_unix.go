//go:build unix

package llm

import (
	"os"
	"os/exec"
	"syscall"
)

// Kill the process group as well as the CLI so cancellation cannot orphan
// children holding pipes open. Temporary HOME cleanup happens after Wait.
func configureDevinProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if err == syscall.ESRCH {
			return os.ErrProcessDone
		}
		return err
	}
}
