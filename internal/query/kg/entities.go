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
	ID         int64             `json:"id,omitempty"`
	Type       string            `json:"type"`
	Name       string            `json:"name"`
	Active     bool              `json:"active,omitempty"`
	Scope      map[string]string `json:"scope,omitempty"`
	Properties map[string]any    `json:"properties,omitempty"`
}

// LookupEntity resolves one entity by type + name + optional scope.
// Returns (nil, nil) on HTTP 204 — "not found" is not an error.
func (c *Client) LookupEntity(ctx context.Context, entityType, name string, scope map[string]string, domain string, startMs, endMs int64) (*Entity, error) {
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
	var result Entity
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("kg: decode entity: %w", err)
	}
	return &result, nil
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

// EntityPage is one page of a ListEntities search.
type EntityPage struct {
	Entities    []Entity
	PageNum     int
	LastPage    bool
	MaxLimitHit bool
}

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

// ListEntities returns the entities of one type within an optional scope,
// following the same "name IS NOT NULL" default filter the Knowledge Graph
// provider's own entity listing uses. startMs/endMs bound the search window
// (both zero defaults to the last hour, same as TimeRangeParams).
func (c *Client) ListEntities(ctx context.Context, entityType string, scope EntityScope, startMs, endMs int64, pageNum int) (EntityPage, error) {
	startMs, endMs = defaultTimeWindow(startMs, endMs)

	req := entitySearchRequest{
		FilterCriteria: []entityFilterCriteria{{
			EntityType: entityType,
			PropertyMatchers: []entityPropertyMatcher{
				{Name: "name", Op: "IS NOT NULL"},
			},
		}},
		PageNum: pageNum,
	}
	req.TimeCriteria.Start = startMs
	req.TimeCriteria.End = endMs
	if nv := scope.nameAndValues(); nv != nil {
		req.ScopeCriteria = &entityScopeCriteria{NameAndValues: nv}
	}

	var wrapper struct {
		Data struct {
			Entities                 []Entity `json:"entities"`
			PageNum                  int      `json:"pageNum"`
			LastPage                 bool     `json:"lastPage"`
			SearchResultsMaxLimitHit bool     `json:"searchResultsMaxLimitHit"`
		} `json:"data"`
	}
	if err := c.PostJSON(ctx, searchPath, req, &wrapper); err != nil {
		return EntityPage{}, fmt.Errorf("kg: list entities: %w", err)
	}
	entities := wrapper.Data.Entities
	if entities == nil {
		entities = []Entity{}
	}
	return EntityPage{
		Entities:    entities,
		PageNum:     wrapper.Data.PageNum,
		LastPage:    wrapper.Data.LastPage,
		MaxLimitHit: wrapper.Data.SearchResultsMaxLimitHit,
	}, nil
}
