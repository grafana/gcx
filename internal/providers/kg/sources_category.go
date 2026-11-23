package kg

import (
	"context"
	"fmt"
	"maps"
	"strings"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
)

// alertCategoryLabel is the assertion label that alert-rule and change
// insights are matched on by the source-metrics endpoint. 'kg entities
// inspect' shows it as a timeline entry's "category" field, not in its
// labels, so a lookup built from timeLines[].labels misses it.
const alertCategoryLabel = "asserts_alert_category"

// entityTypeLabel is the assertion label that names the entity's type.
const entityTypeLabel = "asserts_entity_type"

// sourceMetricsWithCategoryFill runs the source-metrics lookup. When it
// returns nothing and the labels name an insight but no category, it reads
// the insight's category from the entity's insight timeline and, if there is
// exactly one, retries once with that category added. A lookup that returns
// metrics the first time costs no extra request; a failed read or retry
// leaves the first result, unless the command's context was cancelled or
// timed out meanwhile, in which case the context's error is returned and
// nothing is printed.
func sourceMetricsWithCategoryFill(cmd *cobra.Command, client *Client, req SourceMetricsRequest) ([]SourceMetricsResponse, error) {
	ctx := cmd.Context()
	results, err := client.AssertionSourceMetrics(ctx, req)
	if err != nil || len(results) > 0 {
		return results, err
	}
	fill := insightCategoryForSources(ctx, client, req)
	if fill.category == "" {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return results, nil
	}
	retryReq := withLabel(req, alertCategoryLabel, fill.category)
	if fill.entityType != "" {
		retryReq.Labels[entityTypeLabel] = fill.entityType
	}
	retried, err := client.AssertionSourceMetrics(ctx, retryReq)
	if err != nil || len(retried) == 0 {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		// The first (empty) result stands when the retry fails.
		return results, nil
	}
	hint := fmt.Sprintf("no source matched the labels given; added %s=%s, the category of this insight on the entity's insight timeline in the same window",
		alertCategoryLabel, fill.category)
	if fill.entityType != "" {
		hint += fmt.Sprintf(", and set %s=%s, the graph's name for entity type %s",
			entityTypeLabel, fill.entityType, req.Labels[entityTypeLabel])
	}
	cmdio.EmitHint(cmd.ErrOrStderr(), hint, "")
	return retried, nil
}

// categoryFill is what the retry changes: the insight's category, and the
// graph's name for the entity type when the labels write the type in another
// case ("" = the type is kept as given).
type categoryFill struct {
	category   string
	entityType string
}

// insightCategoryForSources returns the category of the insight named by
// labels["alertname"] on the entity named by labels["asserts_entity_type"]
// and labels["asserts_entity_name"]: the asserts_alert_category label of that
// insight's entries on the entity's insight timeline, read over the request's
// window and the scope labels it carries (env, namespace, site). When the
// timeline read finds no such entity, the type may be written in another
// case than the graph's (the backend matches type names by case): if exactly
// one entity type of that window and scope equals it ignoring case, the read
// is repeated with that name, which is also returned for the retry. It
// returns no category (no retry) when the labels already carry a category,
// lack an alertname or entity, a request fails, or the timeline has no
// category or more than one category for that insight name.
func insightCategoryForSources(ctx context.Context, client *Client, req SourceMetricsRequest) categoryFill {
	labels := req.Labels
	if labels[alertCategoryLabel] != "" {
		return categoryFill{}
	}
	alertname := labels["alertname"]
	entityType := labels[entityTypeLabel]
	name := labels["asserts_entity_name"]
	if alertname == "" || entityType == "" || name == "" {
		return categoryFill{}
	}

	var sc *ScopeCriteria
	for _, dim := range []string{"env", "namespace", "site"} {
		if v := labels[dim]; v != "" {
			if sc == nil {
				sc = &ScopeCriteria{NameAndValues: map[string][]string{}}
			}
			sc.NameAndValues[dim] = []string{v}
		}
	}

	var fill categoryFill
	entries, found, err := entityTimeline(ctx, client, req, entityType, name, sc)
	if err != nil {
		return categoryFill{}
	}
	if !found {
		graphType := graphEntityType(ctx, client, req, entityType, sc)
		if graphType == "" {
			return categoryFill{}
		}
		entries, _, err = entityTimeline(ctx, client, req, graphType, name, sc)
		if err != nil {
			return categoryFill{}
		}
		fill.entityType = graphType
	}
	fill.category = soleCategory(entries, alertname)
	if fill.category == "" {
		return categoryFill{}
	}
	return fill
}

