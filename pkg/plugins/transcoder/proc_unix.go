//go:build unix

package transcoder

import (
	"os/exec"
	"syscall"
)

// configureProcGroup puts the child in its own process group so a cancel or
// timeout can signal the whole tree (FFmpeg/ffprobe spawn helper processes),
// not just the direct child.
func configureProcGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// killProcGroup kills the child's entire process group. With Setpgid the
// child's pid equals its process-group id.
func killProcGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
		return cmd.Process.Kill()
	}
	return nil
}
