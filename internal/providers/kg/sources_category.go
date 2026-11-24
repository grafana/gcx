package kg

import (
	"context"
	"fmt"
	"maps"

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
	category := insightCategoryForSources(ctx, client, req)
	if category == "" {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return results, nil
	}
	retried, err := client.AssertionSourceMetrics(ctx, withLabel(req, alertCategoryLabel, category))
	if err != nil || len(retried) == 0 {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		// The first (empty) result stands when the retry fails.
		return results, nil
	}
	cmdio.EmitHint(cmd.ErrOrStderr(), fmt.Sprintf(
		"no source matched the labels given; added %s=%s, the category of this insight on the entity's insight timeline in the same window",
		alertCategoryLabel, category), "")
	return retried, nil
}

// insightCategoryForSources returns the category of the insight named by
// labels["alertname"] on the entity named by labels["asserts_entity_type"]
// and labels["asserts_entity_name"]: the asserts_alert_category label of that
// insight's entries on the entity's insight timeline, read over the request's
// window and the scope labels it carries (env, namespace, site). It returns ""
// (no retry) when the labels already carry a category, lack an alertname or
// entity, a request fails, or the timeline has no category or more than one
// category for that insight name.
func insightCategoryForSources(ctx context.Context, client *Client, req SourceMetricsRequest) string {
	labels := req.Labels
	if labels[alertCategoryLabel] != "" {
		return ""
	}
	alertname := labels["alertname"]
	entityType := labels[entityTypeLabel]
	name := labels["asserts_entity_name"]
	if alertname == "" || entityType == "" || name == "" {
		return ""
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

	entries, err := entityTimeline(ctx, client, req, entityType, name, sc)
	if err != nil {
		return ""
	}
	return soleCategory(entries, alertname)
}

// entityTimeline reads the insight timeline of one entity, matched by type
// and exact name, over the request's window and scope (POST
// /v1/search/assertions) and returns the labels of its entries that carry an
// alertname. Every such entry of that entity's timeline is returned, also one whose
// labels name a related entity: an insight raised on a related entity can be
// listed on this entity's timeline, and 'kg entities inspect' lists it among
// this entity's timeLines. The request sets no havingAssertion filter: that filter
// is applied by the entity search, which can leave out an entity whose
// insight only touches the window's start although the entity's timeline for
// that window lists the insight.
func entityTimeline(ctx context.Context, client *Client, req SourceMetricsRequest, entityType, name string, sc *ScopeCriteria) ([]map[string]string, error) {
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
		return nil, err
	}
	var entries []map[string]string
	for _, tl := range timelines {
		if tl.Name != name || tl.Type != entityType {
			continue
		}
		for _, node := range tl.AllAssertions {
			entries = appendTimelineEntries(entries, node)
		}
	}
	return entries, nil
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
