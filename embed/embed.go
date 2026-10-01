// Package embed runs gcx commands in-process, for programs that embed gcx
// rather than shelling out to it — for example a multi-tenant MCP server.
//
// Every invocation is isolated: credentials come from [Options] instead of
// config files, the keychain or the environment; output is captured instead
// of written to the process's stdio; and the host filesystem, subprocesses,
// listeners and signals are unavailable (see internal/host). Commands that
// need those fail with an explanatory error. Concurrent calls to [Run] are
// safe.
package embed

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/fatih/color"
	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/cmd/gcx/root"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/host"
	"github.com/grafana/gcx/internal/style"
	"github.com/grafana/gcx/internal/terminal"
	"github.com/grafana/gcx/internal/version"
)

// contextName names the single config context built for each invocation.
const contextName = "embedded"

// Options configures a single [Run].
type Options struct {
	// Grafana is the Grafana instance commands talk to. Required.
	Grafana Grafana
	// Cloud enables Grafana Cloud platform commands (stacks, access policies,
	// and products such as Synthetic Monitoring or k6 whose credentials gcx
	// derives through the Grafana Cloud API). Optional.
	Cloud *Cloud
	// Env is the complete environment visible to the command; the process
	// environment is never read.
	Env map[string]string
	// Stdin is supplied to commands reading from "-" (e.g. -f -).
	Stdin string
}

// Grafana describes a Grafana instance and how to authenticate to it. Set at
// most one of Token or User/Password.
type Grafana struct {
	// URL is the Grafana server, e.g. https://example.grafana.net. Required.
	URL string
	// Token is a service account token or other bearer token.
	Token string
	// User and Password select basic authentication.
	User, Password string
	// OrgID selects the organization on self-hosted Grafana.
	OrgID int64
	// StackID is the Grafana Cloud stack ID. When zero gcx discovers it.
	StackID int64
	// StackSlug is the Grafana Cloud stack slug. When empty gcx derives it
	// from URL.
	StackSlug string
	// Headers are sent with every request to Grafana, e.g. on-behalf-of
	// credentials for an authenticating proxy.
	Headers map[string]string
	// TLS configures the connection to Grafana. Optional.
	TLS *TLS
}

// TLS holds PEM-encoded TLS material.
type TLS struct {
	CertPEM, KeyPEM, CAPEM []byte
	ServerName             string
	InsecureSkipVerify     bool
}

// Cloud holds Grafana Cloud API credentials.
type Cloud struct {
	// Token is a Grafana Cloud access policy token.
	Token string
	// APIURL overrides the Grafana Cloud API, https://grafana.com by default.
	APIURL string
}

// Result is the outcome of a command that ran.
type Result struct {
	// Stdout is the command's result. In embedded mode gcx always runs as an
	// agent, so this is JSON, including the error document when the command
	// fails.
	Stdout string
	// Stderr carries diagnostics such as warnings and -v logs.
	Stderr string
	// ExitCode is gcx's exit code; zero means success.
	ExitCode int
}

// init fixes gcx's process-wide output state for embedded use. gcx keeps
// TTY, color and agent-mode state in package globals that the CLI sets per
// invocation, which would race between concurrent embedded calls. Embedded
// output never goes to a terminal, so importing this package sets that state
// once for the whole process.
func init() { //nolint:gochecknoinits // Process-wide output state must be fixed before any concurrent Run.
	agent.SetFlag(true)
	terminal.SetPiped(true)
	terminal.SetNoTruncate(true)
	color.NoColor = true
	style.SetEnabled(false)
}

// Run executes command — a gcx command line such as
// `slo definitions list -o json`, with or without the leading "gcx" — and
// returns its output. The command is split using shell quoting rules but is
// never passed to a shell: pipes, redirects and expansions are rejected.
//
// The returned error reports a command that could not be started (invalid
// options or command syntax). A command that runs and fails returns a Result
// with a non-zero ExitCode and a nil error.
func Run(ctx context.Context, command string, opts Options) (Result, error) {
	if opts.Grafana.URL == "" {
		return Result{}, errors.New("embed: Grafana URL is required")
	}
	args, err := splitCommand(command)
	if err != nil {
		return Result{}, fmt.Errorf("embed: %w", err)
	}

	var stdout, stderr bytes.Buffer
	ctx = host.WithSandbox(ctx, &host.Sandbox{
		Args:   append([]string{"gcx"}, args...),
		Env:    opts.Env,
		Stdin:  strings.NewReader(opts.Stdin),
		Stdout: &stdout,
		Stderr: &stderr,
	})
	ctx = config.ContextWithInMemoryConfig(ctx, opts.config())

	cmd := root.Command(version.Get()) //nolint:contextcheck // Cobra receives the caller's context through ExecuteContext below.
	cmd.SetArgs(args)
	cmd.SetIn(host.Stdin(ctx))
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	var exitCode int
	if err := root.ValidateArgs(cmd, args); err != nil {
		exitCode = reportError(ctx, err)
	} else {
		exitCode = reportError(ctx, cmd.ExecuteContext(ctx))
	}

	return Result{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
	}, nil
}

// reportError renders err the way the gcx CLI does in agent mode and returns
// the exit code. It mirrors cmd/gcx's reportError minus telemetry and the
// agent invocation log, which belong to the CLI process.
func reportError(ctx context.Context, err error) int {
	if err == nil {
		return 0
	}
	if errors.Is(err, context.Canceled) {
		return gcxerrors.ExitCancelled
	}
	if exitCode, ok := gcxerrors.AlreadyReportedExitCode(err); ok {
		return exitCode
	}
	var emitted *gcxerrors.EmittedError
	if errors.As(err, &emitted) {
		return emitted.Code
	}

	detailed := fail.ErrorToDetailedError(err)
	if detailed == nil {
		return 1
	}
	exitCode := 1
	if detailed.ExitCode != nil {
		exitCode = *detailed.ExitCode
	}
	if writeErr := detailed.WriteJSON(host.Stdout(ctx), exitCode); writeErr != nil {
		fmt.Fprintln(host.Stderr(ctx), detailed.Error())
	}
	return exitCode
}

// config builds the single-context gcx config for opts.
func (opts Options) config() config.Config {
	g := opts.Grafana
	grafana := &config.GrafanaConfig{
		Server:   strings.TrimSuffix(g.URL, "/"),
		APIToken: g.Token,
		User:     g.User,
		Password: g.Password,
		OrgID:    g.OrgID,
		StackID:  g.StackID,
		Headers:  g.Headers,
	}
	if g.TLS != nil {
		grafana.TLS = &config.TLS{
			CertData:   g.TLS.CertPEM,
			KeyData:    g.TLS.KeyPEM,
			CAData:     g.TLS.CAPEM,
			ServerName: g.TLS.ServerName,
			Insecure:   g.TLS.InsecureSkipVerify,
		}
	}

	cfgCtx := &config.Context{Stack: contextName}
	cfg := config.Config{
		Version:        config.ConfigVersion,
		CurrentContext: contextName,
		Stacks:         map[string]*config.StackConfig{contextName: {Slug: g.StackSlug, Grafana: grafana}},
		Contexts:       map[string]*config.Context{contextName: cfgCtx},
	}
	if opts.Cloud != nil {
		cfgCtx.Cloud = contextName
		cfg.Cloud = map[string]*config.CloudEntry{contextName: {
			Token:  opts.Cloud.Token,
			APIUrl: opts.Cloud.APIURL,
		}}
	}
	return cfg
}
