package assistant

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/grafana/gcx/internal/httputils"
)

// DefaultAgentID is the default agent to use if not specified.
const DefaultAgentID = "grafana_assistant_cli"

// Client is a client for interacting with the Grafana Assistant via A2A API.
type Client struct {
	grafanaURL     string
	baseURL        string
	token          string
	agentID        string
	logger         Logger
	tokenRefresher TokenRefresher
	httpClient     *http.Client
	tokenMu        sync.RWMutex
}

// New creates a new Client with the given options.
func New(opts ClientOptions) *Client {
	grafanaURL := strings.TrimSuffix(opts.GrafanaURL, "/")

	baseURL := grafanaURL + "/api/plugins/grafana-assistant-app/resources/api/v1"
	if opts.APIEndpoint != "" {
		baseURL = strings.TrimSuffix(opts.APIEndpoint, "/") + "/api/cli/v1"
	}

	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = httputils.NewDefaultClient(context.Background())
	}

	agentID := opts.AgentID
	if agentID == "" {
		agentID = DefaultAgentID
	}

	return &Client{
		grafanaURL:     grafanaURL,
		baseURL:        baseURL,
		token:          opts.Token,
		agentID:        agentID,
		logger:         NopLogger{},
		tokenRefresher: opts.TokenRefresher,
		httpClient:     httpClient,
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
	token, err := c.freshToken(ctx)
	if err != nil {
		return StreamResult{Failed: true, ErrorMessage: fmt.Sprintf("refresh authentication token: %v", err)}
	}

	return StreamChatWithApproval(ctx, c.baseURL, token, c.agentID, promptWithContext, opts, c.logger, approvalHandler, c.httpClient)
}

// GetChat fetches a single chat by ID.
func (c *Client) GetChat(ctx context.Context, chatID string) (*Chat, error) {
	token, err := c.freshToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("refresh authentication token: %w", err)
	}
	return FetchChat(ctx, c.baseURL, token, chatID, c.httpClient)
}

// GetChatMessages fetches all messages for a chat.
func (c *Client) GetChatMessages(ctx context.Context, chatID string) ([]ChatMessage, error) {
	token, err := c.freshToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("refresh authentication token: %w", err)
	}
	return FetchChatMessages(ctx, c.baseURL, token, chatID, c.httpClient)
}

// ListChats lists the caller's chats, with optional filtering and pagination.
func (c *Client) ListChats(ctx context.Context, opts ListChatsOptions) ([]Chat, error) {
	token, err := c.freshToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("refresh authentication token: %w", err)
	}
	return FetchChats(ctx, c.baseURL, token, opts, c.httpClient)
}

// ValidateCLIContext validates that contextID refers to an existing chat the caller can access.
// It returns an optional notice when continuing a conversation not started from the CLI.
func (c *Client) ValidateCLIContext(ctx context.Context, contextID string) (string, error) {
	chat, err := c.GetChat(ctx, contextID)
	if err != nil {
		return "", fmt.Errorf("failed to validate context: %w", err)
	}
	return ValidateResumableChatSource(contextID, chat)
}

// ValidateResumableChatSource reports whether chat can be resumed and returns an
// optional notice when continuing a conversation not started from the CLI.
func ValidateResumableChatSource(contextID string, chat *Chat) (string, error) {
	if chat == nil {
		return "", fmt.Errorf("context %s not found or not accessible", contextID)
	}
	if chat.Source != "" && chat.Source != "cli" {
		return fmt.Sprintf(
			"Continuing a %s conversation (id: %s). Message history is shared; agent behavior may differ from the CLI assistant.",
			chat.Source,
			contextID,
		), nil
	}
	return "", nil
}

// GetBaseURL returns the computed base URL for API requests.
func (c *Client) GetBaseURL() string {
	return c.baseURL
}

// GetGrafanaURL returns the Grafana instance URL.
func (c *Client) GetGrafanaURL() string {
	return c.grafanaURL
}

func (c *Client) GetAgentID() string {
	return c.agentID
}

// GetToken returns a synchronized snapshot of the current authentication
// token. Network operations refresh explicitly through freshToken so refresh
// failures cannot be hidden behind this accessor's string-only signature.
func (c *Client) GetToken() string {
	return c.currentToken()
}

func (c *Client) freshToken(ctx context.Context) (string, error) {
	if c.tokenRefresher != nil {
		newToken, err := c.tokenRefresher(ctx)
		if err != nil {
			return c.currentToken(), err
		}
		if newToken != "" {
			c.tokenMu.Lock()
			c.token = newToken
			c.tokenMu.Unlock()
			return newToken, nil
		}
	}
	return c.currentToken(), nil
}

func (c *Client) currentToken() string {
	c.tokenMu.RLock()
	defer c.tokenMu.RUnlock()
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
