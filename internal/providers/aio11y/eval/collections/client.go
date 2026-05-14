package collections

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/grafana/gcx/internal/providers/aio11y/aio11yhttp"
	"github.com/grafana/gcx/internal/providers/aio11y/eval/savedconversations"
)

const basePath = "/eval/collections"

// ErrNotFound is returned by Get when the server responds with 404.
var ErrNotFound = errors.New("collection not found")

type Client struct {
	base *aio11yhttp.Client
}

// NewClient creates a new collection client.
func NewClient(base *aio11yhttp.Client) *Client {
	return &Client{base: base}
}

// List returns collections, paginated. Pass 0 for no limit.
func (c *Client) List(ctx context.Context, limit int) ([]Collection, error) {
	return aio11yhttp.ListAll[Collection](ctx, c.base, basePath, nil, limit)
}

// Get returns a single collection by ID. Returns ErrNotFound on HTTP 404 so
// callers (and the resource adapter) can distinguish missing resources from
// other API errors.
func (c *Client) Get(ctx context.Context, id string) (*Collection, error) {
	resp, err := c.base.DoRequest(ctx, http.MethodGet, basePath+"/"+url.PathEscape(id), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get collection %s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%s: %w", id, ErrNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, aio11yhttp.HandleErrorResponse(resp)
	}

	var col Collection
	if err := json.NewDecoder(resp.Body).Decode(&col); err != nil {
		return nil, fmt.Errorf("failed to decode collection response: %w", err)
	}
	return &col, nil
}

// Create creates a new collection. Server-managed fields on the input are
// dropped on the wire by the `omitempty` / `omitzero` JSON tags.
func (c *Client) Create(ctx context.Context, col *Collection) (*Collection, error) {
	body, err := json.Marshal(col)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal create request: %w", err)
	}

	resp, err := c.base.DoRequest(ctx, http.MethodPost, basePath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create collection: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, aio11yhttp.HandleErrorResponse(resp)
	}

	var created Collection
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return nil, fmt.Errorf("failed to decode collection response: %w", err)
	}
	return &created, nil
}

// Update patches a collection's name and/or description. The collections API
// is not an upsert — Update must be called against an existing collection.
func (c *Client) Update(ctx context.Context, id string, req *UpdateRequest) (*Collection, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal update request: %w", err)
	}

	resp, err := c.base.DoRequest(ctx, http.MethodPatch, basePath+"/"+url.PathEscape(id), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to update collection %s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, aio11yhttp.HandleErrorResponse(resp)
	}

	var col Collection
	if err := json.NewDecoder(resp.Body).Decode(&col); err != nil {
		return nil, fmt.Errorf("failed to decode collection response: %w", err)
	}
	return &col, nil
}

// Delete removes a collection by ID.
func (c *Client) Delete(ctx context.Context, id string) error {
	resp, err := c.base.DoRequest(ctx, http.MethodDelete, basePath+"/"+url.PathEscape(id), nil)
	if err != nil {
		return fmt.Errorf("failed to delete collection %s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return aio11yhttp.HandleErrorResponse(resp)
	}
	return nil
}

// ListMembers returns the saved conversations belonging to a collection.
// The CLI surface calls these "conversations", but the HTTP path remains
// `/members` upstream.
func (c *Client) ListMembers(ctx context.Context, id string, limit int) ([]savedconversations.SavedConversation, error) {
	path := basePath + "/" + url.PathEscape(id) + "/members"
	return aio11yhttp.ListAll[savedconversations.SavedConversation](ctx, c.base, path, nil, limit)
}

// AddMembers adds one or more saved conversations to a collection.
func (c *Client) AddMembers(ctx context.Context, id string, savedIDs []string) error {
	body, err := json.Marshal(&AddMembersRequest{SavedIDs: savedIDs})
	if err != nil {
		return fmt.Errorf("failed to marshal add-members request: %w", err)
	}

	path := basePath + "/" + url.PathEscape(id) + "/members"
	resp, err := c.base.DoRequest(ctx, http.MethodPost, path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to add members to collection %s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusNoContent {
		return aio11yhttp.HandleErrorResponse(resp)
	}
	return nil
}

// RemoveMember removes a single saved conversation from a collection.
func (c *Client) RemoveMember(ctx context.Context, id, savedID string) error {
	path := basePath + "/" + url.PathEscape(id) + "/members/" + url.PathEscape(savedID)
	resp, err := c.base.DoRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return fmt.Errorf("failed to remove member %s from collection %s: %w", savedID, id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return aio11yhttp.HandleErrorResponse(resp)
	}
	return nil
}
