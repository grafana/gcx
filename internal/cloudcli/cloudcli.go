// Package cloudcli provides a thin, testable wrapper around native cloud
// provider CLIs (az, aws, gcloud). gcx shells out to these tools to reuse the
// user's local cloud login when onboarding datasources, rather than embedding
// cloud SDKs or handling credentials directly.
package cloudcli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/grafana-app-sdk/logging"
)

// Runner abstracts process execution so tests can inject a fake CLI.
type Runner interface {
	// LookPath reports whether the named binary is resolvable on PATH.
	LookPath(bin string) error
	// Run executes bin with args and returns stdout, stderr and the run error.
	Run(ctx context.Context, bin string, args ...string) (stdout, stderr []byte, err error)
}

// Tool describes a required cloud CLI binary and how to run it.
type Tool struct {
	Bin     string // "az", "aws", "gcloud"
	Name    string // human name, e.g. "Azure CLI"
	DocsURL string // install docs surfaced when the binary is missing

	runner Runner
}

// New constructs a Tool backed by the real os/exec runner.
func New(bin, name, docsURL string) Tool {
	return Tool{Bin: bin, Name: name, DocsURL: docsURL, runner: execRunner{}}
}

// WithRunner returns a copy of the Tool using the supplied runner. Intended
// for tests that inject a fake CLI.
func (t Tool) WithRunner(r Runner) Tool {
	t.runner = r
	return t
}

func (t Tool) r() Runner {
	if t.runner == nil {
		return execRunner{}
	}
	return t.runner
}

// Ensure returns an actionable error when the CLI binary is not on PATH.
// A missing CLI means the user's cloud credentials are unavailable to gcx.
func (t Tool) Ensure() error {
	if err := t.r().LookPath(t.Bin); err != nil {
		return gcxerrors.DetailedError{
			Summary: fmt.Sprintf("%s (%q) was not found on your PATH", t.Name, t.Bin),
			Details: "gcx uses your local cloud CLI session to obtain credentials. Without it, gcx cannot authenticate to your cloud provider.",
			Suggestions: []string{
				fmt.Sprintf("Install the %s: %s", t.Name, t.DocsURL),
				fmt.Sprintf("Sign in with `%s login`, then re-run this command", t.Bin),
			},
		}
	}
	return nil
}

// Run executes the CLI, returning stdout, stderr, and the run error. Every
// invocation is traced to the context logger at Debug level (enabled with -vvv),
// recording the (redacted) command, its duration, and any stderr on failure.
// stdout is never logged because some commands (e.g. credential reset) return
// secrets there.
func (t Tool) Run(ctx context.Context, args ...string) ([]byte, []byte, error) {
	log := logging.FromContext(ctx)
	cmdline := t.Bin + " " + strings.Join(redactArgs(args), " ")
	log.Debug("running cloud CLI command", "command", cmdline)

	start := time.Now()
	stdout, stderr, err := t.r().Run(ctx, t.Bin, args...)
	dur := time.Since(start)

	if err != nil {
		log.Debug("cloud CLI command failed",
			"command", cmdline,
			"duration", dur.String(),
			"error", err.Error(),
			"stderr", string(bytes.TrimSpace(stderr)),
		)
	} else {
		log.Debug("cloud CLI command succeeded", "command", cmdline, "duration", dur.String())
	}
	return stdout, stderr, err
}

// redactArgs masks the value following any flag whose name suggests a secret, so
// command tracing never leaks credentials passed as arguments.
//
// This relies on secrets only ever reaching the CLI through a --secret/--password
// style flag. gcx today never passes a credential as a positional argument or
// embeds one in a --body/--uri payload, and secret-bearing command *output*
// (e.g. `az ad app credential reset`) is protected separately: Run never logs
// stdout. If a future caller passes a secret via --body, --uri, or a positional
// argument, this redaction will NOT catch it — add explicit handling at that
// call site (or extend this matcher) before it ships.
func redactArgs(args []string) []string {
	out := make([]string, len(args))
	redactNext := false
	for i, a := range args {
		if redactNext {
			out[i] = "***"
			redactNext = false
			continue
		}
		out[i] = a
		lower := strings.ToLower(a)
		if strings.HasPrefix(a, "--") && (strings.Contains(lower, "secret") || strings.Contains(lower, "password")) {
			// "--flag value" form redacts the next arg; "--flag=value" is masked inline.
			if name, _, ok := strings.Cut(a, "="); ok {
				out[i] = name + "=***"
			} else {
				redactNext = true
			}
		}
	}
	return out
}

// RunJSON executes the CLI and decodes stdout JSON into out. When out is nil
// the call is run for its side effects and output is discarded.
func (t Tool) RunJSON(ctx context.Context, out any, args ...string) error {
	stdout, stderr, err := t.Run(ctx, args...)
	if err != nil {
		return fmt.Errorf("%s %v failed: %w: %s", t.Bin, args, err, bytes.TrimSpace(stderr))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(stdout, out); err != nil {
		return fmt.Errorf("failed to parse %s output: %w", t.Bin, err)
	}
	return nil
}

// execRunner is the default Runner backed by os/exec.
type execRunner struct{}

func (execRunner) LookPath(bin string) error {
	_, err := exec.LookPath(bin)
	return err
}

func (execRunner) Run(ctx context.Context, bin string, args ...string) ([]byte, []byte, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}
