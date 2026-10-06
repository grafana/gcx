package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

type hybridSearchQuery struct {
	APIVersion string               `json:"apiVersion"`
	Kind       string               `json:"kind"`
	Query      string               `json:"query"`
	Limit      int                  `json:"limit"`
	Filters    []hybridSearchFilter `json:"filters,omitempty"`
}

type hybridSearchFilter struct {
	Field  string   `json:"field"`
	Values []string `json:"values"`
}

type hybridSearchResponse struct {
	Items []struct {
		Resource struct {
			Name string `json:"name"`
		} `json:"resource"`
		Title  string                 `json:"title"`
		Folder string                 `json:"folder"`
		Score  float64                `json:"score"`
		Chunks []DashboardSearchChunk `json:"chunks"`
	} `json:"items"`
}

func (c *searchClient) HybridSearch(ctx context.Context, params SearchParams) (*DashboardSearchResultList, error) {
	query := hybridSearchQuery{
		APIVersion: "search.grafana.app/v0alpha1",
		Kind:       "HybridSearchQuery",
		Query:      params.Query,
		Limit:      params.Limit,
	}
	if len(params.Folders) > 0 {
		query.Filters = []hybridSearchFilter{{Field: "folder", Values: params.Folders}}
	}
	body, err := json.Marshal(query)
	if err != nil {
		return nil, fmt.Errorf("failed to encode hybrid search query: %w", err)
	}
	endpoint := fmt.Sprintf("%s/apis/%s/%s/namespaces/%s/dashboards/search/hybrid",
		c.baseURL, searchAPIGroup, searchAPIVersion, url.PathEscape(c.namespace))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create hybrid search request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	var wire hybridSearchResponse
	if err := c.do(req, &wire); err != nil {
		return nil, err
	}
	result := &DashboardSearchResultList{
		Kind:       searchResultKind,
		APIVersion: searchResultAPIVersion,
		Items:      make([]DashboardHit, 0, len(wire.Items)),
		Limit:      params.Limit,
	}
	for _, hit := range wire.Items {
		result.Items = append(result.Items, DashboardHit{
			Kind:       searchHitKind,
			APIVersion: searchResultAPIVersion,
			Metadata:   DashboardHitMeta{Name: hit.Resource.Name},
			Spec: DashboardHitSpec{
				Title:  hit.Title,
				Folder: hit.Folder,
				Score:  &hit.Score,
				Chunks: hit.Chunks,
			},
		})
	}
	return result, nil
}
