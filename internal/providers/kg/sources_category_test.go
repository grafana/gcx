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

// sourcesFake serves source-metrics, the insight timeline read and the
// entity type counts for the sources category fill-in tests, and records the
// label set of every source-metrics request and the body of every timeline
// and type-count request.
type sourcesFake struct {
	mu sync.Mutex
	// metrics returns the source-metrics answer for one request's labels;
	// sourceStatus, when set and non-zero for the nth (0-based) request,
	// overrides 200 for that request.
	metrics      func(labels map[string]string) string
	sourceStatus func(n int) int
	// timelines maps the entity type of a timeline request's filter to its
	// answer (default `[]`); timelineStatus overrides 200 when set.
	timelines      map[string]string
	timelineStatus int
	// types is the entity type count answer (default `{}`); typesStatus
	// overrides 200 when set.
	types       string
	typesStatus int
	// onTimeline, onTypes and onSource, when set, run before a timeline or
	// type-count request or the nth (0-based) source-metrics request is
	// answered.
	onTimeline func()
	onTypes    func()
	onSource   func(n int)

	sourceCalls    []map[string]string
	timelineBodies []map[string]any
	typeBodies     []map[string]any
	// searchCalls counts requests to the entity search (POST /v1/search),
	// which the fill must not use.
	searchCalls int
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
			n := len(f.sourceCalls)
			if f.onSource != nil {
				f.onSource(n)
			}
			f.sourceCalls = append(f.sourceCalls, body.Labels)
			if f.sourceStatus != nil {
				if status := f.sourceStatus(n); status != 0 {
					w.WriteHeader(status)
					return
				}
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(f.metrics(body.Labels)))
		case strings.HasSuffix(r.URL.Path, "/v1/search/assertions"):
			var body map[string]any
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			f.timelineBodies = append(f.timelineBodies, body)
			if f.onTimeline != nil {
				f.onTimeline()
			}
			if f.timelineStatus != 0 {
				w.WriteHeader(f.timelineStatus)
				return
			}
			answer := `[]`
			if a, ok := f.timelines[filterEntityType(body)]; ok {
				answer = a
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(answer))
		case strings.HasSuffix(r.URL.Path, "/v1/entity_type/count"):
			var body map[string]any
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			f.typeBodies = append(f.typeBodies, body)
			if f.onTypes != nil {
				f.onTypes()
			}
			if f.typesStatus != 0 {
				w.WriteHeader(f.typesStatus)
				return
			}
			answer := f.types
			if answer == "" {
				answer = `{}`
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(answer))
		case strings.HasSuffix(r.URL.Path, "/v1/search"):
			f.searchCalls++
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

// filterEntityType returns filterCriteria[0].entityType of a request body.
func filterEntityType(body map[string]any) string {
	filters, _ := body["filterCriteria"].([]any)
	if len(filters) == 0 {
		return ""
	}
	f, _ := filters[0].(map[string]any)
	s, _ := f["entityType"].(string)
	return s
}

const ruleMetrics = `[{"metricName":"metric_x","labels":[{"label":"namespace","op":"=","value":"ns1"}]}]`

// metricsWhenFailure answers like the backend for an alert-rule insight of
// category failure on an entity of type TypeA: the rule's metrics only when
// that category label and that type name are sent.
func metricsWhenFailure() func(map[string]string) string {
	return func(labels map[string]string) string {
		if labels["asserts_alert_category"] == "failure" && labels["asserts_entity_type"] == "TypeA" {
			return ruleMetrics
		}
		return `[]`
	}
}

// entry is one insight entry of a timeline: a node whose labels name the
// insight, the entity and, when category is set, asserts_alert_category.
func entry(entityType, name, alertname, category string) string {
	labels := map[string]string{
		"alertname":           alertname,
		"asserts_entity_type": entityType,
		"asserts_entity_name": name,
		"extra_label":         "v",
	}
	if category != "" {
		labels["asserts_alert_category"] = category
	}
	b, err := json.Marshal(map[string]any{"alertName": alertname, "name": name, "type": entityType, "labels": labels})
	if err != nil {
		panic(err)
	}
	return string(b)
}

// group is a timeline node without an alertname that nests other nodes, as
// the backend nests entries under per-category and per-kind nodes. Its name
// is not a category, so a category can only come from the entries' labels.
func group(nodes ...string) string {
	return `{"name":"group-1","labels":{},"nestedTimelines":[` + strings.Join(nodes, ",") + `]}`
}

// nameATimeline is an insight timeline answer with TypeA name-a's timeline.
func nameATimeline(nodes ...string) string {
	return `[{"type":"TypeA","name":"name-a","scope":{"env":"prod-x","namespace":"ns1"},` +
		`"allAssertions":[{"name":"name-a","type":"TypeA","labels":{},"nestedTimelines":[` +
		strings.Join(nodes, ",") + `]}]}]`
}

// ownFailure is TypeA name-a's timeline with AlertX (failure) and an
// unrelated insight of another category.
func ownFailure() string {
	return nameATimeline(
		group(entry("TypeA", "name-a", "AlertX", "failure")),
		group(entry("TypeA", "name-a", "OtherChange", "amend")))
}

const (
	hintTimeline = "added asserts_alert_category=failure, the category of this insight on the entity's insight timeline in the same window"
	hintType     = hintTimeline + ", and set asserts_entity_type=TypeA, the graph's name for entity type typea"
)

// categoryFillCase is one sources lookup run against sourcesFake.
type categoryFillCase struct {
	name           string
	args           []string
	metrics        func(map[string]string) string
	timelines      map[string]string
	timelineStatus int
	types          string
	typesStatus    int
	// sourceStatus overrides 200 for the nth (0-based) source-metrics
	// request when it returns non-zero.
	sourceStatus func(n int) int
	wantMetrics  bool
	// wantTimelineTypes lists the entity type of each timeline read.
	wantTimelineTypes []string
	wantTypeLookups   int
	// wantSourceCategories and wantSourceTypes list the
	// asserts_alert_category and asserts_entity_type labels of each
	// source-metrics request in order ("" = label absent).
	wantSourceCategories []string
	wantSourceTypes      []string
	// wantHint is the stderr hint expected ("" = no category hint).
	wantHint string
}

// fillArgs is a sources command for entity over a fixed window, with the
// insight AlertX and extra arguments.
func fillArgs(entity string, extra ...string) []string {
	return append([]string{"sources", entity, "--insight", "AlertX",
		"--from", "2026-01-01T00:00:00Z", "--to", "2026-01-01T01:00:00Z"}, extra...)
}

// runCategoryFillCases runs each case and checks the output, the requests
// the fill sent and the hint.
func runCategoryFillCases(t *testing.T, tests []categoryFillCase) {
	t.Helper()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fake := &sourcesFake{
				metrics: tc.metrics, sourceStatus: tc.sourceStatus,
				timelines: tc.timelines, timelineStatus: tc.timelineStatus,
				types: tc.types, typesStatus: tc.typesStatus,
			}
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

			gotTimelineTypes := make([]string, 0, len(fake.timelineBodies))
			for _, b := range fake.timelineBodies {
				gotTimelineTypes = append(gotTimelineTypes, filterEntityType(b))
			}
			wantTimelineTypes := tc.wantTimelineTypes
			if wantTimelineTypes == nil {
				wantTimelineTypes = []string{}
			}
			assert.Equal(t, wantTimelineTypes, gotTimelineTypes, "entity type per timeline read")
			assert.Len(t, fake.typeBodies, tc.wantTypeLookups, "entity type lookups")
			assert.Zero(t, fake.searchCalls, "entity searches")
			gotCategories := make([]string, 0, len(fake.sourceCalls))
			gotTypes := make([]string, 0, len(fake.sourceCalls))
			for _, l := range fake.sourceCalls {
				gotCategories = append(gotCategories, l["asserts_alert_category"])
				gotTypes = append(gotTypes, l["asserts_entity_type"])
			}
			assert.Equal(t, tc.wantSourceCategories, gotCategories, "asserts_alert_category per source-metrics request")
			assert.Equal(t, tc.wantSourceTypes, gotTypes, "asserts_entity_type per source-metrics request")
			switch tc.wantHint {
			case "":
				assert.NotContains(t, stderr, "asserts_alert_category")
			case hintTimeline:
				assert.Contains(t, stderr, tc.wantHint)
				assert.NotContains(t, stderr, "graph's name")
			default:
				assert.Contains(t, stderr, tc.wantHint)
			}
		})
	}
}

