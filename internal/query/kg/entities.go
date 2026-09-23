package kg

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

const (
	entityLookupPath = PluginResourcePath + "/asserts/api-server/v1/entity"
	searchPath       = PluginResourcePath + "/asserts/api-server/v1/search"
)

// Entity is the minimal Knowledge Graph entity projection this package
// decodes — narrower than the provider's rich GraphEntity (no assertion
// summaries, no connected-entity counts). Consumers that need the richer
// shape use internal/providers/kg directly.
type Entity struct {
	ID   int64  `json:"id,omitempty"`
	Type string `json:"type"`
	// EntityType is an alternate field name the same /v1/entity and /v1/search
	// responses sometimes carry the type under instead of "type" — the
	// provider's own decode of the identical response falls back to it
	// (internal/providers/kg SearchResult/table codec); resolveType keeps
	// that fallback in one place.
	EntityType string            `json:"entityType,omitempty"`
	Name       string            `json:"name"`
	Active     bool              `json:"active,omitempty"`
	Scope      map[string]string `json:"scope,omitempty"`
	Properties map[string]any    `json:"properties,omitempty"`
}

// resolveType applies the Type/EntityType fallback in place, mirroring the
// provider's table-codec fallback so a response that only populated
// entityType doesn't come back from this package with an empty Type.
func (e *Entity) resolveType() {
	if e.Type == "" {
		e.Type = e.EntityType
	}
}

// GetEntity issues the GET /v1/entity lookup shared by every caller that
// resolves one entity by type + name + optional scope, decoding the
// response into T. Returns (nil, nil) on HTTP 204 — "not found" is not an
// error. T is the per-caller decode shape: this package's own LookupEntity
// uses the minimal Entity below; internal/providers/kg.Client.LookupEntity
// uses its own richer GraphEntity over the identical endpoint — one request
// implementation, two decode targets, instead of two copies of the request.
func GetEntity[T any](ctx context.Context, c *Client, entityType, name string, scope map[string]string, domain string, startMs, endMs int64) (*T, error) {
	q := TimeRangeParams(startMs, endMs)
	q.Set("asserts_entity_type", entityType)
	q.Set("asserts_entity_name", name)
	for k, v := range scope {
		q.Set(k, v)
	}
	if domain != "" {
		q.Set("domain", domain)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.host+entityLookupPath+"?"+q.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("kg: create request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("kg: execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		return nil, nil //nolint:nilnil
	}
	if resp.StatusCode >= 400 {
		return nil, ReadError(resp)
	}
	var result T
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("kg: decode entity: %w", err)
	}
	return &result, nil
}

// LookupEntity resolves one entity by type + name + optional scope.
// Returns (nil, nil) on HTTP 204 — "not found" is not an error.
func (c *Client) LookupEntity(ctx context.Context, entityType, name string, scope map[string]string, domain string, startMs, endMs int64) (*Entity, error) {
	result, err := GetEntity[Entity](ctx, c, entityType, name, scope, domain, startMs, endMs)
	if err != nil || result == nil {
		return result, err
	}
	result.resolveType()
	return result, nil
}

// EntityScope narrows an entity search/list to specific scope dimension
// values (env/site/namespace). All fields are optional; empty means
// "don't filter on this dimension".
type EntityScope struct {
	Env       string
	Site      string
	Namespace string
}

func (s EntityScope) nameAndValues() map[string][]string {
	nv := map[string][]string{}
	if s.Env != "" {
		nv["env"] = []string{s.Env}
	}
	if s.Site != "" {
		nv["site"] = []string{s.Site}
	}
	if s.Namespace != "" {
		nv["namespace"] = []string{s.Namespace}
	}
	if len(nv) == 0 {
		return nil
	}
	return nv
}

// Page is one page of search results plus the backend's pagination
// signals, generic over the per-entity decode shape T. LastPage is true
// when no further pages exist; MaxLimitHit is true when the backend's
// per-page result cap was reached (i.e. the page is truncated and a
// subsequent --page would return more).
type Page[T any] struct {
	Entities    []T
	PageNum     int
	LastPage    bool
	MaxLimitHit bool
}

