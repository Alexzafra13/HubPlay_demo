//go:build unix

package procutil

import (
	"os/exec"
	"testing"
	"time"
)

func TestSetProcessGroup_SetsFlag(t *testing.T) {
	cmd := exec.Command("sleep", "1")
	SetProcessGroup(cmd)
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
		t.Fatal("SetProcessGroup did not set Setpgid")
	}
}

func TestKillProcessGroup_NilSafe(t *testing.T) {
	if err := KillProcessGroup(nil); err != nil {
		t.Fatalf("nil cmd: %v", err)
	}
	// A command that was never started has a nil Process.
	if err := KillProcessGroup(exec.Command("true")); err != nil {
		t.Fatalf("not-started cmd: %v", err)
	}
}

func TestKillProcessGroup_KillsRunningProcess(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	SetProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start sleep: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	if err := KillProcessGroup(cmd); err != nil {
		t.Fatalf("KillProcessGroup: %v", err)
	}
	select {
	case <-done:
		// process reaped — killed as expected
	case <-time.After(5 * time.Second):
		t.Fatal("process not killed within 5s")
	}
}
