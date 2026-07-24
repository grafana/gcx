// Package assistant provides the assistant command group for interacting with Grafana Assistant.
package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/assistant"
	"github.com/grafana/gcx/internal/assistant/investigations"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/docs"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/httputils"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	mcpserverscmd "github.com/grafana/gcx/internal/providers/assistant/mcpservers"
	"github.com/spf13/cobra"
	"k8s.io/client-go/rest"
)

func requireGrafanaCloud(ctx *config.Context) error {
	if ctx.Grafana == nil || ctx.Grafana.Server == "" {
		return nil
	}
	if !ctx.IsCloud() {
		return gcxerrors.DetailedError{
			Summary: "Unsupported command",
			Details: "Due to technical limitations of how gcx interacts with Grafana Assistant, " +
				"`gcx assistant` commands do not currently work with self-hosted Grafana instances.",
		}
	}
	return nil
}

// Command returns the assistant command group.
func Command() *cobra.Command {
	// A single ConfigLoader is shared across every subcommand (prompt,
	// dashboard, conversation, investigations, mcp-servers). --config is bound
	// on the group's persistent flags; --context is the root command's global
	// flag, threaded into context.Context and read back via
	// config.ContextNameFromCtx — so no per-subcommand flag-copying is needed.
	loader := &providers.ConfigLoader{}

	cmd := &cobra.Command{
		Use:   "assistant",
		Short: "Interact with Grafana Assistant",
		Long: `Send prompts to Grafana Assistant and receive streaming responses via the A2A protocol.

Requires Grafana Cloud with OAuth authentication (gcx login with browser flow).
Service account tokens are not supported.

Note: Grafana Assistant is billed based on tokens consumed, including requests
made through gcx. See ` + docs.AssistantPricing + `.`,
	}
	// We need a "before each run" hook to block assistant commands on self-hosted
	// instances. Defining one here replaces the root command's hook (cobra doesn't
	// stack them), so we call the root's hook manually first. The root != cmd
	// guard avoids self-recursion when there's no parent (in tests). Running the
	// root hook first also threads --context into c.Context() before the loader
	// resolves it.
	cmd.PersistentPreRunE = func(c *cobra.Command, args []string) error {
		if root := c.Root(); root != cmd {
			if root.PersistentPreRunE != nil {
				if err := root.PersistentPreRunE(c, args); err != nil {
					return err
				}
			} else if root.PersistentPreRun != nil {
				root.PersistentPreRun(c, args)
			}
		}
		cfg, err := loader.LoadConfigTolerant(c.Context())
		if err != nil {
			return err
		}
		if curCtx := cfg.Contexts[cfg.CurrentContext]; curCtx != nil {
			return requireGrafanaCloud(curCtx)
		}
		return nil
	}

	loader.BindFlags(cmd.PersistentFlags())
	cmd.AddCommand(promptCommand(loader))
	cmd.AddCommand(dashboardCommand(loader))
	cmd.AddCommand(conversationCommand(loader))
	cmd.AddCommand(investigations.Commands(loader))
	cmd.AddCommand(mcpserverscmd.Commands(loader))
	return cmd
}

// promptOpts holds options for the prompt subcommand.
type promptOpts struct {
	timeout   int
	contextID string
	cont      bool // --continue
	jsonOut   bool
	noStream  bool
	agentID   string
}

// setup binds the shared streaming flags. If exposeAgentID is true, the
// --agent-id flag is also bound; subcommands that target a fixed agent (e.g.
// `assistant dashboard`) pass false and pre-populate o.agentID instead.
func (o *promptOpts) setup(cmd *cobra.Command, exposeAgentID bool) {
	cmd.Flags().IntVar(&o.timeout, "timeout", 300, "Timeout in seconds when waiting for a response")
	cmd.Flags().StringVar(&o.contextID, "context-id", "", "Context ID for conversation threading")
	cmd.Flags().BoolVar(&o.cont, "continue", false, "Continue the previous chat session")
	cmd.Flags().BoolVar(&o.jsonOut, "json", false, "Output as JSON (streams NDJSON events by default)")
	cmd.Flags().BoolVar(&o.noStream, "no-stream", false, "With --json, emit a single JSON object instead of streaming events")
	if exposeAgentID {
		cmd.Flags().StringVar(&o.agentID, "agent-id", assistant.DefaultAgentID, "Agent ID to target (e.g. grafana_assistant_cli, grafana_dashboarding)")
	}
}

