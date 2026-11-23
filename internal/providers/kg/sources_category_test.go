package kg_test

import (
	"bytes"
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/providers/kg"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sourcesFake serves source-metrics and entity search for the sources
// category fill-in tests and records the label set of every source-metrics
// request and the body of every search request.
type sourcesFake struct {
	mu sync.Mutex
	// metrics returns the source-metrics answer for one request's labels.
	metrics func(labels map[string]string) string
	// search is the /v1/search answer; searchStatus overrides 200 when set.
	search       string
	searchStatus int
	// onSearch and onSource, when set, run before the search or the nth
	// (0-based) source-metrics request is answered.
	onSearch func()
	onSource func(n int)

	sourceCalls  []map[string]string
	searchCalls  int
	searchBodies []map[string]any
}

func (f *sourcesFake) handler(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch {
		case strings.HasSuffix(r.URL.Path, "/v1/assertion/source-metrics"):
			var body struct {
				Labels map[string]string `json:"labels"`
			}
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			if f.onSource != nil {
				f.onSource(len(f.sourceCalls))
			}
			f.sourceCalls = append(f.sourceCalls, body.Labels)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(f.metrics(body.Labels)))
		case strings.HasSuffix(r.URL.Path, "/v1/search"):
			f.searchCalls++
			var body map[string]any
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			f.searchBodies = append(f.searchBodies, body)
			if f.onSearch != nil {
				f.onSearch()
			}
			if f.searchStatus != 0 {
				w.WriteHeader(f.searchStatus)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(f.search))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

const ruleMetrics = `[{"metricName":"metric_x","labels":[{"label":"namespace","op":"=","value":"ns1"}]}]`

// metricsWhenFailure answers like the backend for an alert-rule insight of
// category failure: the rule's metrics only when that category label is sent.
func metricsWhenFailure() func(map[string]string) string {
	return func(labels map[string]string) string {
		if labels["asserts_alert_category"] == "failure" {
			return ruleMetrics
		}
		return `[]`
	}
}

func searchPage(entities ...string) string {
	return `{"data":{"entities":[` + strings.Join(entities, ",") + `],"pageNum":0,"lastPage":true}}`
}

const ownFailure = `{"name":"name-a","type":"TypeA","scope":{"env":"prod-x","namespace":"ns1"},
 "assertion":{"assertions":[{"assertionName":"AlertX","category":"failure","entityType":"TypeA"},
                            {"assertionName":"OtherChange","category":"amend","entityType":"TypeA"}]}}`

const connectedFailure = `{"name":"name-a","type":"TypeA","scope":{"env":"prod-x","namespace":"ns1"},
 "assertion":{"assertions":[{"assertionName":"OtherChange","category":"amend","entityType":"TypeA"}]},
 "connectedAssertion":{"assertions":[{"assertionName":"AlertX","category":"failure","entityType":"TypeB"}]}}`

// ownAndConnectedDisagree has the insight on the entity itself (failure)
// and on a connected entity under another category (error): the entity's own
// category wins.
const ownAndConnectedDisagree = `{"name":"name-a","type":"TypeA","scope":{"env":"prod-x","namespace":"ns1"},
 "assertion":{"assertions":[{"assertionName":"AlertX","category":"failure","entityType":"TypeA"}]},
 "connectedAssertion":{"assertions":[{"assertionName":"AlertX","category":"error","entityType":"TypeB"}]}}`

// otherTypeSameName has the entity's name and the insight, but another type.
const otherTypeSameName = `{"name":"name-a","type":"TypeB","entityType":"TypeB","scope":{"env":"prod-x","namespace":"ns1"},
 "assertion":{"assertions":[{"assertionName":"AlertX","category":"failure","entityType":"TypeB"}]}}`

const (
	hintOwn       = "added asserts_alert_category=failure, the insight's category on this entity"
	hintConnected = "added asserts_alert_category=failure, the insight's category on an entity connected to this one"
)

func TestInsightsSources_CategoryFill(t *testing.T) {
	forceNoColor(t)
	pinHumanMode(t)

	base := []string{"sources", "TypeA--name-a", "--from", "2026-01-01T00:00:00Z", "--to", "2026-01-01T01:00:00Z"}

	tests := []struct {
		name         string
		args         []string
		metrics      func(map[string]string) string
		search       string
		searchStatus int
		wantMetrics  bool
		wantSearches int
		// wantSourceCategories lists the asserts_alert_category label of each
		// source-metrics request in order ("" = label absent).
		wantSourceCategories []string
		// wantHint is the stderr hint expected ("" = no category hint).
		wantHint string
	}{
		{
			name:                 "first lookup returns metrics: no search, no retry",
			args:                 append([]string{"--insight", "AlertY"}, base...),
			metrics:              func(map[string]string) string { return ruleMetrics },
			search:               searchPage(ownFailure),
			wantMetrics:          true,
			wantSearches:         0,
			wantSourceCategories: []string{""},
		},
		{
			name:                 "empty lookup, own insight has one category: retried with it",
			args:                 append([]string{"--insight", "AlertX"}, base...),
			metrics:              metricsWhenFailure(),
			search:               searchPage(ownFailure),
			wantMetrics:          true,
			wantSearches:         1,
			wantSourceCategories: []string{"", "failure"},
			wantHint:             hintOwn,
		},
		{
			name:                 "empty lookup, insight only on a connected entity: its category is used",
			args:                 append([]string{"--insight", "AlertX"}, base...),
			metrics:              metricsWhenFailure(),
			search:               searchPage(connectedFailure),
			wantMetrics:          true,
			wantSearches:         1,
			wantSourceCategories: []string{"", "failure"},
			wantHint:             hintConnected,
		},
		{
			name:                 "own and connected insights disagree: the entity's own category is used",
			args:                 append([]string{"--insight", "AlertX"}, base...),
			metrics:              metricsWhenFailure(),
			search:               searchPage(ownAndConnectedDisagree),
			wantMetrics:          true,
			wantSearches:         1,
			wantSourceCategories: []string{"", "failure"},
			wantHint:             hintOwn,
		},
		{
			name:    "own insight has two categories, connected has one: no retry",
			args:    append([]string{"--insight", "AlertX"}, base...),
			metrics: metricsWhenFailure(),
			search: searchPage(`{"name":"name-a","type":"TypeA",
 "assertion":{"assertions":[{"assertionName":"AlertX","category":"error"},{"assertionName":"AlertX","category":"saturation"}]},
 "connectedAssertion":{"assertions":[{"assertionName":"AlertX","category":"failure"}]}}`),
			wantSearches:         1,
			wantSourceCategories: []string{""},
		},
		{
			name:    "own insight has no category, connected has one: no retry",
			args:    append([]string{"--insight", "AlertX"}, base...),
			metrics: metricsWhenFailure(),
			search: searchPage(`{"name":"name-a","type":"TypeA",
 "assertion":{"assertions":[{"assertionName":"AlertX"}]},
 "connectedAssertion":{"assertions":[{"assertionName":"AlertX","category":"failure"}]}}`),
			wantSearches:         1,
			wantSourceCategories: []string{""},
		},
		{
			name:                 "same name, other entity type: no retry",
			args:                 append([]string{"--insight", "AlertX"}, base...),
			metrics:              metricsWhenFailure(),
			search:               searchPage(otherTypeSameName),
			wantSearches:         1,
			wantSourceCategories: []string{""},
		},
		{
			name:                 "category passed by hand: no search",
			args:                 append([]string{"--insight", "AlertX", "--label", "asserts_alert_category=amend"}, base...),
			metrics:              metricsWhenFailure(),
			search:               searchPage(ownFailure),
			wantSearches:         0,
			wantSourceCategories: []string{"amend"},
		},
		{
			name:                 "no alertname: no search",
			args:                 base,
			metrics:              metricsWhenFailure(),
			search:               searchPage(ownFailure),
			wantSearches:         0,
			wantSourceCategories: []string{""},
		},
		{
			name:                 "insight name not on the entity: no retry",
			args:                 append([]string{"--insight", "OtherAlert"}, base...),
			metrics:              metricsWhenFailure(),
			search:               searchPage(ownFailure),
			wantSearches:         1,
			wantSourceCategories: []string{""},
		},
		{
			name:    "insight name with two categories: no retry",
			args:    append([]string{"--insight", "AlertX"}, base...),
			metrics: metricsWhenFailure(),
			search: searchPage(`{"name":"name-a","type":"TypeA","assertion":{"assertions":[
				{"assertionName":"AlertX","category":"failure"},{"assertionName":"AlertX","category":"error"}]}}`),
			wantSearches:         1,
			wantSourceCategories: []string{""},
		},
		{
			name:                 "another entity's insight is not used",
			args:                 append([]string{"--insight", "AlertX"}, base...),
			metrics:              metricsWhenFailure(),
			search:               searchPage(strings.Replace(ownFailure, `"name":"name-a"`, `"name":"name-b"`, 1)),
			wantSearches:         1,
			wantSourceCategories: []string{""},
		},
		{
			name:                 "search fails: the empty result is printed, exit 0",
			args:                 append([]string{"--insight", "AlertX"}, base...),
			metrics:              metricsWhenFailure(),
			searchStatus:         http.StatusInternalServerError,
			wantSearches:         1,
			wantSourceCategories: []string{""},
		},
		{
			name:                 "retry also empty: the empty result is printed",
			args:                 append([]string{"--insight", "AlertX"}, base...),
			metrics:              func(map[string]string) string { return `[]` },
			search:               searchPage(ownFailure),
			wantSearches:         1,
			wantSourceCategories: []string{"", "failure"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fake := &sourcesFake{metrics: tc.metrics, search: tc.search, searchStatus: tc.searchStatus}
			server := httptest.NewServer(fake.handler(t))
			defer server.Close()

			stdout, stderr, err := runKgCommand(t,
				func() *cobra.Command { return kg.NewInsightsCommand(writeLoaderFor(server)) },
				tc.args, "")
			require.NoError(t, err)

			var got []map[string]any
			require.NoError(t, json.Unmarshal([]byte(stdout), &got), "stdout: %s", stdout)
			if tc.wantMetrics {
				require.Len(t, got, 1)
				assert.Equal(t, "metric_x", got[0]["metricName"])
			} else {
				assert.Empty(t, got)
			}

			assert.Equal(t, tc.wantSearches, fake.searchCalls, "entity searches")
			gotCategories := make([]string, 0, len(fake.sourceCalls))
			for _, l := range fake.sourceCalls {
				gotCategories = append(gotCategories, l["asserts_alert_category"])
			}
			assert.Equal(t, tc.wantSourceCategories, gotCategories, "asserts_alert_category per source-metrics request")
			if tc.wantHint != "" {
				assert.Contains(t, stderr, tc.wantHint)
			} else {
				assert.NotContains(t, stderr, "asserts_alert_category")
			}
		})
	}
}

// The retry keeps every label of the first request and only adds the
// category, so it returns exactly what passing the label by hand returns.
func TestInsightsSources_CategoryFillKeepsLabels(t *testing.T) {
	forceNoColor(t)
	pinHumanMode(t)
	fake := &sourcesFake{metrics: metricsWhenFailure(), search: searchPage(ownFailure)}
	server := httptest.NewServer(fake.handler(t))
	defer server.Close()

	_, _, err := runKgCommand(t,
		func() *cobra.Command { return kg.NewInsightsCommand(writeLoaderFor(server)) },
		[]string{"sources", "-f", "-"},
		"startTime: 1\nendTime: 2\nlabels:\n  alertname: AlertX\n  asserts_entity_type: TypeA\n  asserts_entity_name: name-a\n  namespace: ns1\n  extra_label: v\n")
	require.NoError(t, err)
	require.Len(t, fake.sourceCalls, 2)
	want := maps.Clone(fake.sourceCalls[0])
	want["asserts_alert_category"] = "failure"
	assert.Equal(t, want, fake.sourceCalls[1])
}

// The category search covers the lookup's window and the scope labels it
// carries, filters on the entity's type and exact name, and asks for the
// insights of connected entities (the fallback reads them). The window is a
// past one that is not one hour long, so neither the default window nor a
// fixed-length window ending at --to matches it.
func TestInsightsSources_CategoryFillSearchRequest(t *testing.T) {
	forceNoColor(t)
	pinHumanMode(t)

	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(3*time.Hour + 20*time.Minute)
	window := []string{"--from", from.Format(time.RFC3339), "--to", to.Format(time.RFC3339)}

	tests := []struct {
		name      string
		scopeArgs []string
		// wantScope is scopeCriteria.nameAndValues (nil = scopeCriteria absent).
		wantScope map[string]any
	}{
		{name: "no scope flags: no scope criteria"},
		{
			name:      "env and namespace",
			scopeArgs: []string{"--env", "prod-x", "--namespace", "ns1"},
			wantScope: map[string]any{"env": []any{"prod-x"}, "namespace": []any{"ns1"}},
		},
		{
			name:      "site",
			scopeArgs: []string{"--site", "site-1"},
			wantScope: map[string]any{"site": []any{"site-1"}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fake := &sourcesFake{metrics: metricsWhenFailure(), search: searchPage(ownFailure)}
			server := httptest.NewServer(fake.handler(t))
			defer server.Close()

			args := append([]string{"sources", "TypeA--name-a", "--insight", "AlertX"}, window...)
			args = append(args, tc.scopeArgs...)
			_, _, err := runKgCommand(t,
				func() *cobra.Command { return kg.NewInsightsCommand(writeLoaderFor(server)) },
				args, "")
			require.NoError(t, err)
			require.Len(t, fake.searchBodies, 1)
			body := fake.searchBodies[0]

			tcrit, ok := body["timeCriteria"].(map[string]any)
			require.True(t, ok, "timeCriteria: %v", body["timeCriteria"])
			assert.InDelta(t, float64(from.UnixMilli()), tcrit["start"], 0)
			assert.InDelta(t, float64(to.UnixMilli()), tcrit["end"], 0)

			if tc.wantScope == nil {
				assert.NotContains(t, body, "scopeCriteria")
			} else {
				sc, ok := body["scopeCriteria"].(map[string]any)
				require.True(t, ok, "scopeCriteria: %v", body["scopeCriteria"])
				assert.Equal(t, tc.wantScope, sc["nameAndValues"])
			}

			filters, ok := body["filterCriteria"].([]any)
			require.True(t, ok)
			require.Len(t, filters, 1)
			f, ok := filters[0].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, "TypeA", f["entityType"])
			assert.Equal(t, true, f["havingAssertion"], "havingAssertion")
			assert.Equal(t, true, f["havingPropagatedAssertions"], "havingPropagatedAssertions")
			matchers, ok := f["propertyMatchers"].([]any)
			require.True(t, ok)
			require.Len(t, matchers, 1)
			m, ok := matchers[0].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, "name", m["name"])
			assert.Equal(t, "=", m["op"])
			assert.Equal(t, "name-a", m["value"])
		})
	}
}

