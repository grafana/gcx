package assistant

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// DefaultAgentID is the default agent to use if not specified.
const DefaultAgentID = "grafana_assistant_cli"

// Client is a client for interacting with the Grafana Assistant via A2A API.
type Client struct {
	grafanaURL     string
	baseURL        string
	token          string
	logger         Logger
	tokenRefresher TokenRefresher
}

// New creates a new Client with the given options.
func New(opts ClientOptions) *Client {
	grafanaURL := strings.TrimSuffix(opts.GrafanaURL, "/")

	baseURL := grafanaURL + "/api/plugins/grafana-assistant-app/resources/api/v1"
	if opts.APIEndpoint != "" {
		baseURL = strings.TrimSuffix(opts.APIEndpoint, "/") + "/api/cli/v1"
	}

	return &Client{
		grafanaURL:     grafanaURL,
		baseURL:        baseURL,
		token:          opts.Token,
		logger:         NopLogger{},
		tokenRefresher: opts.TokenRefresher,
	}
}

// SetLogger sets a custom logger for events.
func (c *Client) SetLogger(logger Logger) {
	c.logger = logger
}

// Chat sends a message and streams the response.
func (c *Client) Chat(ctx context.Context, prompt string, opts StreamOptions) StreamResult {
	return c.ChatWithApproval(ctx, prompt, opts, nil)
}

// ChatWithApproval sends a message and streams the response with approval handling.
func (c *Client) ChatWithApproval(ctx context.Context, prompt string, opts StreamOptions, approvalHandler ApprovalHandler) StreamResult {
	c.logger.Info(fmt.Sprintf("Sending message (timeout: %ds)...", opts.Timeout))

	promptWithContext := prompt + "\n" + FormatTimeContext()

	return StreamChatWithApproval(ctx, c.baseURL, c.freshToken(), DefaultAgentID, promptWithContext, opts, c.logger, approvalHandler)
}

// GetChat fetches a single chat by ID.
func (c *Client) GetChat(ctx context.Context, chatID string) (*Chat, error) {
	return FetchChat(ctx, c.baseURL, c.freshToken(), chatID)
}

// ValidateCLIContext validates that a context ID belongs to a CLI-created chat.
func (c *Client) ValidateCLIContext(ctx context.Context, contextID string) error {
	chat, err := c.GetChat(ctx, contextID)
	if err != nil {
		return fmt.Errorf("failed to validate context: %w", err)
	}
	if chat.Source != "cli" {
		return fmt.Errorf("context %s was not created by CLI (source: %s). Use a CLI-created context or start a new conversation", contextID, chat.Source)
	}
	return nil
}

// GetBaseURL returns the computed base URL for API requests.
func (c *Client) GetBaseURL() string {
	return c.baseURL
}

// GetGrafanaURL returns the Grafana instance URL.
func (c *Client) GetGrafanaURL() string {
	return c.grafanaURL
}

// GetToken returns the current authentication token.
func (c *Client) GetToken() string {
	return c.freshToken()
}

func (c *Client) freshToken() string {
	if c.tokenRefresher != nil {
		if newToken, err := c.tokenRefresher(); err == nil && newToken != "" {
			c.token = newToken
		}
	}
	return c.token
}

// FormatTimeContext generates time context XML tags for the assistant.
func FormatTimeContext() string {
	now := time.Now()
	return fmt.Sprintf(
		"<context><time_iso_utc>%s</time_iso_utc><time_iso_local>%s</time_iso_local><timezone>%s</timezone></context>",
		now.UTC().Format(time.RFC3339),
		now.Format(time.RFC3339),
		now.Location().String(),
	)
}
