// Package assistant provides the assistant command group for interacting with Grafana Assistant.
package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/assistant"
	"github.com/grafana/gcx/internal/assistant/investigations"
	"github.com/grafana/gcx/internal/auth"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/httputils"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
)

// Command returns the assistant command group.
func Command() *cobra.Command {
	configOpts := &cmdconfig.Options{}

	cmd := &cobra.Command{
		Use:   "assistant",
		Short: "Interact with Grafana Assistant",
		Long:  "Send prompts to Grafana Assistant and receive streaming responses via the A2A protocol.",
	}

	configOpts.BindFlags(cmd.PersistentFlags())
	cmd.AddCommand(promptCommand(configOpts))

	// Create a ConfigLoader for investigations that shares the same --config/--context
	// flags already bound by configOpts. Wire the values via PersistentPreRun so that
	// the loader picks up flag values resolved at execution time.
	invLoader := &providers.ConfigLoader{}
	invCmd := investigations.Commands(invLoader)
	invCmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		if root := cmd.Root(); root.PersistentPreRun != nil {
			root.PersistentPreRun(cmd, args)
		}
		if configOpts.ConfigFile != "" {
			invLoader.SetConfigFile(configOpts.ConfigFile)
		}
		if configOpts.Context != "" {
			invLoader.SetContextName(configOpts.Context)
		}
	}
	cmd.AddCommand(invCmd)
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

