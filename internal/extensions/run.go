package extensions

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
)

// Environment variables gcx sets on every extension subprocess. Extensions
// never receive a Grafana or Cloud credential: they reach Grafana by invoking
// GCXBinEnv and consuming its JSON output, inheriting gcx's auth and refresh.
const (
	// GCXBinEnv is the absolute path to the gcx binary that dispatched the run.
	GCXBinEnv = "GCX_EXT_GCX_BIN"
	// ContextEnv is the config context the parent gcx invocation resolved,
	// including one supplied by --context. Extensions must pass it back on
	// every gcx call so a --context on the outer command is not silently lost.
	ContextEnv = "GCX_EXT_CONTEXT"
	// AgentModeEnv is "true" when the parent gcx is in agent mode, so an
	// extension can make the same structured-output choice gcx made.
	AgentModeEnv = "GCX_EXT_AGENT_MODE"
	// VersionEnv is the version of the dispatching gcx.
	VersionEnv = "GCX_EXT_VERSION"
	// NameEnv is the name the extension was invoked under.
	NameEnv = "GCX_EXT_NAME"
)

// RunOptions carries everything needed to dispatch an installed extension.
type RunOptions struct {
	Args       []string
	Stdin      io.Reader
	Stdout     io.Writer
	Stderr     io.Writer
	GCXBin     string
	Context    string
	AgentMode  bool
	GCXVersion string
}

// ExitError reports that the extension ran and exited non-zero. The code is
// propagated verbatim so an extension owns its own exit-code contract.
type ExitError struct {
	Name string
	Code int
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("extension %q exited with code %d", e.Name, e.Code)
}

// Run executes an installed extension, forwarding stdio and its exit code.
func (e *Installed) Run(ctx context.Context, opts RunOptions) error {
	if _, err := os.Stat(e.Entrypoint); err != nil {
		return fmt.Errorf("extension %q is recorded in the index but its entrypoint is missing (%s); reinstall it", e.Name, e.Entrypoint)
	}

	cmd := exec.CommandContext(ctx, e.Entrypoint, opts.Args...) //nolint:gosec // dispatching an explicitly installed extension is the feature
	cmd.Stdin = opts.Stdin
	cmd.Stdout = opts.Stdout
	cmd.Stderr = opts.Stderr
	cmd.Env = append(os.Environ(),
		GCXBinEnv+"="+opts.GCXBin,
		ContextEnv+"="+opts.Context,
		AgentModeEnv+"="+boolString(opts.AgentMode),
		VersionEnv+"="+opts.GCXVersion,
		NameEnv+"="+e.Name,
	)

	err := cmd.Run()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return &ExitError{Name: e.Name, Code: exitErr.ExitCode()}
	}
	return err
}

func boolString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