func TestInsightsSources_CategoryFill(t *testing.T) {
	forceNoColor(t)
	pinHumanMode(t)
	base := fillArgs("TypeA--name-a")
	runCategoryFillCases(t, []categoryFillCase{
		{
			name:                 "first lookup returns metrics: no timeline read, no retry",
			args:                 base,
			metrics:              func(map[string]string) string { return ruleMetrics },
			timelines:            map[string]string{"TypeA": ownFailure()},
			wantMetrics:          true,
			wantSourceCategories: []string{""},
			wantSourceTypes:      []string{"TypeA"},
		},
		{
			name:                 "empty lookup, the insight on the entity's timeline has one category: retried with it",
			args:                 base,
			metrics:              metricsWhenFailure(),
			timelines:            map[string]string{"TypeA": ownFailure()},
			wantMetrics:          true,
			wantTimelineTypes:    []string{"TypeA"},
			wantSourceCategories: []string{"", "failure"},
			wantSourceTypes:      []string{"TypeA", "TypeA"},
			wantHint:             hintTimeline,
		},
		{
			name:    "entry nested two groups deep is read",
			args:    base,
			metrics: metricsWhenFailure(),
			timelines: map[string]string{"TypeA": nameATimeline(
				group(group(entry("TypeA", "name-a", "AlertX", "failure"))))},
			wantMetrics:          true,
			wantTimelineTypes:    []string{"TypeA"},
			wantSourceCategories: []string{"", "failure"},
			wantSourceTypes:      []string{"TypeA", "TypeA"},
			wantHint:             hintTimeline,
		},
		{
			name:    "entries of the insight with two categories: no retry",
			args:    base,
			metrics: metricsWhenFailure(),
			timelines: map[string]string{"TypeA": nameATimeline(
				group(entry("TypeA", "name-a", "AlertX", "failure")),
				group(entry("TypeA", "name-a", "AlertX", "error")))},
			wantTimelineTypes:    []string{"TypeA"},
			wantSourceCategories: []string{""},
			wantSourceTypes:      []string{"TypeA"},
		},
		{
			name:    "entry of the insight without a category label: no retry",
			args:    base,
			metrics: metricsWhenFailure(),
			timelines: map[string]string{"TypeA": nameATimeline(
				group(entry("TypeA", "name-a", "AlertX", "")))},
			wantTimelineTypes:    []string{"TypeA"},
			wantSourceCategories: []string{""},
			wantSourceTypes:      []string{"TypeA"},
		},
		{
			name:                 "insight name not on the entity's timeline: no retry, no type lookup",
			args:                 []string{"sources", "TypeA--name-a", "--insight", "OtherAlert", "--from", "2026-01-01T00:00:00Z", "--to", "2026-01-01T01:00:00Z"},
			metrics:              metricsWhenFailure(),
			timelines:            map[string]string{"TypeA": ownFailure()},
			wantTimelineTypes:    []string{"TypeA"},
			wantSourceCategories: []string{""},
			wantSourceTypes:      []string{"TypeA"},
		},
		{
			name:                 "timeline of another entity name is not used",
			args:                 base,
			metrics:              metricsWhenFailure(),
			timelines:            map[string]string{"TypeA": strings.ReplaceAll(ownFailure(), "name-a", "name-b")},
			types:                `{"TypeA":2}`,
			wantTimelineTypes:    []string{"TypeA"},
			wantTypeLookups:      1,
			wantSourceCategories: []string{""},
			wantSourceTypes:      []string{"TypeA"},
		},
		{
			name:                 "timeline of the same name and another entity type is not used",
			args:                 base,
			metrics:              metricsWhenFailure(),
			timelines:            map[string]string{"TypeA": strings.ReplaceAll(ownFailure(), "TypeA", "TypeB")},
			types:                `{"TypeA":2,"TypeB":1}`,
			wantTimelineTypes:    []string{"TypeA"},
			wantTypeLookups:      1,
			wantSourceCategories: []string{""},
			wantSourceTypes:      []string{"TypeA"},
		},
		{
			// An insight raised on a related entity sits on the entity's own
			// timeline with labels that name the related entity.
			name:    "entry on the entity's timeline that names another entity: retried with its category",
			args:    base,
			metrics: metricsWhenFailure(),
			timelines: map[string]string{"TypeA": nameATimeline(
				group(group(entry("TypeB", "name-b", "AlertX", "failure"))))},
			wantMetrics:          true,
			wantTimelineTypes:    []string{"TypeA"},
			wantSourceCategories: []string{"", "failure"},
			wantSourceTypes:      []string{"TypeA", "TypeA"},
			wantHint:             hintTimeline,
		},
		{
			name:    "entries of the insight on the entity and on another entity with two categories: no retry",
			args:    base,
			metrics: metricsWhenFailure(),
			timelines: map[string]string{"TypeA": nameATimeline(
				group(entry("TypeA", "name-a", "AlertX", "failure")),
				group(entry("TypeB", "name-b", "AlertX", "error")))},
			wantTimelineTypes:    []string{"TypeA"},
			wantSourceCategories: []string{""},
			wantSourceTypes:      []string{"TypeA"},
		},
		{
			name:                 "category passed by hand: no timeline read",
			args:                 fillArgs("TypeA--name-a", "--label", "asserts_alert_category=amend"),
			metrics:              metricsWhenFailure(),
			timelines:            map[string]string{"TypeA": ownFailure()},
			wantSourceCategories: []string{"amend"},
			wantSourceTypes:      []string{"TypeA"},
		},
		{
			name:                 "no alertname: no timeline read",
			args:                 []string{"sources", "TypeA--name-a", "--from", "2026-01-01T00:00:00Z", "--to", "2026-01-01T01:00:00Z"},
			metrics:              metricsWhenFailure(),
			timelines:            map[string]string{"TypeA": ownFailure()},
			wantSourceCategories: []string{""},
			wantSourceTypes:      []string{"TypeA"},
		},
		{
			name:                 "timeline read fails: the empty result is printed, exit 0",
			args:                 base,
			metrics:              metricsWhenFailure(),
			timelineStatus:       http.StatusInternalServerError,
			wantTimelineTypes:    []string{"TypeA"},
			wantSourceCategories: []string{""},
			wantSourceTypes:      []string{"TypeA"},
		},
		{
			name:      "retry fails: the empty result is printed, exit 0",
			args:      base,
			metrics:   metricsWhenFailure(),
			timelines: map[string]string{"TypeA": ownFailure()},
			sourceStatus: func(n int) int {
				if n == 1 {
					return http.StatusInternalServerError
				}
				return 0
			},
			wantTimelineTypes:    []string{"TypeA"},
			wantSourceCategories: []string{"", "failure"},
			wantSourceTypes:      []string{"TypeA", "TypeA"},
		},
		{
			name:                 "retry also empty: the empty result is printed",
			args:                 base,
			metrics:              func(map[string]string) string { return `[]` },
			timelines:            map[string]string{"TypeA": ownFailure()},
			wantTimelineTypes:    []string{"TypeA"},
			wantSourceCategories: []string{"", "failure"},
			wantSourceTypes:      []string{"TypeA", "TypeA"},
		},
	})
}

