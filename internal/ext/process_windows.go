package ext

import (
	"os"
	"os/exec"
)

func interrupt(p *os.Process) error           { return p.Kill() }
func processExitCode(err *exec.ExitError) int { return err.ExitCode() }