// EntityPage is one page of a ListEntities search.
type EntityPage = Page[Entity]

// entityPropertyMatcher mirrors the wire shape of the KG search API's
// property matcher entries — same JSON tags as the provider's
// PropertyMatcher, defined locally so this package doesn't import the
// provider (which would create the reverse dependency this package exists
// to avoid).
type entityPropertyMatcher struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Op    string `json:"op"`
	Type  string `json:"type"`
	Value string `json:"value"`
}

type entityFilterCriteria struct {
	EntityType       string                  `json:"entityType"`
	PropertyMatchers []entityPropertyMatcher `json:"propertyMatchers,omitempty"`
}

type entityScopeCriteria struct {
	NameAndValues map[string][]string `json:"nameAndValues,omitempty"`
}

type entitySearchRequest struct {
	TimeCriteria struct {
		Start int64 `json:"start,omitempty"`
		End   int64 `json:"end,omitempty"`
	} `json:"timeCriteria"`
	ScopeCriteria  *entityScopeCriteria   `json:"scopeCriteria,omitempty"`
	FilterCriteria []entityFilterCriteria `json:"filterCriteria"`
	PageNum        int                    `json:"pageNum"`
}

// SearchEntities posts req — any JSON-marshalable Knowledge Graph search
// request body — to /v1/search and decodes the standard paginated-entities
// response envelope this backend uses for every filtered entity listing,
// with T as the per-entity decode shape. This is the single implementation
// of that endpoint: this package's own ListEntities below passes its
// minimal Entity and a name-filter-only request;
// internal/providers/kg.Client.Search passes its own richer SearchResult
// and a request with arbitrary matchers/bindings — same wire call, two
// decode shapes, instead of two copies of the request/response handling.
func SearchEntities[T any](ctx context.Context, c *Client, req any) (Page[T], error) {
	var wrapper struct {
		Data struct {
			Entities                 []T  `json:"entities"`
			PageNum                  int  `json:"pageNum"`
			LastPage                 bool `json:"lastPage"`
			SearchResultsMaxLimitHit bool `json:"searchResultsMaxLimitHit"`
		} `json:"data"`
	}
	if err := c.PostJSON(ctx, searchPath, req, &wrapper); err != nil {
		return Page[T]{}, err
	}
	entities := wrapper.Data.Entities
	if entities == nil {
		entities = []T{}
	}
	return Page[T]{
		Entities:    entities,
		PageNum:     wrapper.Data.PageNum,
		LastPage:    wrapper.Data.LastPage,
		MaxLimitHit: wrapper.Data.SearchResultsMaxLimitHit,
	}, nil
}

// ListEntities returns the entities of one type within an optional scope,
// following the same "name IS NOT NULL" default filter the Knowledge Graph
// provider's own entity listing uses. startMs/endMs bound the search window
// (either left at zero defaults to the last hour, same as TimeRangeParams —
// passing only one of the two does not clear the other's default).
func (c *Client) ListEntities(ctx context.Context, entityType string, scope EntityScope, startMs, endMs int64, pageNum int) (EntityPage, error) {
	startMs, endMs = defaultTimeWindow(startMs, endMs)

	req := entitySearchRequest{
		FilterCriteria: []entityFilterCriteria{{
			EntityType: entityType,
			PropertyMatchers: []entityPropertyMatcher{
				{Name: "name", Op: "IS NOT NULL", Type: "String"},
			},
		}},
		PageNum: pageNum,
	}
	req.TimeCriteria.Start = startMs
	req.TimeCriteria.End = endMs
	if nv := scope.nameAndValues(); nv != nil {
		req.ScopeCriteria = &entityScopeCriteria{NameAndValues: nv}
	}

	page, err := SearchEntities[Entity](ctx, c, req)
	if err != nil {
		return EntityPage{}, fmt.Errorf("kg: list entities: %w", err)
	}
	for i := range page.Entities {
		page.Entities[i].resolveType()
	}
	return page, nil
}