// An entity type written in another case than the graph's type name.
func TestInsightsSources_CategoryFillEntityTypeCase(t *testing.T) {
	forceNoColor(t)
	pinHumanMode(t)
	base := fillArgs("TypeA--name-a")
	lower := fillArgs("typea--name-a")
	runCategoryFillCases(t, []categoryFillCase{
		{
			name:                 "type in another case: the graph's type name is used for the timeline and the retry",
			args:                 lower,
			metrics:              metricsWhenFailure(),
			timelines:            map[string]string{"TypeA": ownFailure()},
			types:                `{"TypeA":2,"TypeB":1}`,
			wantMetrics:          true,
			wantTimelineTypes:    []string{"typea", "TypeA"},
			wantTypeLookups:      1,
			wantSourceCategories: []string{"", "failure"},
			wantSourceTypes:      []string{"typea", "TypeA"},
			wantHint:             hintType,
		},
		{
			name: "timeline answered for the type in another case: its entries are used, the type is kept",
			args: lower,
			metrics: func(labels map[string]string) string {
				if labels["asserts_alert_category"] == "failure" {
					return ruleMetrics
				}
				return `[]`
			},
			timelines:            map[string]string{"typea": ownFailure()},
			wantMetrics:          true,
			wantTimelineTypes:    []string{"typea"},
			wantSourceCategories: []string{"", "failure"},
			wantSourceTypes:      []string{"typea", "typea"},
			wantHint:             hintTimeline,
		},
		{
			name:                 "type in another case, two graph types match it: no retry",
			args:                 lower,
			metrics:              metricsWhenFailure(),
			timelines:            map[string]string{"TypeA": ownFailure()},
			types:                `{"TypeA":2,"TYPEA":1}`,
			wantTimelineTypes:    []string{"typea"},
			wantTypeLookups:      1,
			wantSourceCategories: []string{""},
			wantSourceTypes:      []string{"typea"},
		},
		{
			name:                 "type in another case, type lookup fails: no retry",
			args:                 lower,
			metrics:              metricsWhenFailure(),
			timelines:            map[string]string{"TypeA": ownFailure()},
			typesStatus:          http.StatusInternalServerError,
			wantTimelineTypes:    []string{"typea"},
			wantTypeLookups:      1,
			wantSourceCategories: []string{""},
			wantSourceTypes:      []string{"typea"},
		},
		{
			name:                 "entity not on any timeline and no type in another case: no retry",
			args:                 base,
			metrics:              metricsWhenFailure(),
			types:                `{"TypeA":2,"TypeB":1}`,
			wantTimelineTypes:    []string{"TypeA"},
			wantTypeLookups:      1,
			wantSourceCategories: []string{""},
			wantSourceTypes:      []string{"TypeA"},
		},
	})
}

