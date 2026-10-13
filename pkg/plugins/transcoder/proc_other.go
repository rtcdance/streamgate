//go:build !unix

package transcoder

import "os/exec"

// configureProcGroup is a no-op on platforms without process groups.
func configureProcGroup(cmd *exec.Cmd) {}

// killProcGroup falls back to killing the direct child only.
func killProcGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