// A cancelled command (Ctrl-C) during the category search or the retry
// returns the context's error and prints nothing, instead of the first
// lookup's empty array with exit 0.
func TestInsightsSources_CategoryFillCancelled(t *testing.T) {
	forceNoColor(t)
	pinHumanMode(t)

	tests := []struct {
		name string
		// search is the search answer; the cancel hook decides when to cancel.
		search string
		cancel func(f *sourcesFake, cancel context.CancelFunc)
	}{
		{
			name:   "cancelled during the search",
			search: searchPage(strings.Replace(ownFailure, `"AlertX"`, `"OtherAlert"`, 1)),
			cancel: func(f *sourcesFake, cancel context.CancelFunc) { f.onSearch = cancel },
		},
		{
			name:   "cancelled during the retry",
			search: searchPage(ownFailure),
			cancel: func(f *sourcesFake, cancel context.CancelFunc) {
				f.onSource = func(n int) {
					if n == 1 {
						cancel()
					}
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			fake := &sourcesFake{metrics: func(map[string]string) string { return `[]` }, search: tc.search}
			tc.cancel(fake, cancel)
			server := httptest.NewServer(fake.handler(t))
			defer server.Close()

			cmd := kg.NewInsightsCommand(writeLoaderFor(server))
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)
			cmd.SetArgs([]string{"sources", "TypeA--name-a", "--insight", "AlertX",
				"--from", "2026-01-01T00:00:00Z", "--to", "2026-01-01T01:00:00Z"})
			err := cmd.ExecuteContext(ctx)

			require.Error(t, err)
			require.ErrorIs(t, err, context.Canceled)
			assert.Empty(t, stdout.String())
			assert.Equal(t, 1, fake.searchCalls)
		})
	}
}