func (o *promptOpts) setup(cmd *cobra.Command) {
	cmd.Flags().IntVar(&o.timeout, "timeout", 300, "Timeout in seconds when waiting for a response")
	cmd.Flags().StringVar(&o.contextID, "context-id", "", "Context ID for conversation threading")
	cmd.Flags().BoolVar(&o.cont, "continue", false, "Continue the previous chat session")
	cmd.Flags().BoolVar(&o.jsonOut, "json", false, "Output as JSON (streams NDJSON events by default)")
	cmd.Flags().BoolVar(&o.noStream, "no-stream", false, "With --json, emit a single JSON object instead of streaming events")
	cmd.Flags().StringVar(&o.agentID, "agent-id", assistant.DefaultAgentID, "Agent ID to target")
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

func promptCommand(configOpts *cmdconfig.Options) *cobra.Command {
	opts := &promptOpts{}

	cmd := &cobra.Command{
		Use:   "prompt <message>",
		Short: "Send a single message to Grafana Assistant",
		Long: `Send a single message to Grafana Assistant and receive the response.

This is useful for scripting and automation. The response streams via
the A2A (Agent-to-Agent) protocol over Server-Sent Events.`,
		Args:    cobra.ExactArgs(1),
		Example: "  gcx assistant prompt \"What alerts are firing?\"\n  gcx assistant prompt \"Show CPU usage\" --json\n  gcx assistant prompt \"Follow up\" --continue",
		Annotations: map[string]string{
			agent.AnnotationTokenCost: "large",
			agent.AnnotationLLMHint:   "Prefer deterministic gcx commands (gcx metrics query, gcx slo definitions status, gcx alert instances list) for precise data retrieval. Use assistant prompt for reasoning: root cause analysis, holistic health questions, or when you don't know which metrics/labels exist — the Assistant's Infrastructure Memories know your stack topology. Example: \"Why is checkout-latency spiking?\" --json",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			return runPrompt(cmd, args[0], opts, configOpts)
		},
	}

	opts.setup(cmd)
	return cmd
}

func runPrompt(cmd *cobra.Command, message string, opts *promptOpts, configOpts *cmdconfig.Options) error {
	ctx := cmd.Context()
	jsonStream := opts.jsonOut && !opts.noStream
	w := cmd.OutOrStdout()
	errW := cmd.ErrOrStderr()

	// jsonError emits a JSON error and returns it.
	jsonError := func(err error) error {
		if jsonStream {
			jsonLine(w, assistant.StreamEvent{Type: "error", Error: err.Error()})
		} else {
			jsonPretty(w, promptResult{Status: "error", Error: err.Error()})
		}
		return err
	}

	// Resolve context ID
	contextID := opts.contextID
	if opts.cont {
		lastContextID, err := assistant.GetLastContextID()
		if err != nil {
			if opts.jsonOut {
				return jsonError(err)
			}
			return err
		}
		contextID = lastContextID
	}

	clientOpts, err := resolveAssistantClientOptions(ctx, configOpts, opts.timeout)
	if err != nil {
		if opts.jsonOut {
			return jsonError(err)
		}
		return err
	}
	c := assistant.New(clientOpts)

	// Validate context ID if provided
	if contextID != "" {
		if err := c.ValidateCLIContext(ctx, contextID); err != nil {
			if opts.jsonOut {
				return jsonError(err)
			}
			return err
		}
	}

	// Set up logging (disabled in JSON mode)
	var logger assistant.Logger
	if !opts.jsonOut {
		logger = &sseLogger{w: errW}
		c.SetLogger(logger)
	}

	// Set up approval handler (interactive for non-JSON mode)
	var approvalHandler assistant.ApprovalHandler
	if !opts.jsonOut {
		approvalHandler = &assistant.InteractiveApprovalHandler{Logger: logger}
	}

	streamOpts := assistant.StreamOptions{
		Timeout:   opts.timeout,
		ContextID: contextID,
	}

	// In JSON streaming mode, emit each event as NDJSON
	if jsonStream {
		streamOpts.OnEvent = func(event assistant.StreamEvent) {
			jsonLine(w, event)
		}
	}

	result := c.ChatWithApproval(ctx, message, streamOpts, approvalHandler)

	return handlePromptResult(cmd, result, opts, jsonStream)
}

func handlePromptResult(cmd *cobra.Command, result assistant.StreamResult, opts *promptOpts, jsonStream bool) error {
	w := cmd.OutOrStdout()
	errW := cmd.ErrOrStderr()

	if result.Completed {
		if result.ContextID != "" {
			_ = assistant.SaveLastContextID(result.ContextID)
		}
		switch {
		case opts.jsonOut && !jsonStream:
			jsonPretty(w, promptResult{
				TaskID:    result.TaskID,
				ContextID: result.ContextID,
				Status:    "completed",
				Response:  result.Response,
			})
		case !opts.jsonOut:
			cmdio.Success(errW, "Completed!")
			fmt.Fprintln(w)
			fmt.Fprintln(w, "--- Response ---")
			fmt.Fprintln(w)
			fmt.Fprintln(w, result.Response)
			fmt.Fprintln(w)
			fmt.Fprintln(w, "----------------")
		}
		return nil
	}

	if result.TimedOut {
		err := fmt.Errorf("request timed out after %ds", opts.timeout)
		switch {
		case jsonStream:
			jsonLine(w, assistant.StreamEvent{
				Type:    "error",
				Error:   err.Error(),
				Timeout: opts.timeout,
			})
		case opts.jsonOut:
			jsonPretty(w, promptResult{
				TaskID:    result.TaskID,
				ContextID: result.ContextID,
				Status:    "timeout",
				Timeout:   opts.timeout,
			})
		default:
			cmdio.Warning(errW, "Request timed out after %ds. Task may still be processing.", opts.timeout)
			if result.TaskID != "" {
				cmdio.Info(errW, "Task ID: %s", result.TaskID)
			}
		}
		return err
	}

	if result.Failed {
		err := fmt.Errorf("request failed: %s", result.ErrorMessage)
		switch {
		case jsonStream && !result.ErrorEventEmitted:
			jsonLine(w, assistant.StreamEvent{
				Type:      "error",
				TaskID:    result.TaskID,
				ContextID: result.ContextID,
				Error:     result.ErrorMessage,
			})
		case opts.jsonOut && !jsonStream:
			jsonPretty(w, promptResult{
				TaskID:    result.TaskID,
				ContextID: result.ContextID,
				Status:    "failed",
				Error:     result.ErrorMessage,
			})
		case !opts.jsonOut:
			cmdio.Error(errW, "Request failed: %s", result.ErrorMessage)
		}
		return err
	}

	if result.Canceled {
		err := errors.New("request was canceled")
		switch {
		case opts.jsonOut && !jsonStream:
			jsonPretty(w, promptResult{
				TaskID:    result.TaskID,
				ContextID: result.ContextID,
				Status:    "canceled",
			})
		case !opts.jsonOut:
			cmdio.Warning(errW, "Request was canceled")
		}
		return err
	}

	// Unknown state
	err := errors.New("request ended in unknown state")
	switch {
	case jsonStream:
		jsonLine(w, assistant.StreamEvent{Type: "error", Error: "stream ended unexpectedly"})
	case opts.jsonOut:
		jsonPretty(w, promptResult{
			TaskID:    result.TaskID,
			ContextID: result.ContextID,
			Status:    "unknown",
		})
	default:
		cmdio.Warning(errW, "Request ended unexpectedly. The stream closed without a completion signal.")
		if result.TaskID != "" {
			cmdio.Info(errW, "Task ID: %s", result.TaskID)
		}
	}
	return err
}

// resolveAssistantClientOptions loads the gcx config and returns assistant
// ClientOptions for assistant prompt, including an HTTP client whose Timeout
// matches streamTimeoutSeconds (see --timeout and SSE body reads).
func resolveAssistantClientOptions(ctx context.Context, configOpts *cmdconfig.Options, streamTimeoutSeconds int) (assistant.ClientOptions, error) {
	cfg, err := configOpts.LoadConfig(ctx)
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

	httpClient := newAssistantStreamingHTTPClient(ctx, streamTimeoutSeconds)

	switch {
	case grafana.ProxyEndpoint != "" && grafana.OAuthToken != "":
		// OAuth path: direct API via ProxyEndpoint
		refresher := buildTokenRefresher(ctx, configOpts, &cfg, cfg.CurrentContext, grafana)
		return assistant.ClientOptions{
			GrafanaURL:     grafana.Server,
			Token:          grafana.OAuthToken,
			APIEndpoint:    grafana.ProxyEndpoint,
			TokenRefresher: refresher,
			HTTPClient:     httpClient,
		}, nil

	case grafana.APIToken != "":
		// SA token path: plugin proxy through Grafana
		return assistant.ClientOptions{
			GrafanaURL: grafana.Server,
			Token:      grafana.APIToken,
			HTTPClient: httpClient,
		}, nil

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

const refreshThreshold = 5 * time.Minute

// buildTokenRefresher creates a TokenRefresher that uses gcx's auth refresh mechanism.
func buildTokenRefresher(ctx context.Context, configOpts *cmdconfig.Options, cfg *config.Config, ctxName string, grafana *config.GrafanaConfig) assistant.TokenRefresher {
	var mu sync.Mutex
	token := grafana.OAuthToken
	refreshToken := grafana.OAuthRefreshToken
	expiresAt := parseRFC3339OrZero(grafana.OAuthTokenExpiresAt)
	refreshExpiresAt := parseRFC3339OrZero(grafana.OAuthRefreshExpiresAt)
	proxyEndpoint := grafana.ProxyEndpoint

	return func() (string, error) {
		mu.Lock()
		defer mu.Unlock()

		// Token still valid — return as-is
		if time.Until(expiresAt) > refreshThreshold {
			return token, nil
		}

		// Refresh token itself expired
		if !refreshExpiresAt.IsZero() && time.Now().After(refreshExpiresAt) {
			return "", auth.ErrRefreshTokenExpired
		}

		// Do the refresh
		rr, err := auth.DoRefresh(ctx, proxyEndpoint, refreshToken)
		if err != nil {
			return token, err // return stale token on failure
		}

		// Update captured state
		token = rr.Token
		if rr.RefreshToken != "" {
			refreshToken = rr.RefreshToken
		}
		if t, parseErr := time.Parse(time.RFC3339, rr.ExpiresAt); parseErr == nil {
			expiresAt = t
		}
		if t, parseErr := time.Parse(time.RFC3339, rr.RefreshExpiresAt); parseErr == nil {
			refreshExpiresAt = t
		}

		// Persist to config
		persistRefreshedTokens(ctx, configOpts, cfg, ctxName, rr.Token, rr.RefreshToken, rr.ExpiresAt, rr.RefreshExpiresAt)

		return token, nil
	}
}

func persistRefreshedTokens(ctx context.Context, configOpts *cmdconfig.Options, cfg *config.Config, ctxName, token, refreshToken, expiresAt, refreshExpiresAt string) {
	curCtx := cfg.Contexts[ctxName]
	if curCtx == nil || curCtx.Grafana == nil {
		return
	}
	curCtx.Grafana.OAuthToken = token
	if refreshToken != "" {
		curCtx.Grafana.OAuthRefreshToken = refreshToken
	}
	curCtx.Grafana.OAuthTokenExpiresAt = expiresAt
	curCtx.Grafana.OAuthRefreshExpiresAt = refreshExpiresAt
	_ = config.Write(ctx, configOpts.ConfigSource(), *cfg)
}

func parseRFC3339OrZero(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

// Output helpers

func jsonLine(w io.Writer, data any) {
	output, err := json.Marshal(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Failed to marshal JSON: %v\n", err)
		return
	}
	fmt.Fprintln(w, string(output))
}

func jsonPretty(w io.Writer, data any) {
	output, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Failed to marshal JSON: %v\n", err)
		return
	}
	fmt.Fprintln(w, string(output))
}

// sseLogger implements assistant.Logger using stderr.
type sseLogger struct {
	w io.Writer
}

func (l *sseLogger) Info(msg string)    { cmdio.Info(l.w, "%s", msg) }
func (l *sseLogger) Debug(msg string)   {} // Silent by default; enable with -v flags
func (l *sseLogger) Warning(msg string) { cmdio.Warning(l.w, "%s", msg) }
