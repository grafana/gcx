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

// sourceMetricsWithCategoryFill runs the source-metrics lookup. When it
// returns nothing and the labels name an insight but no category, it looks up
// the insight's category on the entity and, if there is exactly one, retries
// once with that category added. A lookup that returns metrics the first time
// costs no extra request; a failed search or retry leaves the first result,
// unless the command's context was cancelled or timed out meanwhile, in which
// case the context's error is returned and nothing is printed.
func sourceMetricsWithCategoryFill(cmd *cobra.Command, client *Client, req SourceMetricsRequest) ([]SourceMetricsResponse, error) {
	ctx := cmd.Context()
	results, err := client.AssertionSourceMetrics(ctx, req)
	if err != nil || len(results) > 0 {
		return results, err
	}
	category, fromConnected := insightCategoryForSources(ctx, client, req)
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
	where := "on this entity"
	if fromConnected {
		where = "on an entity connected to this one"
	}
	cmdio.EmitHint(cmd.ErrOrStderr(),
		fmt.Sprintf("no source matched the labels given; added %s=%s, the insight's category %s", alertCategoryLabel, category, where),
		"")
	return retried, nil
}

// insightCategoryForSources returns the category of the insight named by
// labels["alertname"] on the entity named by labels["asserts_entity_type"] and
// labels["asserts_entity_name"], read from one entity search over the
// request's window and the scope labels it carries (env, namespace, site).
// The entity's own insights are used first; when none of them has that name,
// the insights the search reports on its connected entities are used (an
// insight raised on a connected entity is listed in the search result's
// connectedAssertion), and the second result is true. It returns "" (no
// retry) when the labels already carry a category, lack an alertname or
// entity, the search fails, or the search finds no category or more than one
// category for that insight name.
func insightCategoryForSources(ctx context.Context, client *Client, req SourceMetricsRequest) (string, bool) {
	labels := req.Labels
	if labels[alertCategoryLabel] != "" {
		return "", false
	}
	alertname := labels["alertname"]
	entityType := labels["asserts_entity_type"]
	name := labels["asserts_entity_name"]
	if alertname == "" || entityType == "" || name == "" {
		return "", false
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
	searchReq := SearchRequest{
		TimeCriteria: &TimeCriteria{Start: req.StartTime, End: req.EndTime},
		FilterCriteria: []EntityMatcher{{
			EntityType:                 entityType,
			HavingAssertion:            true,
			HavingPropagatedAssertions: true,
			PropertyMatchers: []PropertyMatcher{
				{Name: "name", Op: "=", Value: name},
			},
		}},
		ScopeCriteria: sc,
	}
	page, err := client.Search(ctx, searchReq)
	if err != nil {
		return "", false
	}

	var own, connected []GraphAssertion
	for _, e := range page.Entities {
		if e.Name != name || (e.Type != entityType && e.EntityType != entityType) {
			continue
		}
		own = append(own, assertionList(e.Assertion)...)
		connected = append(connected, assertionList(e.ConnectedAssertion)...)
	}
	if c, found := soleCategory(own, alertname); found {
		return c, false
	}
	c, _ := soleCategory(connected, alertname)
	return c, c != ""
}

// soleCategory returns the category of the assertions named alertname. found
// is true when at least one assertion has that name; the category is "" when
// those assertions carry no category or more than one.
func soleCategory(assertions []GraphAssertion, alertname string) (string, bool) {
	categories := map[string]struct{}{}
	found := false
	for _, a := range assertions {
		if a.AssertionName != alertname {
			continue
		}
		found = true
		if a.Category != "" {
			categories[a.Category] = struct{}{}
		}
	}
	if len(categories) != 1 {
		return "", found
	}
	for c := range categories {
		return c, true
	}
	return "", found
}

// assertionList decodes the "assertions" array of a search result's inlined
// assertion summary. Entries that are not objects are skipped.
func assertionList(summary map[string]any) []GraphAssertion {
	raw, _ := summary["assertions"].([]any)
	out := make([]GraphAssertion, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		a := GraphAssertion{}
		a.AssertionName, _ = m["assertionName"].(string)
		a.Category, _ = m["category"].(string)
		out = append(out, a)
	}
	return out
}

// withLabel returns a copy of req with one more label set.
func withLabel(req SourceMetricsRequest, key, value string) SourceMetricsRequest {
	labels := make(map[string]string, len(req.Labels)+1)
	maps.Copy(labels, req.Labels)
	labels[key] = value
	req.Labels = labels
	return req
}