// The sources help must not present an empty array as proof that the labels
// are wrong: the same labels return an empty array for a window that does not
// cover the insight.
func TestInsightsSources_HelpEmptyArray(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	cmd := kg.NewInsightsCommand(writeLoaderFor(server))
	sources, _, err := cmd.Find([]string{"sources"})
	require.NoError(t, err)
	long := strings.Join(strings.Fields(sources.Long), " ")

	tests := []struct {
		phrase string
		want   bool
	}{
		{"An empty array means that nothing matched the labels sent in that window", true},
		{"check that --from/--to (or --since) covers the insight before changing labels", true},
		{"An empty array means that no rule matched the labels sent.", false},
	}
	for _, tc := range tests {
		t.Run(tc.phrase, func(t *testing.T) {
			assert.Equal(t, tc.want, strings.Contains(long, tc.phrase), "Long: %s", long)
		})
	}
}

// The sources help shows the command's shape with placeholders and states the
// label rule generically: the entity, the insight and every label come from
// what the graph reports for the reader's entity, not from names in the help.
func TestInsightsSources_HelpPlaceholders(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	cmd := kg.NewInsightsCommand(writeLoaderFor(server))
	sources, _, err := cmd.Find([]string{"sources"})
	require.NoError(t, err)

	examples := []struct {
		name string
		re   *regexp.Regexp
		want string
	}{
		{"entity argument", regexp.MustCompile(`gcx kg insights sources (\S+)`), "<Type>--<Name>"},
		{"--env", regexp.MustCompile(`--env (\S+)`), "<env>"},
		{"--namespace", regexp.MustCompile(`--namespace (\S+)`), "<namespace>"},
		{"--insight", regexp.MustCompile(`--insight (\S+)`), "<insight>"},
		{"--label", regexp.MustCompile(`--label (\S+)`), "<key>=<value>"},
	}
	for _, tc := range examples {
		t.Run("Example "+tc.name, func(t *testing.T) {
			matches := tc.re.FindAllStringSubmatch(sources.Example, -1)
			require.NotEmpty(t, matches, "Example: %s", sources.Example)
			for _, m := range matches {
				assert.Equal(t, tc.want, m[1], "Example: %s", sources.Example)
			}
		})
	}

	long := strings.Join(strings.Fields(sources.Long), " ")
	for _, phrase := range []string{
		"copy every label of the insight's timeline entry in 'gcx kg entities inspect' (timeLines[].labels) with --label",
		"For alert-rule and change insights also pass asserts_alert_category=<the entry's category>",
		"the insights the search lists for the entities connected to it",
	} {
		t.Run("Long "+phrase, func(t *testing.T) {
			assert.Contains(t, long, phrase)
		})
	}
}
