package kg

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
)

// maxListPages bounds how many backend pages 'entities list' follows for one
// entity type when --limit is passed (including --limit 0).
const maxListPages = 50

// maxDefaultListPages bounds how many backend pages 'entities list' follows
// for one entity type when --limit is not passed. A backend page holds more
// rows than the default limit, so further pages are read only when a
// client-side filter (--insight, or a scope the backend did not apply) drops
// rows.
const maxDefaultListPages = 10

// typeSearch is an entity search without its entity type: the request
// fields of searchTypePage plus the client-side filter applied to each page.
type typeSearch struct {
	assertionsOnly    bool
	includePropagated bool
	sc                *ScopeCriteria
	startMs, endMs    int64
	propertyFilters   []PropertyMatcher
	// keep filters a page client-side; nil keeps every row.
	keep func([]SearchResult) []SearchResult
	// dims, when set, is the schema the scope filter in keep reads; the type
	// check hands it the schema it fetched.
	dims *scopeDims
}

func (s typeSearch) page(ctx context.Context, client *Client, entityType string, pageNum int) (SearchPage, error) {
	return searchTypePage(ctx, client, entityType, s.assertionsOnly, s.includePropagated, s.sc, s.startMs, s.endMs, pageNum, s.propertyFilters)
}

func (s typeSearch) filter(rows []SearchResult) []SearchResult {
	if s.keep == nil {
		return rows
	}
	return s.keep(rows)
}

// searchTypePage fetches one backend page of entities of a single type.
func searchTypePage(ctx context.Context, client *Client, entityType string, assertionsOnly, includePropagated bool, sc *ScopeCriteria, startMs, endMs int64, pageNum int, propertyFilters []PropertyMatcher) (SearchPage, error) {
	matchers := append([]PropertyMatcher{{Name: "name", Op: "IS NOT NULL", Type: "String"}}, propertyFilters...)
	req := SearchRequest{
		TimeCriteria: &TimeCriteria{Start: startMs, End: endMs},
		FilterCriteria: []EntityMatcher{{
			EntityType:                 entityType,
			HavingAssertion:            assertionsOnly,
			HavingPropagatedAssertions: assertionsOnly && includePropagated,
			PropertyMatchers:           matchers,
		}},
		ScopeCriteria: sc,
		PageNum:       pageNum,
	}
	return client.Search(ctx, req)
}

// hasMorePages reports whether the backend signalled a further page after p.
func hasMorePages(p SearchPage) bool {
	return p.MaxLimitHit || (!p.LastPage && len(p.Entities) > 0)
}

// searchByTypesAllPages is searchByTypes for 'entities list' without --page:
// for each type it follows the backend's pages until the backend reports the
// last page, limit matching entities are collected (limit <= 0 means no
// limit) or maxPages pages are read. A row that an earlier page returned is
// skipped, a hint says how many repeats the pages read for the type returned,
// and only rows that pass s.keep count against limit. A failure on
// the first page of a type is handled as searchByTypes does; a failure on a
// later page aborts the listing, since the rows read so far would be an
// incomplete answer.
func searchByTypesAllPages(ctx context.Context, cmd *cobra.Command, client *Client, entityTypes []string, s typeSearch, limit int64, maxPages int) ([]SearchResult, error) {
	if s.startMs == 0 && s.endMs == 0 {
		now := time.Now().UnixMilli()
		s.startMs = now - 3600000
		s.endMs = now
	}
	all := []SearchResult{}
	for _, et := range entityTypes {
		// s.keep runs over every row read for the type so far, not page by
		// page: without a schema, the scope filter judges a type's dimensions
		// from all its rows.
		var raw, kept []SearchResult
		// seen holds the rows of earlier pages: the backend can return a row
		// again on a later page, and a repeat is skipped and counted.
		seen := map[string]bool{}
		repeats := 0
		for pageNum := 0; ; pageNum++ {
			if pageNum == maxPages {
				msg := fmt.Sprintf("stopped after %d backend pages for type %q — the list is incomplete; narrow with --property/--namespace, or count with 'gcx kg graph query'", maxPages, et)
				if maxPages < maxListPages {
					msg += fmt.Sprintf("; pass --limit N or --limit 0 to read up to %d pages", maxListPages)
				}
				cmdio.EmitHint(cmd.ErrOrStderr(), msg, "")
				break
			}
			page, err := s.page(ctx, client, et, pageNum)
			if err != nil {
				if pageNum > 0 {
					return nil, listPageError(et, pageNum, len(raw), err)
				}
				var apiErr *APIError
				if errors.As(err, &apiErr) && apiErr.IsServerError() {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: skipping entity type %q — server error: %v\n", et, apiErr)
					break
				}
				return nil, fmt.Errorf("search entity type %s: %w", et, err)
			}
			pageKeys := make([]string, 0, len(page.Entities))
			for _, r := range page.Entities {
				k := rowKey(r)
				if seen[k] {
					repeats++
				} else {
					raw = append(raw, r)
				}
				pageKeys = append(pageKeys, k)
			}
			for _, k := range pageKeys {
				seen[k] = true
			}
			kept = s.filter(raw)
			if !hasMorePages(page) || (limit > 0 && int64(len(all)+len(kept)) >= limit) {
				break
			}
		}
		if repeats > 0 {
			cmdio.EmitHint(cmd.ErrOrStderr(), repeatedRowsHint(et, repeats), "")
		}
		all = append(all, kept...)
	}
	return all, nil
}

