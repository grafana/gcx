package savedconversations

import (
	"context"
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
	sc, err := aio11yhttp.DoJSON[any, SavedConversation](ctx, c.base, http.MethodGet, basePath+"/"+url.PathEscape(savedID), nil, http.StatusOK)
	if err != nil {
		return nil, err
	}
	return &sc, nil
}

// Save bookmarks an existing live conversation.
func (c *Client) Save(ctx context.Context, req *SaveRequest) (*SavedConversation, error) {
	sc, err := aio11yhttp.DoJSON[SaveRequest, SavedConversation](ctx, c.base, http.MethodPost, basePath, req, http.StatusOK, http.StatusCreated)
	if err != nil {
		return nil, err
	}
	return &sc, nil
}

// Delete removes a saved conversation by saved ID.
func (c *Client) Delete(ctx context.Context, savedID string) error {
	return aio11yhttp.DoStatus[any](ctx, c.base, http.MethodDelete, basePath+"/"+url.PathEscape(savedID), nil, http.StatusOK, http.StatusNoContent)
}

// collectionsEnvelope is the response envelope for the saved-conversation
// collections endpoint.
type collectionsEnvelope struct {
	Items []CollectionRef `json:"items"`
}

// ListCollections returns all collections that contain the given saved
// conversation. The endpoint is not cursor-paginated — it returns the full
// set in one response.
func (c *Client) ListCollections(ctx context.Context, savedID string) ([]CollectionRef, error) {
	path := basePath + "/" + url.PathEscape(savedID) + "/collections"
	page, err := aio11yhttp.DoJSON[any, collectionsEnvelope](ctx, c.base, http.MethodGet, path, nil, http.StatusOK)
	if err != nil {
		return nil, err
	}
	if page.Items == nil {
		return []CollectionRef{}, nil
	}
	return page.Items, nil
}