// entityTimeline reads the insight timeline of one entity, matched by type
// and exact name, over the request's window and scope (POST
// /v1/search/assertions) and returns the labels of its entries that carry an
// alertname and name no other entity. found is false when the answer has no
// timeline for that entity. The request sets no havingAssertion filter: that
// filter is applied by the entity search, which can leave out an entity whose
// insight only touches the window's start although the entity's timeline for
// that window lists the insight.
func entityTimeline(ctx context.Context, client *Client, req SourceMetricsRequest, entityType, name string, sc *ScopeCriteria) ([]map[string]string, bool, error) {
	timelines, err := client.SearchAssertions(ctx, SearchRequest{
		TimeCriteria: &TimeCriteria{Start: req.StartTime, End: req.EndTime},
		FilterCriteria: []EntityMatcher{{
			EntityType: entityType,
			PropertyMatchers: []PropertyMatcher{
				{Name: "name", Op: "=", Value: name},
			},
		}},
		ScopeCriteria: sc,
	})
	if err != nil {
		return nil, false, err
	}
	var entries []map[string]string
	found := false
	for _, tl := range timelines {
		if tl.Name != name || !strings.EqualFold(tl.Type, entityType) {
			continue
		}
		found = true
		for _, node := range tl.AllAssertions {
			entries = appendTimelineEntries(entries, node)
		}
	}
	kept := entries[:0]
	for _, l := range entries {
		if v := l["asserts_entity_name"]; v != "" && v != name {
			continue
		}
		if v := l[entityTypeLabel]; v != "" && !strings.EqualFold(v, entityType) {
			continue
		}
		kept = append(kept, l)
	}
	return kept, found, nil
}

// appendTimelineEntries appends the string labels of every node of an
// insight timeline tree (the node and its nestedTimelines) whose labels
// carry an alertname. Nodes that are not objects are skipped.
func appendTimelineEntries(out []map[string]string, node any) []map[string]string {
	m, ok := node.(map[string]any)
	if !ok {
		return out
	}
	if raw, ok := m["labels"].(map[string]any); ok {
		if _, ok := raw["alertname"].(string); ok {
			labels := make(map[string]string, len(raw))
			for k, v := range raw {
				if s, ok := v.(string); ok {
					labels[k] = s
				}
			}
			out = append(out, labels)
		}
	}
	nested, _ := m["nestedTimelines"].([]any)
	for _, n := range nested {
		out = appendTimelineEntries(out, n)
	}
	return out
}

// graphEntityType returns the graph's name for an entity type written in
// another case: the one entity type of the request's window and scope whose
// name equals entityType ignoring case but not exactly. It returns "" when
// there is no such type or more than one, or the lookup fails.
func graphEntityType(ctx context.Context, client *Client, req SourceMetricsRequest, entityType string, sc *ScopeCriteria) string {
	counts, err := client.CountEntityTypes(ctx, req.StartTime, req.EndTime, sc)
	if err != nil {
		return ""
	}
	match := ""
	for t := range counts {
		if t == entityType || !strings.EqualFold(t, entityType) {
			continue
		}
		if match != "" {
			return ""
		}
		match = t
	}
	return match
}

// soleCategory returns the asserts_alert_category of the timeline entries
// named alertname, or "" when those entries carry no category or more than
// one.
func soleCategory(entries []map[string]string, alertname string) string {
	categories := map[string]struct{}{}
	for _, l := range entries {
		if l["alertname"] != alertname {
			continue
		}
		if c := l[alertCategoryLabel]; c != "" {
			categories[c] = struct{}{}
		}
	}
	if len(categories) != 1 {
		return ""
	}
	for c := range categories {
		return c
	}
	return ""
}

// withLabel returns a copy of req with one more label set.
func withLabel(req SourceMetricsRequest, key, value string) SourceMetricsRequest {
	labels := make(map[string]string, len(req.Labels)+1)
	maps.Copy(labels, req.Labels)
	labels[key] = value
	req.Labels = labels
	return req
}
