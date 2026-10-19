//go:build !windows && !wasip1

package ext

import (
	"os"
	"os/exec"
	"syscall"
)

func interrupt(p *os.Process) error { return p.Signal(os.Interrupt) }
func processExitCode(err *exec.ExitError) int {
	if status, ok := err.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		return 128 + int(status.Signal())
	}
	return err.ExitCode()
}
