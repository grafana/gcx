//go:build !wasip1

package ext

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"time"
)

type RunOptions struct {
	Args                                []string
	Binary, Context, AgentMode, Version string
	Stdin                               io.Reader
	Stdout, Stderr                      io.Writer
}

func (s *Store) Run(ctx context.Context, name string, opts RunOptions) (int, error) {
	if err := ctx.Err(); err != nil {
		return 5, err
	}
	lock, err := s.Lock(true)
	if err != nil {
		return 1, err
	}
	defer lock.Close()
	installed, err := s.Lookup(name)
	if err != nil {
		return 1, err
	}
	if err := CheckVersion(installed.Manifest, opts.Version); err != nil {
		return 1, err
	}
	cmd := exec.CommandContext(ctx, installed.Entrypoint, opts.Args...) //nolint:gosec // The user approved installing this executable; no shell is involved.
	cmd.Stdin, cmd.Stdout, cmd.Stderr = opts.Stdin, opts.Stdout, opts.Stderr
	cmd.Env = append(os.Environ(), "GCX_EXT_GCX_BIN="+opts.Binary, "GCX_CONTEXT="+opts.Context, "GCX_AGENT_MODE="+opts.AgentMode, "GCX_EXT_NAME="+name)
	cmd.Cancel = func() error { return interrupt(cmd.Process) }
	cmd.WaitDelay = 5 * time.Second
	err = cmd.Run()
	var child *exec.ExitError
	if errors.As(err, &child) {
		return processExitCode(child), nil
	}
	// CommandContext can return cancellation even when a child handles the
	// interrupt and exits successfully. After launch the child's status wins.
	if cmd.ProcessState != nil && cmd.ProcessState.Success() {
		return 0, nil
	}
	return 0, err
}
