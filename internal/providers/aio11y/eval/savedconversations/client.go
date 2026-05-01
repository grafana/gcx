package savedconversations

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/grafana/gcx/internal/providers/aio11y/aio11yhttp"
)

const basePath = "/eval/saved-conversations"

type Client struct {
	base *aio11yhttp.Client
}

func NewClient(base *aio11yhttp.Client) *Client {
	return &Client{base: base}
}

// List returns saved conversations, optionally filtered by source
// (`telemetry` or `manual`). Pass empty source for no filter, and 0 for
// no limit.
func (c *Client) List(ctx context.Context, source string, limit int) ([]SavedConversation, error) {
	var query url.Values
	if source != "" {
		query = url.Values{}
		query.Set("source", source)
	}
	return aio11yhttp.ListAll[SavedConversation](ctx, c.base, basePath, query, limit)
}

// Get returns a single saved conversation by saved ID.
func (c *Client) Get(ctx context.Context, savedID string) (*SavedConversation, error) {
	resp, err := c.base.DoRequest(ctx, http.MethodGet, basePath+"/"+url.PathEscape(savedID), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get saved conversation %s: %w", savedID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, aio11yhttp.HandleErrorResponse(resp)
	}

	var sc SavedConversation
	if err := json.NewDecoder(resp.Body).Decode(&sc); err != nil {
		return nil, fmt.Errorf("failed to decode saved conversation response: %w", err)
	}
	return &sc, nil
}

// Save bookmarks an existing live conversation.
func (c *Client) Save(ctx context.Context, req *SaveRequest) (*SavedConversation, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal save request: %w", err)
	}

	resp, err := c.base.DoRequest(ctx, http.MethodPost, basePath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to save conversation: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, aio11yhttp.HandleErrorResponse(resp)
	}

	var sc SavedConversation
	if err := json.NewDecoder(resp.Body).Decode(&sc); err != nil {
		return nil, fmt.Errorf("failed to decode save response: %w", err)
	}
	return &sc, nil
}

// Delete removes a saved conversation by saved ID.
func (c *Client) Delete(ctx context.Context, savedID string) error {
	resp, err := c.base.DoRequest(ctx, http.MethodDelete, basePath+"/"+url.PathEscape(savedID), nil)
	if err != nil {
		return fmt.Errorf("failed to delete saved conversation %s: %w", savedID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return aio11yhttp.HandleErrorResponse(resp)
	}
	return nil
}

// ListCollections returns all collections that contain the given saved
// conversation. The endpoint is not cursor-paginated — it returns the full
// set in one response.
func (c *Client) ListCollections(ctx context.Context, savedID string) ([]CollectionRef, error) {
	path := basePath + "/" + url.PathEscape(savedID) + "/collections"
	resp, err := c.base.DoRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list collections for saved conversation %s: %w", savedID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, aio11yhttp.HandleErrorResponse(resp)
	}

	var page struct {
		Items []CollectionRef `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		return nil, fmt.Errorf("failed to decode collections response: %w", err)
	}
	if page.Items == nil {
		return []CollectionRef{}, nil
	}
	return page.Items, nil
}
