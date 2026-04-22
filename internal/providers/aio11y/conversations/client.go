package conversations

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/grafana/gcx/internal/providers/aio11y/aio11yhttp"
)

// Client is an HTTP client for AI Observability conversation endpoints.
type Client struct {
	base *aio11yhttp.Client
}

// NewClient creates a new conversation client.
func NewClient(base *aio11yhttp.Client) *Client {
	return &Client{base: base}
}

// List returns conversations, limited to the given count. Pass 0 for no limit.
func (c *Client) List(ctx context.Context, limit int) ([]Conversation, error) {
	return aio11yhttp.ListAll[Conversation](ctx, c.base, "/query/conversations", nil, limit)
}

// Get returns a single conversation by ID with all its generations.
func (c *Client) Get(ctx context.Context, id string) (*ConversationDetail, error) {
	resp, err := c.base.DoRequest(ctx, http.MethodGet, "/query/conversations/"+url.PathEscape(id), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get conversation %s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, aio11yhttp.HandleErrorResponse(resp)
	}

	var detail ConversationDetail
	if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil {
		return nil, fmt.Errorf("failed to decode conversation response: %w", err)
	}
	return &detail, nil
}

// Search searches conversations with filters and time range.
func (c *Client) Search(ctx context.Context, req SearchRequest) (*SearchResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal search request: %w", err)
	}

	resp, err := c.base.DoRequest(ctx, http.MethodPost, "/query/conversations/search", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to search conversations: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, aio11yhttp.HandleErrorResponse(resp)
	}

	var searchResp SearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
		return nil, fmt.Errorf("failed to decode search response: %w", err)
	}
	return &searchResp, nil
}
