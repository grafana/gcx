package collections

import (
	"context"
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
	col, err := aio11yhttp.DoJSONNotFound[any, Collection](ctx, c.base, http.MethodGet, basePath+"/"+url.PathEscape(id), nil,
		fmt.Errorf("%s: %w", id, ErrNotFound), http.StatusOK)
	if err != nil {
		return nil, err
	}
	return &col, nil
}

// Create creates a new collection. Server-managed fields on the input are
// dropped on the wire by the `omitempty` / `omitzero` JSON tags.
func (c *Client) Create(ctx context.Context, col *Collection) (*Collection, error) {
	created, err := aio11yhttp.DoJSON[Collection, Collection](ctx, c.base, http.MethodPost, basePath, col, http.StatusOK, http.StatusCreated)
	if err != nil {
		return nil, err
	}
	return &created, nil
}

// Update patches a collection's name and/or description. The collections API
// is not an upsert — Update must be called against an existing collection.
func (c *Client) Update(ctx context.Context, id string, req *UpdateRequest) (*Collection, error) {
	col, err := aio11yhttp.DoJSON[UpdateRequest, Collection](ctx, c.base, http.MethodPatch, basePath+"/"+url.PathEscape(id), req, http.StatusOK)
	if err != nil {
		return nil, err
	}
	return &col, nil
}

// Delete removes a collection by ID.
func (c *Client) Delete(ctx context.Context, id string) error {
	return aio11yhttp.DoStatus[any](ctx, c.base, http.MethodDelete, basePath+"/"+url.PathEscape(id), nil, http.StatusOK, http.StatusNoContent)
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
	path := basePath + "/" + url.PathEscape(id) + "/members"
	return aio11yhttp.DoStatus(ctx, c.base, http.MethodPost, path, &AddMembersRequest{SavedIDs: savedIDs}, http.StatusOK, http.StatusCreated, http.StatusNoContent)
}

// RemoveMember removes a single saved conversation from a collection.
func (c *Client) RemoveMember(ctx context.Context, id, savedID string) error {
	path := basePath + "/" + url.PathEscape(id) + "/members/" + url.PathEscape(savedID)
	return aio11yhttp.DoStatus[any](ctx, c.base, http.MethodDelete, path, nil, http.StatusOK, http.StatusNoContent)
}
