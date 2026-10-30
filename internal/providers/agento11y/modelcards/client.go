package modelcards

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/grafana/gcx/internal/providers/agento11y/agento11yhttp"
)

const (
	listPath   = "/query/model-cards"
	lookupPath = "/query/model-cards/lookup"

	// maxPageSize is the largest page the backend serves; it clamps anything
	// bigger.
	maxPageSize = 200
)

// Client is an HTTP client for the Agent Observability model catalog endpoints.
type Client struct {
	base *agento11yhttp.Client
}

// NewClient creates a model catalog client.
func NewClient(base *agento11yhttp.Client) *Client {
	return &Client{base: base}
}

// Card is one catalog entry. It stays a generic document so catalog fields gcx
// does not model survive into -o json and -o yaml.
type Card map[string]any

// LookupQuery identifies one card, either by canonical model key or by the
// catalog source and that source's own model ID.
type LookupQuery struct {
	ModelKey      string
	Source        string
	SourceModelID string
}

// Lookup returns the backend's lookup envelope ({data, freshness}) untouched.
func (c *Client) Lookup(ctx context.Context, q LookupQuery) (map[string]any, error) {
	query := url.Values{}
	if q.ModelKey != "" {
		query.Set("model_key", q.ModelKey)
	}
	if q.Source != "" {
		query.Set("source", q.Source)
	}
	if q.SourceModelID != "" {
		query.Set("source_model_id", q.SourceModelID)
	}
	return agento11yhttp.DoJSON[any, map[string]any](ctx, c.base, http.MethodGet, lookupPath+"?"+query.Encode(), nil, http.StatusOK)
}

// ListQuery holds the optional catalog filters. Zero values are not sent.
type ListQuery struct {
	Q                string
	Source           string
	Provider         string
	Regex            string
	Sort             string
	Order            string
	FreeOnly         *bool
	MinContextLength *int
}

func (q ListQuery) values() url.Values {
	v := url.Values{}
	for key, value := range map[string]string{
		"q": q.Q, "source": q.Source, "provider": q.Provider,
		"regex": q.Regex, "sort": q.Sort, "order": q.Order,
	} {
		if value != "" {
			v.Set(key, value)
		}
	}
	if q.FreeOnly != nil {
		v.Set("free_only", strconv.FormatBool(*q.FreeOnly))
	}
	if q.MinContextLength != nil {
		v.Set("min_context_length", strconv.Itoa(*q.MinContextLength))
	}
	return v
}

// ListResult is the cards found, whether more exist beyond the limit, and the
// catalog freshness the backend reported with the last page.
type ListResult struct {
	Cards     []Card
	HasMore   bool
	Freshness Freshness
}

// Freshness reports how current the catalog is.
type Freshness struct {
	Stale                  bool   `json:"stale"`
	CatalogLastRefreshedAt string `json:"catalog_last_refreshed_at,omitempty"`
}

type listPage struct {
	Data       []Card    `json:"data"`
	NextCursor string    `json:"next_cursor"`
	Freshness  Freshness `json:"freshness"`
}

// List pages through the catalog until limit cards are collected. A limit of
// zero or less fetches every page.
func (c *Client) List(ctx context.Context, q ListQuery, limit int) (ListResult, error) {
	query := q.values()
	pageSize := maxPageSize
	if limit > 0 && limit < maxPageSize {
		pageSize = limit
	}
	query.Set("limit", strconv.Itoa(pageSize))

	result := ListResult{Cards: []Card{}}
	for {
		page, err := agento11yhttp.DoJSON[any, listPage](ctx, c.base, http.MethodGet, listPath+"?"+query.Encode(), nil, http.StatusOK)
		if err != nil {
			return ListResult{}, err
		}
		result.Cards = append(result.Cards, page.Data...)
		result.Freshness = page.Freshness
		result.HasMore = page.NextCursor != ""

		if limit > 0 && len(result.Cards) >= limit {
			result.HasMore = result.HasMore || len(result.Cards) > limit
			result.Cards = result.Cards[:limit]
			return result, nil
		}
		if page.NextCursor == "" {
			return result, nil
		}
		query.Set("cursor", page.NextCursor)
	}
}

// rawNumber extracts a JSON number from a decoded document.
func rawNumber(v any) (float64, bool) {
	n, ok := v.(float64)
	return n, ok
}