func (o *promptOpts) Validate() error {
	if o.contextID != "" && o.cont {
		return errors.New("cannot use both --context-id and --continue flags")
	}
	if o.timeout <= 0 {
		return errors.New("--timeout must be positive")
	}
	return nil
}

// promptResult represents the result for JSON output.
type promptResult struct {
	TaskID    string `json:"taskId,omitempty"`
	ContextID string `json:"contextId,omitempty"`
	Status    string `json:"status"`
	Response  string `json:"response,omitempty"`
	Timeout   int    `json:"timeout,omitempty"`
	Error     string `json:"error,omitempty"`
}

func promptCommand(configOpts *providers.ConfigLoader) *cobra.Command {
	opts := &promptOpts{}

	cmd := &cobra.Command{
		Use:   "prompt <message>",
		Short: "Send a single message to Grafana Assistant",
		Long: `Send a single message to Grafana Assistant and receive the response.

This is useful for scripting and automation. The response streams via
the A2A (Agent-to-Agent) protocol over Server-Sent Events.

Known agent IDs:
  grafana_assistant_cli   General-purpose assistant (default)
  grafana_dashboarding    Dashboard builder — queries live Prometheus to discover
                          metrics and returns complete dashboard JSON ready for
                          'gcx resources push'. See also: gcx assistant dashboard

Note: each prompt consumes billable Grafana Assistant tokens, including requests
made through gcx. See ` + docs.AssistantPricing + `.`,
		Args: cobra.ExactArgs(1),
		Example: `  gcx assistant prompt "What alerts are firing?"
  gcx assistant prompt "Show CPU usage" --json
  gcx assistant prompt "Follow up" --continue
  gcx assistant prompt "Build a CPU dashboard" --agent-id grafana_dashboarding`,
		Annotations: map[string]string{
			agent.AnnotationTokenCost: "large",
			agent.AnnotationLLMHint:   "Prefer deterministic gcx commands (gcx metrics query, gcx slo definitions status, gcx alert instances list) for precise data retrieval. Use assistant prompt for reasoning: root cause analysis, holistic health questions, or when you don't know which metrics/labels exist — the Assistant's Infrastructure Memories know your stack topology. Each prompt consumes billable Grafana Assistant tokens (" + docs.AssistantPricing + "). Example: \"Why is checkout-latency spiking?\" --json",
		},
		RunE: promptRunE(opts, configOpts),
	}

	opts.setup(cmd, true)
	return cmd
}

// dashboardCommand returns a subcommand that routes to the grafana_dashboarding
// agent. It queries live Prometheus to discover metrics and returns complete
// dashboard JSON ready for 'gcx resources push'.
func dashboardCommand(configOpts *providers.ConfigLoader) *cobra.Command {
	opts := &promptOpts{agentID: "grafana_dashboarding"}

	cmd := &cobra.Command{
		Use:   "dashboard <message>",
		Short: "Build a dashboard using the Grafana dashboarding agent",
		Long: `Send a dashboard creation request to the Grafana dashboarding agent.

The agent queries live Prometheus to discover available clusters and metric
names, then returns complete dashboard JSON that can be pushed directly with
'gcx resources push'.

This is equivalent to:
  gcx assistant prompt --agent-id grafana_dashboarding <message>

Note: each request consumes billable Grafana Assistant tokens, including
requests made through gcx. See ` + docs.AssistantPricing + `.`,
		Args: cobra.ExactArgs(1),
		Example: `  gcx assistant dashboard "Build a CPU usage dashboard across all clusters"
  gcx assistant dashboard "Create a dashboard for HTTP error rates by service" --json`,
		Annotations: map[string]string{
			agent.AnnotationTokenCost: "large",
			agent.AnnotationLLMHint:   "Use assistant dashboard to build Grafana dashboards from natural language. The agent discovers live Prometheus metrics and returns complete dashboard JSON. Pipe the result to 'gcx resources push' to publish it. Each request consumes billable Grafana Assistant tokens (" + docs.AssistantPricing + ").",
		},
		RunE: promptRunE(opts, configOpts),
	}

	opts.setup(cmd, false)
	return cmd
}

// promptRunE returns the RunE used by both `prompt` and `dashboard` — the only
// per-command difference is the pre-populated agent ID on opts.
func promptRunE(opts *promptOpts, configOpts *providers.ConfigLoader) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		if err := opts.Validate(); err != nil {
			return err
		}
		return runPrompt(cmd, args[0], opts, configOpts)
	}
}

