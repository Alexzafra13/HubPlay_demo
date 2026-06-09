//go:build unix

// Package procutil centralises cross-platform process-group handling for
// the external child processes HubPlay spawns (ffmpeg for VOD transcode
// and IPTV transmux).
//
// ffmpeg with hardware acceleration (VAAPI/NVENC) or certain protocol
// handlers can fork helper subprocesses. exec.CommandContext's default
// cancellation only SIGKILLs the direct child, which can leave those
// grandchildren orphaned — holding a transcode/transmux slot and, worse,
// a wedged GPU context. Putting the child in its own process group and
// signalling the whole group on teardown guarantees the entire tree dies.
package procutil

import (
	"os/exec"
	"syscall"
)

// SetProcessGroup makes cmd start as the leader of a new process group so
// the whole process tree can be signalled at once via the negative-PID
// group target. Call before cmd.Start().
func SetProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// KillProcessGroup sends SIGKILL to the process group led by cmd's
// process. Because SetProcessGroup made the child a group leader
// (PGID == PID), signalling -PID reaches the child and every descendant,
// so no orphaned ffmpeg / GPU helper survives. Falls back to killing the
// single process if the group signal fails (e.g. Setpgid never took).
// Safe to call when the process never started or already exited.
func KillProcessGroup(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	pgid := cmd.Process.Pid
	if err := syscall.Kill(-pgid, syscall.SIGKILL); err != nil {
		return cmd.Process.Kill()
	}
	return nil
}