// The retry keeps every label of the first request and only adds the
// category, so it returns exactly what passing the label by hand returns.
func TestInsightsSources_CategoryFillKeepsLabels(t *testing.T) {
	forceNoColor(t)
	pinHumanMode(t)
	fake := &sourcesFake{metrics: metricsWhenFailure(), timelines: map[string]string{"TypeA": ownFailure()}}
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

// The timeline read covers the lookup's window and the scope labels it
// carries and filters on the entity's type and exact name, without the
// havingAssertion filter; the type lookup covers the same window and scope.
// The window is a past one that is not one hour long, so neither the default
// window nor a fixed-length window ending at --to matches it.
func TestInsightsSources_CategoryFillTimelineRequest(t *testing.T) {
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
	checkWindowAndScope := func(t *testing.T, body map[string]any, wantScope map[string]any) {
		t.Helper()
		tcrit, ok := body["timeCriteria"].(map[string]any)
		require.True(t, ok, "timeCriteria: %v", body["timeCriteria"])
		assert.InDelta(t, float64(from.UnixMilli()), tcrit["start"], 0)
		assert.InDelta(t, float64(to.UnixMilli()), tcrit["end"], 0)
		if wantScope == nil {
			assert.NotContains(t, body, "scopeCriteria")
			return
		}
		sc, ok := body["scopeCriteria"].(map[string]any)
		require.True(t, ok, "scopeCriteria: %v", body["scopeCriteria"])
		assert.Equal(t, wantScope, sc["nameAndValues"])
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// The type is written in another case, so the fill reads the
			// timeline, looks the type up and reads the timeline again.
			fake := &sourcesFake{
				metrics:   metricsWhenFailure(),
				timelines: map[string]string{"TypeA": ownFailure()},
				types:     `{"TypeA":1}`,
			}
			server := httptest.NewServer(fake.handler(t))
			defer server.Close()

			args := append([]string{"sources", "typea--name-a", "--insight", "AlertX"}, window...)
			args = append(args, tc.scopeArgs...)
			_, _, err := runKgCommand(t,
				func() *cobra.Command { return kg.NewInsightsCommand(writeLoaderFor(server)) },
				args, "")
			require.NoError(t, err)
			require.Len(t, fake.timelineBodies, 2)
			require.Len(t, fake.typeBodies, 1)

			checkWindowAndScope(t, fake.typeBodies[0], tc.wantScope)
			for i, body := range fake.timelineBodies {
				checkWindowAndScope(t, body, tc.wantScope)

				filters, ok := body["filterCriteria"].([]any)
				require.True(t, ok)
				require.Len(t, filters, 1)
				f, ok := filters[0].(map[string]any)
				require.True(t, ok)
				assert.Equal(t, []string{"typea", "TypeA"}[i], f["entityType"])
				assert.NotContains(t, f, "havingAssertion")
				assert.NotContains(t, f, "havingPropagatedAssertions")
				matchers, ok := f["propertyMatchers"].([]any)
				require.True(t, ok)
				require.Len(t, matchers, 1)
				m, ok := matchers[0].(map[string]any)
				require.True(t, ok)
				assert.Equal(t, "name", m["name"])
				assert.Equal(t, "=", m["op"])
				assert.Equal(t, "name-a", m["value"])
			}
		})
	}
}