// repeatedRowsHint is the hint list prints when the backend pages it read for
// entityType returned n rows that an earlier page had returned. n counts every
// repeat on those pages, including any past the --limit cut: a repeat anywhere
// means that the backend may have left out a row, before the cut or after it.
func repeatedRowsHint(entityType string, n int) string {
	rows := "rows"
	if n == 1 {
		rows = "row"
	}
	return fmt.Sprintf("the backend pages read for type %q repeated %d %s from earlier pages, and each row is listed once — results may be incomplete, since pages that repeat rows can leave out others; for an exact count use 'gcx kg graph query' with count()", entityType, n, rows)
}

// listPageError reports a failed backend page after the first: the listing
// stops with an error instead of printing a partial list as if it were whole.
func listPageError(entityType string, pageNum, read int, err error) error {
	return gcxerrors.DetailedError{
		Summary: fmt.Sprintf("listing %s entities failed on backend page %d", entityType, pageNum),
		Details: fmt.Sprintf("Pages 0-%d returned %d rows, but the list would be incomplete, so none are printed.", pageNum-1, read),
		Parent:  err,
		Suggestions: []string{
			"Retry the command",
			"Narrow the list with --property or --env/--namespace/--site so it needs fewer pages",
			"For a count, use 'gcx kg graph query' with count()",
		},
	}
}

// rowKey identifies a search row across pages: by its id, or by type, name
// and scope when the row has no id.
func rowKey(r SearchResult) string {
	if r.ID != 0 {
		return strconv.Itoa(r.ID)
	}
	dims := make([]string, 0, len(r.Scope))
	for k, v := range r.Scope {
		dims = append(dims, k+"="+v)
	}
	sort.Strings(dims)
	return resultType(r) + "\x00" + r.Name + "\x00" + strings.Join(dims, ",")
}

// scopeDims tells which scope dimensions an entity type has, from the
// window's schema. The schema is read at most once, and only when a row
// without a requested dimension has to be judged.
type scopeDims struct {
	fetch func() (GraphSchemaResponse, error)

	mu     sync.Mutex
	loaded bool
	types  map[string]map[string]bool // nil when the schema could not be read
}

func newScopeDims(ctx context.Context, client *Client, startMs, endMs int64) *scopeDims {
	return &scopeDims{fetch: func() (GraphSchemaResponse, error) {
		return client.FetchGraphSchema(ctx, startMs, endMs)
	}}
}

// seed hands d a schema that was already fetched for the same window.
func (d *scopeDims) seed(schema KGSchemaResult) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.loaded {
		d.loaded = true
		d.types = schemaScopeDims(schema)
	}
}

// has reports whether entityType has scope dimension dim in the schema. The
// second result is false when the schema could not be read or does not list
// the type.
func (d *scopeDims) has(entityType, dim string) (bool, bool) {
	if d == nil {
		return false, false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.loaded {
		d.loaded = true
		if resp, err := d.fetch(); err == nil {
			d.types = schemaScopeDims(processGraphSchema(resp))
		}
	}
	dims, ok := d.types[entityType]
	if !ok {
		return false, false
	}
	return dims[dim], true
}

// schemaScopeDims maps each schema type to the scope dimensions among its
// properties.
func schemaScopeDims(schema KGSchemaResult) map[string]map[string]bool {
	out := make(map[string]map[string]bool, len(schema.EntityTypes))
	for _, et := range schema.EntityTypes {
		dims := map[string]bool{}
		for _, p := range et.Properties {
			switch p {
			case "env", "namespace", "site":
				dims[p] = true
			}
		}
		out[et.Type] = dims
	}
	return out
}

// filterByScope applies the requested scope (env, namespace, site) to rows
// from a backend that may have ignored the search's scope criteria, the way a
// backend that applies the scope does:
//
//   - a row whose scope carries a requested dimension with another value is
//     dropped;
//   - a row without the dimension is dropped when its entity type has that
//     dimension, and kept otherwise (a type that does not have the
//     dimension is scoped by the dimensions it has). A type has the
//     dimension when some row in results carries it or, failing that,
//     when the window's schema (read through dims) lists it for the type;
//     without dims, or a schema entry for the type, results alone decide;
//   - a requested value of "none" (the KG's value for an unset dimension)
//     matches rows where the dimension is absent, empty or "none".
//
// A backend that applies the scope returns only rows that pass these rules,
// so this leaves its output unchanged.
func filterByScope(results []SearchResult, scope map[string]string, dims *scopeDims) []SearchResult {
	if len(scope) == 0 {
		return results
	}
	carried := map[string]map[string]bool{} // entity type -> dimensions some row carries
	for _, r := range results {
		t := resultType(r)
		for dim := range scope {
			if r.Scope[dim] != "" {
				if carried[t] == nil {
					carried[t] = map[string]bool{}
				}
				carried[t][dim] = true
			}
		}
	}
	typeHas := func(t, dim string) bool {
		if carried[t][dim] {
			return true
		}
		has, _ := dims.has(t, dim)
		return has
	}
	out := make([]SearchResult, 0, len(results))
	for _, r := range results {
		if scopeMatches(r.Scope, scope, func(dim string) bool { return typeHas(resultType(r), dim) }) {
			out = append(out, r)
		}
	}
	return out
}

func scopeMatches(got, want map[string]string, typeHas func(dim string) bool) bool {
	for dim, v := range want {
		g := got[dim]
		switch {
		case v == "none":
			if g != "" && g != "none" {
				return false
			}
		case g == "":
			if typeHas(dim) {
				return false
			}
		case g != v:
			return false
		}
	}
	return true
}

func resultType(r SearchResult) string {
	if r.Type != "" {
		return r.Type
	}
	return r.EntityType
}
