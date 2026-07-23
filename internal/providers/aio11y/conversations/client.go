package conversations

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/grafana/gcx/internal/providers/aio11y/aio11yhttp"
)

const (
	conversationsPath      = "/query/conversations"
	conversationByIDFmt    = conversationsPath + "/%s"
	conversationSearchPath = conversationsPath + "/search"
	annotationsByIDFmt     = conversationByIDFmt + "/annotations"
)

// Client is an HTTP client for Agent Observability conversation endpoints.
type Client struct {
	base *aio11yhttp.Client
}

// NewClient creates a new conversation client.
func NewClient(base *aio11yhttp.Client) *Client {
	return &Client{base: base}
}

// List returns conversations, limited to the given count. Pass 0 for no limit.
func (c *Client) List(ctx context.Context, limit int) ([]Conversation, error) {
	return aio11yhttp.ListAll[Conversation](ctx, c.base, conversationsPath, nil, limit)
}

// Get returns a single conversation by ID with all its generations.
func (c *Client) Get(ctx context.Context, id string) (*ConversationDetail, error) {
	detail, err := aio11yhttp.DoJSON[any, ConversationDetail](ctx, c.base, http.MethodGet, fmt.Sprintf(conversationByIDFmt, url.PathEscape(id)), nil, http.StatusOK)
	if err != nil {
		return nil, err
	}
	return &detail, nil
}

// Search searches conversations with filters and time range.
func (c *Client) Search(ctx context.Context, req SearchRequest) (*SearchResponse, error) {
	searchResp, err := aio11yhttp.DoJSON[SearchRequest, SearchResponse](ctx, c.base, http.MethodPost, conversationSearchPath, &req, http.StatusOK)
	if err != nil {
		return nil, err
	}
	return &searchResp, nil
}

// ListAnnotations returns annotation events attached to a conversation.
func (c *Client) ListAnnotations(ctx context.Context, conversationID string, limit int, cursor string) (*ConversationAnnotationsResponse, error) {
	query := url.Values{}
	if limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}
	if cursor != "" {
		query.Set("cursor", cursor)
	}

	path := fmt.Sprintf(annotationsByIDFmt, url.PathEscape(conversationID))
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}

	resp, err := aio11yhttp.DoJSON[any, ConversationAnnotationsResponse](ctx, c.base, http.MethodGet, path, nil, http.StatusOK)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// CreateAnnotation creates an annotation event for a conversation.
func (c *Client) CreateAnnotation(ctx context.Context, conversationID string, req CreateAnnotationRequest) (*CreateAnnotationResponse, error) {
	resp, err := aio11yhttp.DoJSON[CreateAnnotationRequest, CreateAnnotationResponse](ctx, c.base, http.MethodPost, fmt.Sprintf(annotationsByIDFmt, url.PathEscape(conversationID)), &req, http.StatusOK)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}