// A cancelled command (Ctrl-C) during the timeline read, the type lookup or
// the retry returns the context's error and prints nothing, instead of the
// first lookup's empty array with exit 0.
func TestInsightsSources_CategoryFillCancelled(t *testing.T) {
	forceNoColor(t)
	pinHumanMode(t)

	tests := []struct {
		name   string
		entity string
		// timelines is the timeline answer; the cancel hook decides when to
		// cancel.
		timelines map[string]string
		cancel    func(f *sourcesFake, cancel context.CancelFunc)
	}{
		{
			name:      "cancelled during the timeline read",
			entity:    "TypeA--name-a",
			timelines: map[string]string{"TypeA": strings.ReplaceAll(ownFailure(), `"AlertX"`, `"OtherAlert"`)},
			cancel:    func(f *sourcesFake, cancel context.CancelFunc) { f.onTimeline = cancel },
		},
		{
			name:      "cancelled during the type lookup",
			entity:    "typea--name-a",
			timelines: map[string]string{"TypeA": ownFailure()},
			cancel:    func(f *sourcesFake, cancel context.CancelFunc) { f.onTypes = cancel },
		},
		{
			name:      "cancelled during the retry",
			entity:    "TypeA--name-a",
			timelines: map[string]string{"TypeA": ownFailure()},
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
			fake := &sourcesFake{metrics: func(map[string]string) string { return `[]` }, timelines: tc.timelines, types: `{"TypeA":1}`}
			tc.cancel(fake, cancel)
			server := httptest.NewServer(fake.handler(t))
			defer server.Close()

			cmd := kg.NewInsightsCommand(writeLoaderFor(server))
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)
			cmd.SetArgs([]string{"sources", tc.entity, "--insight", "AlertX",
				"--from", "2026-01-01T00:00:00Z", "--to", "2026-01-01T01:00:00Z"})
			err := cmd.ExecuteContext(ctx)

			require.Error(t, err)
			require.ErrorIs(t, err, context.Canceled)
			assert.Empty(t, stdout.String())
			assert.NotEmpty(t, fake.timelineBodies)
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
// Every example follows the rule it states: it passes the timeline entry's
// labels, and the alert-rule and change example also passes the category.
func TestInsightsSources_HelpPlaceholders(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	cmd := kg.NewInsightsCommand(writeLoaderFor(server))
	sources, _, err := cmd.Find([]string{"sources"})
	require.NoError(t, err)

	examples := []struct {
		name string
		re   *regexp.Regexp
		want []string
	}{
		{"entity argument", regexp.MustCompile(`gcx kg insights sources (\S+)`), []string{"<Type>--<Name>"}},
		{"--env", regexp.MustCompile(`--env (\S+)`), []string{"<env>"}},
		{"--namespace", regexp.MustCompile(`--namespace (\S+)`), []string{"<namespace>"}},
		{"--insight", regexp.MustCompile(`--insight (\S+)`), []string{"'<insight>'"}},
		{"--label", regexp.MustCompile(`--label (\S+)`), []string{"<key>=<value>", "asserts_alert_category=<category>"}},
	}
	for _, tc := range examples {
		t.Run("Example "+tc.name, func(t *testing.T) {
			matches := tc.re.FindAllStringSubmatch(sources.Example, -1)
			require.NotEmpty(t, matches, "Example: %s", sources.Example)
			for _, m := range matches {
				assert.Contains(t, tc.want, m[1], "Example: %s", sources.Example)
			}
		})
	}

	// Each example is a comment followed by one command; every command
	// passes the timeline entry's labels, and the alert-rule and change
	// example passes the category as well.
	blocks := strings.Split(strings.TrimSpace(sources.Example), "\n\n")
	require.Len(t, blocks, 2, "Example: %s", sources.Example)
	for i, b := range blocks {
		flat := strings.Join(strings.Fields(b), " ")
		t.Run("Example labels "+[]string{"first", "second"}[i], func(t *testing.T) {
			assert.Contains(t, flat, "--label <key>=<value> # repeat for each label in the entry's timeLines[].labels", "Example: %s", b)
			if strings.Contains(flat, "alert-rule or change insight") {
				assert.Contains(t, flat, "--label asserts_alert_category=<category>", "Example: %s", b)
			} else {
				assert.NotContains(t, flat, "asserts_alert_category", "Example: %s", b)
			}
		})
	}

	// An insight name with a space, filled in unquoted, would be cut at the
	// space: the example quotes the placeholder and says why.
	t.Run("Example quoting note", func(t *testing.T) {
		assert.Contains(t, strings.Join(strings.Fields(sources.Example), " "),
			"(quote the insight name: insight names can contain spaces)")
	})

	long := strings.Join(strings.Fields(sources.Long), " ")
	for _, phrase := range []string{
		"copy every label of the insight's timeline entry in 'gcx kg entities inspect' (timeLines[].labels) with --label",
		"For alert-rule and change insights also pass asserts_alert_category=<the entry's category>",
		"sources reads the entity's insight timeline for the same window and scope",
		"When the timeline read finds no entity, sources looks up the entity type names of that window and scope",
		"it repeats the timeline read with that name and sends it in the retry",
	} {
		t.Run("Long "+phrase, func(t *testing.T) {
			assert.Contains(t, long, phrase)
		})
	}
	t.Run("Long no connected-entity fallback", func(t *testing.T) {
		assert.NotContains(t, long, "connected to it")
	})
}
