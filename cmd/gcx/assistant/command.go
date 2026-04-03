// Package assistant provides the assistant command group for interacting with Grafana Assistant.
package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/grafana/gcx/internal/assistant"
	"github.com/grafana/gcx/internal/auth"
	"github.com/grafana/gcx/internal/config"
	"github.com/spf13/cobra"
)

// Command returns the assistant command group.
func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "assistant",
		Short: "Interact with Grafana Assistant",
		Long:  "Send prompts to Grafana Assistant and receive streaming responses via the A2A protocol.",
	}
	cmd.AddCommand(promptCommand())
	return cmd
}

// promptOpts holds options for the prompt subcommand.
type promptOpts struct {
	timeout   int
	contextID string
	cont      bool // --continue
	jsonOut   bool
	noStream  bool
	agent     string
}

func (o *promptOpts) setup(cmd *cobra.Command) {
	cmd.Flags().IntVar(&o.timeout, "timeout", 300, "Timeout in seconds when waiting for a response")
	cmd.Flags().StringVar(&o.contextID, "context-id", "", "Context ID for conversation threading")
	cmd.Flags().BoolVar(&o.cont, "continue", false, "Continue the previous chat session")
	cmd.Flags().BoolVar(&o.jsonOut, "json", false, "Output as JSON (streams NDJSON events by default)")
	cmd.Flags().BoolVar(&o.noStream, "no-stream", false, "With --json, emit a single JSON object instead of streaming events")
	cmd.Flags().StringVar(&o.agent, "agent", assistant.DefaultAgentID, "Agent ID to target")
}

func (o *promptOpts) validate() error {
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

func promptCommand() *cobra.Command {
	opts := &promptOpts{}

	cmd := &cobra.Command{
		Use:   "prompt <message>",
		Short: "Send a single message to Grafana Assistant",
		Long: `Send a single message to Grafana Assistant and receive the response.

This is useful for scripting and automation. The response streams via
the A2A (Agent-to-Agent) protocol over Server-Sent Events.`,
		Args:    cobra.ExactArgs(1),
		Example: "  gcx assistant prompt \"What alerts are firing?\"\n  gcx assistant prompt \"Show CPU usage\" --json\n  gcx assistant prompt \"Follow up\" --continue",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.validate(); err != nil {
				return err
			}
			return runPrompt(cmd, args[0], opts)
		},
	}

	opts.setup(cmd)
	return cmd
}

func runPrompt(cmd *cobra.Command, message string, opts *promptOpts) error {
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

	// Load config and build client
	clientOpts, err := resolveClientOptions(ctx)
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
			printSuccess(errW, "Completed!")
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
			printWarning(errW, fmt.Sprintf("Request timed out after %ds. Task may still be processing.", opts.timeout))
			if result.TaskID != "" {
				printInfo(errW, "Task ID: "+result.TaskID)
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
			printError(errW, "Request failed: "+result.ErrorMessage)
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
			printWarning(errW, "Request was canceled")
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
		printWarning(errW, "Request ended unexpectedly. The stream closed without a completion signal.")
		if result.TaskID != "" {
			printInfo(errW, "Task ID: "+result.TaskID)
		}
	}
	return err
}

// resolveClientOptions loads the gcx config and builds assistant.ClientOptions.
func resolveClientOptions(ctx context.Context) (assistant.ClientOptions, error) {
	cfg, err := config.Load(ctx, config.StandardLocation())
	if err != nil {
		return assistant.ClientOptions{}, fmt.Errorf("failed to load config: %w", err)
	}

	ctxName := config.ContextNameFromCtx(ctx)
	if ctxName == "" {
		ctxName = cfg.CurrentContext
	}

	curCtx := cfg.Contexts[ctxName]
	if curCtx == nil {
		return assistant.ClientOptions{}, fmt.Errorf("no context %q found in config; run 'gcx config set-context'", ctxName)
	}

	grafana := curCtx.Grafana
	if grafana == nil {
		return assistant.ClientOptions{}, fmt.Errorf("no grafana config in context %q", ctxName)
	}

	switch {
	case grafana.ProxyEndpoint != "" && grafana.OAuthToken != "":
		// OAuth path: direct API via ProxyEndpoint
		refresher := buildTokenRefresher(ctx, &cfg, ctxName, grafana)
		return assistant.ClientOptions{
			GrafanaURL:     grafana.Server,
			Token:          grafana.OAuthToken,
			APIEndpoint:    grafana.ProxyEndpoint,
			TokenRefresher: refresher,
		}, nil

	case grafana.APIToken != "":
		// SA token path: plugin proxy through Grafana
		return assistant.ClientOptions{
			GrafanaURL: grafana.Server,
			Token:      grafana.APIToken,
		}, nil

	default:
		return assistant.ClientOptions{}, errors.New("no authentication configured; run 'gcx auth login' or set grafana.token in config")
	}
}

const refreshThreshold = 5 * time.Minute

// buildTokenRefresher creates a TokenRefresher that uses gcx's auth refresh mechanism.
func buildTokenRefresher(ctx context.Context, cfg *config.Config, ctxName string, grafana *config.GrafanaConfig) assistant.TokenRefresher {
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
		persistRefreshedTokens(ctx, cfg, ctxName, rr.Token, rr.RefreshToken, rr.ExpiresAt, rr.RefreshExpiresAt)

		return token, nil
	}
}

func persistRefreshedTokens(ctx context.Context, cfg *config.Config, ctxName, token, refreshToken, expiresAt, refreshExpiresAt string) {
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
	_ = config.Write(ctx, config.StandardLocation(), *cfg)
}

func parseRFC3339OrZero(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

// Output helpers

func printInfo(w io.Writer, msg string) {
	fmt.Fprintln(w, msg)
}

func printSuccess(w io.Writer, msg string) {
	fmt.Fprintf(w, "OK %s\n", msg)
}

func printWarning(w io.Writer, msg string) {
	fmt.Fprintf(w, "WARNING: %s\n", msg)
}

func printError(w io.Writer, msg string) {
	fmt.Fprintf(w, "ERROR: %s\n", msg)
}

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

func (l *sseLogger) Info(msg string)    { printInfo(l.w, msg) }
func (l *sseLogger) Debug(msg string)   {} // Silent by default; enable with -v flags
func (l *sseLogger) Warning(msg string) { printWarning(l.w, msg) }