func runPrompt(cmd *cobra.Command, message string, opts *promptOpts, configOpts *providers.ConfigLoader) error {
	ctx := cmd.Context()
	errW := cmd.ErrOrStderr()

	// The emitter owns every stdout/stderr rendering decision for the four
	// consumer modes (human, agent JSONL, --json NDJSON, --json --no-stream).
	// See stream_emitter.go.
	//
	// Errors before the stream starts are returned bare: stdout is still
	// untouched, so the top-level reporter renders the single, properly
	// classified error (JSON document on stdout for machine consumers, prose
	// on stderr for humans). The command no longer pre-emits its own JSON
	// error here — doing so produced two error documents on stdout for
	// machine consumers.
	em := newStreamEmitter(cmd.OutOrStdout(), errW, opts)

	// The emitter cancels this context on the first stdout write failure
	// (broken pipe): the SSE stream loop stops instead of draining events
	// nobody can read, and em.finish returns the write error.
	ctx, cancelStream := context.WithCancel(ctx)
	defer cancelStream()
	em.cancel = cancelStream

	// Resolve context ID
	contextID := opts.contextID
	if opts.cont {
		lastContextID, err := assistant.GetLastContextID()
		if err != nil {
			return err
		}
		contextID = lastContextID
	}

	clientOpts, err := resolveAssistantClientOptions(ctx, configOpts, opts.timeout, opts.agentID)
	if err != nil {
		return err
	}
	c := assistant.New(clientOpts)

	// Validate context ID if provided
	if contextID != "" {
		notice, err := c.ValidateCLIContext(ctx, contextID)
		if err != nil {
			return err
		}
		em.notice(notice)
	}

	// Human mode gets prose progress logging on stderr; the machine modes
	// keep stderr for typed diagnostics emitted by the emitter itself.
	var logger assistant.Logger
	if em.mode == modeHuman {
		logger = &sseLogger{w: errW}
		c.SetLogger(logger)
	}

	streamOpts := assistant.StreamOptions{
		Timeout:   opts.timeout,
		ContextID: contextID,
		OnEvent:   em.onEvent(),
	}

	result := c.ChatWithApproval(ctx, message, streamOpts, em.approvalHandler(logger))

	return em.finish(result, opts.timeout)
}

// resolveAssistantClientOptions loads the gcx config and returns assistant
// ClientOptions for assistant prompt, including an HTTP client whose Timeout
// matches streamTimeoutSeconds (see --timeout and SSE body reads).
func resolveAssistantClientOptions(ctx context.Context, configOpts *providers.ConfigLoader, streamTimeoutSeconds int, agentID string) (assistant.ClientOptions, error) {
	// Select the effective auth method before full context validation so an
	// explicitly selected Basic or mTLS mode fails as unsupported without a
	// namespace-discovery request. Supported bearer modes are validated below.
	cfg, err := configOpts.LoadConfigTolerant(ctx)
	if err != nil {
		return assistant.ClientOptions{}, err
	}

	curCtx := cfg.Contexts[cfg.CurrentContext]
	if curCtx == nil {
		return assistant.ClientOptions{}, fmt.Errorf("no context %q found in config; run 'gcx config use-context'", cfg.CurrentContext)
	}

	grafana := curCtx.Grafana
	if grafana == nil {
		return assistant.ClientOptions{}, fmt.Errorf("no grafana config in context %q", cfg.CurrentContext)
	}
	authMethod, err := curCtx.EffectiveGrafanaAuthMethod()
	if err != nil {
		return assistant.ClientOptions{}, err
	}

	switch authMethod {
	case "oauth":
		// OAuth path: direct API via ProxyEndpoint. Reuse the canonical REST
		// refresh lifecycle so A2A requests get the same cross-process lock,
		// owning-layer reload, keychain handling, and fail-closed persistence as
		// every other OAuth consumer.
		if err := curCtx.Validate(ctx); err != nil {
			return assistant.ClientOptions{}, err
		}
		restCfg, err := curCtx.ToRESTConfig(ctx)
		if err != nil {
			return assistant.ClientOptions{}, err
		}
		restCfg.WireTokenPersistence(ctx, configOpts.ConfigSource(ctx), cfg.CurrentContext, curCtx.Stack, cfg.Sources)
		httpClient, err := newAssistantStreamingHTTPClientForRESTConfig(&restCfg.Config, streamTimeoutSeconds)
		if err != nil {
			return assistant.ClientOptions{}, fmt.Errorf("create assistant HTTP client: %w", err)
		}
		return assistant.ClientOptions{
			GrafanaURL:     grafana.Server,
			Token:          grafana.OAuthToken,
			APIEndpoint:    grafana.ProxyEndpoint,
			AgentID:        agentID,
			TokenRefresher: restCfg.FreshOAuthToken,
			HTTPClient:     httpClient,
		}, nil

	case "token":
		// SA token path: plugin proxy through Grafana
		if err := curCtx.Validate(ctx); err != nil {
			return assistant.ClientOptions{}, err
		}
		restCfg, err := curCtx.ToRESTConfig(ctx)
		if err != nil {
			return assistant.ClientOptions{}, err
		}
		httpClient, err := newAssistantStreamingHTTPClientForRESTConfig(&restCfg.Config, streamTimeoutSeconds)
		if err != nil {
			return assistant.ClientOptions{}, fmt.Errorf("create assistant HTTP client: %w", err)
		}
		return assistant.ClientOptions{
			GrafanaURL: grafana.Server,
			Token:      grafana.APIToken,
			AgentID:    agentID,
			HTTPClient: httpClient,
		}, nil

	case "basic", "mtls":
		return assistant.ClientOptions{}, fmt.Errorf(
			"gcx assistant requires OAuth or token bearer authentication; selected auth-method %q is not supported",
			authMethod,
		)
	default:
		return assistant.ClientOptions{}, errors.New("no authentication configured; run 'gcx login' or set grafana.token in config")
	}
}

