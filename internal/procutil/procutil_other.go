//go:build !unix

package procutil

import "os/exec"

// SetProcessGroup is a no-op on platforms without POSIX process groups
// (Windows). The single-process kill path below is the best available.
func SetProcessGroup(cmd *exec.Cmd) {}

// KillProcessGroup kills the single process; process-group semantics
// aren't available on this platform. Safe on a nil/never-started process.
func KillProcessGroup(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
