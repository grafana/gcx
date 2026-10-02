package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/grafana/gcx/internal/assistant/assistanthttp"
	"github.com/grafana/gcx/internal/httputils"
)

// MemorySearchResult preserves the backend's ranked results and coverage signals.
// Exhaustive is always false: the API caps results without exposing pagination
// or the number of matching resources before the cap.
type MemorySearchResult struct {
	Type                  string                   `json:"type"`
	SchemaVersion         string                   `json:"schema_version"`
	Exhaustive            bool                     `json:"exhaustive"`
	Collections           []MemorySearchCollection `json:"collections"`
	SearchFocus           []string                 `json:"searchFocus,omitempty"`
	SuppressedCollections []string                 `json:"suppressedCollections,omitempty"`
}

// MemorySearchCollection is one searched collection, including backend failures.
type MemorySearchCollection struct {
	Collection string            `json:"collection"`
	Results    []MemorySearchHit `json:"results"`
	Total      int               `json:"total"` // Returned hits, not total matches.
	Error      string            `json:"error,omitempty"`
}

// MemorySearchHit identifies indexed evidence and preserves collection metadata.
type MemorySearchHit struct {
	Score     float64        `json:"score"`
	Title     string         `json:"title"`
	Summary   string         `json:"summary"`
	SourceID  string         `json:"sourceId"`
	SourceURL string         `json:"sourceUrl,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// SearchMemories searches explicit collections without generative query rewriting
// or relevance filtering, matching the hosted assistant_search MCP tool.
func SearchMemories(ctx context.Context, client *assistanthttp.Client, query string, collections []string, startTime, endTime *time.Time) (*MemorySearchResult, error) {
	body, err := json.Marshal(struct {
		Query         string     `json:"query"`
		Collections   []string   `json:"collections"`
		RetrievalOnly bool       `json:"retrievalOnly"`
		StartTime     *time.Time `json:"startTime,omitempty"`
		EndTime       *time.Time `json:"endTime,omitempty"`
	}{
		Query:         query,
		Collections:   collections,
		RetrievalOnly: true,
		StartTime:     startTime,
		EndTime:       endTime,
	})
	if err != nil {
		return nil, fmt.Errorf("encode assistant search request: %w", err)
	}
	resp, err := client.DoRequestWithHeaders(ctx, http.MethodPost, "/api/v1/assistant-search", bytes.NewReader(body), map[string]string{"X-App-Source": "cli"})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &assistantAPIError{
			operation: "memory search",
			status:    resp.StatusCode,
			message:   transcriptErrorMessage(resp),
		}
	}
	var envelope struct {
		Data *MemorySearchResult `json:"data"`
	}
	responseBody, err := httputils.ReadResponseBody(resp.Body, httputils.DefaultResponseLimit)
	if err != nil {
		return nil, fmt.Errorf("read assistant search response: %w", err)
	}
	if err := json.Unmarshal(responseBody, &envelope); err != nil {
		return nil, fmt.Errorf("decode assistant search response: %w", err)
	}
	if envelope.Data == nil {
		return nil, errors.New("assistant search response is missing data")
	}
	result := envelope.Data
	if result.Collections == nil {
		return nil, errors.New("assistant search response is missing collections")
	}
	result.Type = "gcx.assistant_search"
	result.SchemaVersion = "1"
	result.Exhaustive = false
	for i := range result.Collections {
		if result.Collections[i].Results == nil {
			result.Collections[i].Results = []MemorySearchHit{}
		}
	}
	return result, nil
}
