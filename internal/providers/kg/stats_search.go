package kg

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// statsMaxSearchPages bounds how many search pages `kg stats` reads per
// entity type when it counts insights. A type with more pages is reported in
// insights.incompleteTypes. It is a variable so tests can lower it.
var statsMaxSearchPages = 50 //nolint:gochecknoglobals // Overridden only by tests.

// searchInsightCarriers returns, for every given entity type, the entities
// that carry an active insight in the window. It searches one type at a
// time: the search API is rate limited, and searching several types at once
// can exceed that limit. It follows each type's search pages until the last
// one, up to statsMaxSearchPages, and keeps one row per entity when a later
// page repeats a row. The second result lists, sorted, the types whose rows
// could not all be read: more pages than the bound, a server-side result
// limit, or a page that failed with a server error or was still rate limited
// after the client's retries (both also reported on errOut). Any other error
// fails the whole search.
func searchInsightCarriers(ctx context.Context, errOut io.Writer, client *Client, entityTypes []string, sc *ScopeCriteria, startMs, endMs int64) ([]SearchResult, []string, error) {
	if startMs == 0 && endMs == 0 {
		now := time.Now().UnixMilli()
		startMs = now - 3600000
		endMs = now
	}
	all := []SearchResult{}
	var incomplete []string
	for _, et := range entityTypes {
		rows, done, err := searchTypePages(ctx, errOut, client, et, sc, startMs, endMs)
		if err != nil {
			return nil, nil, err
		}
		all = append(all, rows...)
		if !done {
			incomplete = append(incomplete, et)
		}
	}
	sort.Strings(incomplete)
	return all, incomplete, nil
}

// searchTypePages reads one entity type's insight-carrying entities page by
// page. It reports false when the rows are incomplete: the page bound was
// reached, the server said it hit its result limit, or a page failed with an
// error that skippedSearchError accepts.
func searchTypePages(ctx context.Context, errOut io.Writer, client *Client, entityType string, sc *ScopeCriteria, startMs, endMs int64) ([]SearchResult, bool, error) {
	seen := map[string]bool{}
	var rows []SearchResult
	for pageNum := range statsMaxSearchPages {
		page, err := client.Search(ctx, SearchRequest{
			TimeCriteria: &TimeCriteria{Start: startMs, End: endMs},
			FilterCriteria: []EntityMatcher{{
				EntityType:       entityType,
				HavingAssertion:  true,
				PropertyMatchers: []PropertyMatcher{{Name: "name", Op: "IS NOT NULL", Type: "String"}},
			}},
			ScopeCriteria: sc,
			PageNum:       pageNum,
		})
		if err != nil {
			apiErr, reason := skippedSearchError(err)
			if apiErr == nil {
				return nil, false, fmt.Errorf("search entity type %s: %w", entityType, err)
			}
			if pageNum == 0 {
				fmt.Fprintf(errOut, "warning: skipping entity type %q — %s: %v\n", entityType, reason, apiErr)
			} else {
				fmt.Fprintf(errOut, "warning: entity type %q: page %d failed — %s: %v; its insight counts are incomplete\n", entityType, pageNum, reason, apiErr)
			}
			return rows, false, nil
		}
		for _, r := range page.Entities {
			k := statsRowKey(r, entityType)
			if seen[k] {
				continue
			}
			seen[k] = true
			rows = append(rows, r)
		}
		if page.MaxLimitHit {
			return rows, false, nil
		}
		if page.LastPage || len(page.Entities) == 0 {
			return rows, true, nil
		}
	}
	return rows, false, nil
}

// skippedSearchError returns the API error and a short reason when a failed
// search page should leave only its type's insight counts incomplete rather
// than fail `kg stats`: a server error, or a rate limit (HTTP 429) that
// persisted through the client's retries. It returns nil for any other error.
func skippedSearchError(err error) (*APIError, string) {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return nil, ""
	}
	switch {
	case apiErr.IsServerError():
		return apiErr, "server error"
	case apiErr.StatusCode == http.StatusTooManyRequests:
		return apiErr, "rate limited"
	default:
		return nil, ""
	}
}

// statsRowKey identifies one entity across search pages: its id when the
// search returns one, otherwise its type, name and scope.
func statsRowKey(r SearchResult, entityType string) string {
	if r.ID != 0 {
		return strconv.Itoa(r.ID)
	}
	t := r.Type
	if t == "" {
		t = r.EntityType
	}
	if t == "" {
		t = entityType
	}
	dims := make([]string, 0, len(r.Scope))
	for k, v := range r.Scope {
		dims = append(dims, k+"="+v)
	}
	sort.Strings(dims)
	return t + "\x00" + r.Name + "\x00" + strings.Join(dims, ",")
}