// newAssistantStreamingHTTPClient returns an HTTP client suitable for assistant
// A2A streaming: Timeout spans the full response body read and must align with
// internal/assistant StreamOptions.Timeout (see --timeout on assistant prompt).
func newAssistantStreamingHTTPClient(ctx context.Context, streamTimeoutSeconds int) *http.Client {
	if streamTimeoutSeconds <= 0 {
		streamTimeoutSeconds = 300
	}
	d := time.Duration(streamTimeoutSeconds) * time.Second
	if httputils.PayloadLogging(ctx) {
		return httputils.NewClient(httputils.ClientOpts{
			Timeout: d,
			Middlewares: []httputils.Middleware{
				httputils.LoggingMiddleware,
				httputils.RequestResponseLoggingMiddleware,
			},
		})
	}
	return httputils.NewClient(httputils.ClientOpts{Timeout: d})
}

// newAssistantStreamingHTTPClientForRESTConfig materializes the configured
// REST transport so OAuth refreshes and A2A requests share its TLS settings.
// Materializing WrapTransport also gives RefreshTransport the TLS-aware base
// transport it needs for the refresh endpoint instead of http.DefaultTransport.
func newAssistantStreamingHTTPClientForRESTConfig(cfg *rest.Config, streamTimeoutSeconds int) (*http.Client, error) {
	client, err := rest.HTTPClientFor(cfg)
	if err != nil {
		return nil, err
	}
	if streamTimeoutSeconds <= 0 {
		streamTimeoutSeconds = 300
	}
	client.Timeout = time.Duration(streamTimeoutSeconds) * time.Second
	return client, nil
}

// Output helpers

// jsonLine writes data as one NDJSON line. Both the marshal and the write
// error are returned: a stream line that never reached stdout must surface as
// the command error instead of being swallowed (a swallowed terminal write
// would let a failed gcx.stream_end still exit 0 or claim EmittedError).
func jsonLine(w io.Writer, data any) error {
	output, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("encoding JSON line: %w", err)
	}
	_, err = fmt.Fprintln(w, string(output))
	return err
}

// jsonPretty writes data as one indented JSON document, returning marshal and
// write errors for the same reason as jsonLine.
func jsonPretty(w io.Writer, data any) error {
	output, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding JSON document: %w", err)
	}
	_, err = fmt.Fprintln(w, string(output))
	return err
}

// sseLogger implements assistant.Logger using stderr.
type sseLogger struct {
	w io.Writer
}

func (l *sseLogger) Info(msg string)    { cmdio.Info(l.w, "%s", msg) }
func (l *sseLogger) Debug(msg string)   {} // Silent by default; enable with -v flags
func (l *sseLogger) Warning(msg string) { cmdio.Warning(l.w, "%s", msg) }
