package kg_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/kg"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func TestSummarizeEntityCounts(t *testing.T) {
	listed := map[string]bool{"TypeX": true, "TypeY": true}
	tests := []struct {
		name   string
		counts map[string]int64
		listed map[string]bool
		want   kg.EntitiesSummary
	}{
		{
			name:   "no entities",
			counts: map[string]int64{},
			listed: listed,
			want:   kg.EntitiesSummary{ByType: map[string]int64{}},
		},
		{
			name:   "types the schema lists are counted",
			counts: map[string]int64{"TypeX": 3, "TypeY": 7},
			listed: listed,
			want:   kg.EntitiesSummary{Total: 10, ByType: map[string]int64{"TypeX": 3, "TypeY": 7}},
		},
		{
			name: "types the schema does not list and record types are split out of the total",
			counts: map[string]int64{
				"TypeX": 3, "TypeY": 7,
				"RecordTypeA": 3, "RecordTypeB": 5, "Account": 2, "Env": 4,
			},
			listed: listed,
			want: kg.EntitiesSummary{
				Total:  10,
				ByType: map[string]int64{"TypeX": 3, "TypeY": 7},
				Internal: &kg.EntityCount{
					Total:  14,
					ByType: map[string]int64{"RecordTypeA": 3, "RecordTypeB": 5, "Account": 2, "Env": 4},
				},
			},
		},
		{
			name:   "only catalog records leaves an empty total",
			counts: map[string]int64{"RecordTypeA": 3, "Account": 2},
			listed: listed,
			want: kg.EntitiesSummary{
				ByType: map[string]int64{},
				Internal: &kg.EntityCount{
					Total:  5,
					ByType: map[string]int64{"RecordTypeA": 3, "Account": 2},
				},
			},
		},
		{
			name:   "zero counts are dropped",
			counts: map[string]int64{"TypeX": 0, "TypeY": 2, "RecordTypeA": 0},
			listed: listed,
			want:   kg.EntitiesSummary{Total: 2, ByType: map[string]int64{"TypeY": 2}},
		},
		{
			name: "without the schema only the catalog record types are split out",
			counts: map[string]int64{
				"TypeX": 3, "RecordTypeA": 5,
				"Account": 2, "EntityType": 6, "Env": 4, "Schema": 8,
			},
			listed: nil,
			want: kg.EntitiesSummary{
				Total:  8,
				ByType: map[string]int64{"TypeX": 3, "RecordTypeA": 5},
				Internal: &kg.EntityCount{
					Total:  20,
					ByType: map[string]int64{"Account": 2, "EntityType": 6, "Env": 4, "Schema": 8},
				},
			},
		},
		{
			name: "an empty schema splits out only the catalog record types, as without the schema",
			counts: map[string]int64{
				"TypeX": 3, "RecordTypeA": 5,
				"Account": 2, "EntityType": 6, "Env": 4, "Schema": 8,
			},
			listed: map[string]bool{},
			want: kg.EntitiesSummary{
				Total:  8,
				ByType: map[string]int64{"TypeX": 3, "RecordTypeA": 5},
				Internal: &kg.EntityCount{
					Total:  20,
					ByType: map[string]int64{"Account": 2, "EntityType": 6, "Env": 4, "Schema": 8},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, kg.SummarizeEntityCounts(tt.counts, tt.listed))
		})
	}
}

func TestIsCatalogType(t *testing.T) {
	listed := map[string]bool{"TypeX": true}
	tests := []struct {
		name   string
		typ    string
		listed map[string]bool
		want   bool
	}{
		{name: "a type the schema lists", typ: "TypeX", listed: listed, want: false},
		{name: "a type the schema does not list", typ: "RecordTypeA", listed: listed, want: true},
		{name: "account records", typ: "Account", listed: listed, want: true},
		{name: "environment records", typ: "Env", listed: listed, want: true},
		{name: "a catalog record type even when listed", typ: "Env", listed: map[string]bool{"Env": true}, want: true},
		{name: "without the schema an unknown type is not a catalog type", typ: "RecordTypeA", listed: nil, want: false},
		{name: "without the schema a listed type is not a catalog type", typ: "TypeX", listed: nil, want: false},
		{name: "without the schema account records still are", typ: "Account", listed: nil, want: true},
		{name: "without the schema entity-type records still are", typ: "EntityType", listed: nil, want: true},
		{name: "without the schema environment records still are", typ: "Env", listed: nil, want: true},
		{name: "without the schema the Schema records still are", typ: "Schema", listed: nil, want: true},
		{name: "an empty schema lists no type, so an unknown type is not a catalog type", typ: "RecordTypeA", listed: map[string]bool{}, want: false},
		{name: "an empty schema leaves a catalog record type a catalog type", typ: "Schema", listed: map[string]bool{}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, kg.IsCatalogType(tt.typ, tt.listed))
		})
	}
}

func TestListedSchemaTypes(t *testing.T) {
	tests := []struct {
		name string
		body string
		want map[string]bool
	}{
		{
			name: "lists the schema's types except the catalog record types",
			body: `{"data":{"entities":[{"id":1,"name":"TypeX"},{"id":2,"name":"TypeY"},{"id":3,"name":"Account"},{"id":4,"name":"Env"},{"id":5,"name":"EntityType"},{"id":6,"name":"Schema"}]}}`,
			want: map[string]bool{"TypeX": true, "TypeY": true},
		},
		{
			name: "an unnamed type is listed as Unknown, as kg meta schema shows it",
			body: `{"data":{"entities":[{"id":1,"name":""}]}}`,
			want: map[string]bool{"Unknown": true},
		},
		{
			name: "an empty schema lists nothing",
			body: `{"data":{"entities":[]}}`,
			want: map[string]bool{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var resp kg.GraphSchemaResponse
			require.NoError(t, json.Unmarshal([]byte(tt.body), &resp))
			assert.Equal(t, tt.want, kg.ListedSchemaTypes(resp))
		})
	}
}

func TestEntitiesSummary_JSONOmitsEmptyInternal(t *testing.T) {
	listed := map[string]bool{"TypeY": true}
	tests := []struct {
		name   string
		counts map[string]int64
		want   string
	}{
		{
			name:   "no catalog records",
			counts: map[string]int64{"TypeY": 2},
			want:   `{"total":2,"byType":{"TypeY":2}}`,
		},
		{
			name:   "with catalog records",
			counts: map[string]int64{"TypeY": 2, "RecordTypeA": 5},
			want:   `{"total":2,"byType":{"TypeY":2},"internal":{"total":5,"byType":{"RecordTypeA":5}}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := json.Marshal(kg.SummarizeEntityCounts(tt.counts, listed))
			require.NoError(t, err)
			assert.JSONEq(t, tt.want, string(b))
		})
	}
}

func insightResult(insights ...any) kg.SearchResult {
	return kg.SearchResult{Assertion: map[string]any{"assertions": insights}}
}

func TestSummarizeInsights(t *testing.T) {
	tests := []struct {
		name    string
		results []kg.SearchResult
		want    kg.InsightsSummary
	}{
		{
			name:    "no insights",
			results: []kg.SearchResult{{}, insightResult()},
			want: kg.InsightsSummary{
				ByCategory: map[string]kg.InsightGroup{},
				BySeverity: map[string]int{},
				ByName:     map[string]int{},
			},
		},
		{
			name: "grouped by category with per-category names",
			results: []kg.SearchResult{
				insightResult(
					map[string]any{"assertionName": "InsightA", "category": "error", "severity": "critical"},
					map[string]any{"assertionName": "InsightB", "category": "amend", "severity": "info"},
				),
				insightResult(
					map[string]any{"assertionName": "InsightA", "category": "error", "severity": "critical"},
					map[string]any{"assertionName": "InsightC", "category": "anomaly", "severity": "warning"},
				),
			},
			want: kg.InsightsSummary{
				Total: 4,
				ByCategory: map[string]kg.InsightGroup{
					"error":   {Total: 2, ByName: map[string]int{"InsightA": 2}},
					"amend":   {Total: 1, ByName: map[string]int{"InsightB": 1}},
					"anomaly": {Total: 1, ByName: map[string]int{"InsightC": 1}},
				},
				BySeverity: map[string]int{"CRITICAL": 2, "INFO": 1, "WARNING": 1},
				ByName:     map[string]int{"InsightA": 2, "InsightB": 1, "InsightC": 1},
			},
		},
		{
			name: "category case is normalised and missing fields count as unknown",
			results: []kg.SearchResult{
				insightResult(
					map[string]any{"assertionName": "InsightE", "category": "Saturation", "severity": "warning"},
					map[string]any{},
					"not an insight",
				),
			},
			want: kg.InsightsSummary{
				Total: 2,
				ByCategory: map[string]kg.InsightGroup{
					"saturation": {Total: 1, ByName: map[string]int{"InsightE": 1}},
					"unknown":    {Total: 1, ByName: map[string]int{"UNKNOWN": 1}},
				},
				BySeverity: map[string]int{"WARNING": 1, "UNKNOWN": 1},
				ByName:     map[string]int{"InsightE": 1, "UNKNOWN": 1},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, kg.SummarizeInsights(tt.results))
		})
	}
}

// statsPage is one canned answer of the fake search endpoint.
type statsPage struct {
	entities []map[string]any
	last     bool
	limitHit bool
	status   int // non-zero: answer with this HTTP status instead
}

func statsEntity(typ, name string, insights ...map[string]any) map[string]any {
	list := make([]any, len(insights))
	for i, a := range insights {
		list[i] = a
	}
	return map[string]any{
		"name": name, "type": typ, "scope": map[string]any{"env": "env-a"},
		"assertion": map[string]any{"assertions": list},
	}
}

func statsInsight(name, category string) map[string]any {
	return map[string]any{"assertionName": name, "category": category, "severity": "warning"}
}

// statsFake is the API a TestStatsCommand case runs against. It answers
// the schema request with schema only for the fromMs..toMs window, and with
// an empty schema for any other window.
type statsFake struct {
	t            *testing.T
	counts       map[string]int64
	schema       string
	fromMs, toMs int64
	schemaStatus int // non-zero: the schema request fails with this HTTP status
	pages        map[string][]statsPage

	mu       sync.Mutex
	searched map[string]bool
	// inFlight and maxInFlight count the insight searches being answered
	// at the same time; held is set once the first one has been held back.
	inFlight, maxInFlight int
	held                  bool
}

func (f *statsFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	const prefix = "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v1"
	switch r.URL.Path {
	case prefix + "/entity_type/count":
		writeJSON(w, f.counts)
	case prefix + "/search":
		var req struct {
			DefinitionID int `json:"definitionId"`
			TimeCriteria struct {
				Start int64 `json:"start"`
				End   int64 `json:"end"`
			} `json:"timeCriteria"`
			FilterCriteria []struct {
				EntityType string `json:"entityType"`
			} `json:"filterCriteria"`
			PageNum int `json:"pageNum"`
		}
		if !assert.NoError(f.t, json.NewDecoder(r.Body).Decode(&req)) {
			return
		}
		if req.DefinitionID != 0 {
			f.serveSchema(w, req.TimeCriteria.Start, req.TimeCriteria.End)
			return
		}
		if !assert.Len(f.t, req.FilterCriteria, 1) {
			return
		}
		f.servePage(w, req.FilterCriteria[0].EntityType, req.PageNum)
	default:
		f.t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *statsFake) serveSchema(w http.ResponseWriter, startMs, endMs int64) {
	switch {
	case f.schemaStatus != 0:
		w.WriteHeader(f.schemaStatus)
		_, _ = fmt.Fprint(w, `{"message":"boom"}`)
	case startMs != f.fromMs || endMs != f.toMs:
		_, _ = fmt.Fprint(w, `{"data":{"entities":[],"edges":[]}}`)
	default:
		_, _ = fmt.Fprint(w, f.schema)
	}
}

func (f *statsFake) servePage(w http.ResponseWriter, entityType string, pageNum int) {
	f.mu.Lock()
	if f.searched == nil {
		f.searched = map[string]bool{}
	}
	f.searched[entityType] = true
	f.inFlight++
	f.maxInFlight = max(f.maxInFlight, f.inFlight)
	hold := !f.held
	f.held = true
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.inFlight--
		f.mu.Unlock()
	}()
	if hold {
		// Hold the first search back long enough for a search sent at the
		// same time to arrive while it is still being answered.
		time.Sleep(50 * time.Millisecond)
	}
	pages := f.pages[entityType]
	if pageNum >= len(pages) {
		_, _ = fmt.Fprint(w, `{"data":{"entities":[],"lastPage":true}}`)
		return
	}
	p := pages[pageNum]
	if p.status != 0 {
		w.WriteHeader(p.status)
		_, _ = fmt.Fprint(w, `{"message":"boom"}`)
		return
	}
	writeJSON(w, map[string]any{"data": map[string]any{
		"entities": p.entities, "pageNum": pageNum, "lastPage": p.last, "searchResultsMaxLimitHit": p.limitHit,
	}})
}

// searchedTypes returns, sorted, the entity types the insight search asked
// for.
func (f *statsFake) searchedTypes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.searched))
	for et := range f.searched {
		out = append(out, et)
	}
	sort.Strings(out)
	return out
}

// maxConcurrentSearches returns the largest number of insight searches the
// fake answered at the same time.
func (f *statsFake) maxConcurrentSearches() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.maxInFlight
}

// statsOutput is the part of the `kg stats` JSON output TestStatsCommand
// checks.
type statsOutput struct {
	Entities struct {
		Total    float64            `json:"total"`
		ByType   map[string]float64 `json:"byType"`
		Internal struct {
			Total float64 `json:"total"`
		} `json:"internal"`
	} `json:"entities"`
	Insights struct {
		Total      float64 `json:"total"`
		ByCategory map[string]struct {
			Total float64 `json:"total"`
		} `json:"byCategory"`
		IncompleteTypes []any `json:"incompleteTypes"`
	} `json:"insights"`
}

func TestStatsCommand(t *testing.T) {
	// The window every case passes with --from/--to, in milliseconds.
	const fromMs, toMs = 1757000000000, 1757003600000
	counts := map[string]int64{"TypeX": 3, "TypeY": 2, "Account": 2, "EntityType": 1, "RecordTypeA": 5}
	// The schema lists Account like any type; RecordTypeA, which it does not
	// list, is a catalog type only because of that. The fake answers with it
	// only for the --from/--to window and with an empty schema otherwise.
	schema := `{"data":{"entities":[{"id":1,"name":"TypeX"},{"id":2,"name":"TypeY"},{"id":3,"name":"Account"}],"edges":[]}}`
	errInsight := statsInsight("InsightA", "error")
	amend := statsInsight("InsightB", "amend")
	yPage := statsPage{entities: []map[string]any{statsEntity("TypeY", "y-1", errInsight)}, last: true}
	allTypes := []string{"Account", "EntityType", "RecordTypeA", "TypeX", "TypeY"}

	// wantEntities is the entities block of the output.
	type wantEntities struct {
		total    float64
		byType   map[string]float64
		internal float64
	}
	withSchema := wantEntities{total: 5, byType: map[string]float64{"TypeX": 3, "TypeY": 2}, internal: 8}

	tests := []struct {
		name           string
		args           []string
		maxPages       int
		pages          map[string][]statsPage
		schemaStatus   int           // non-zero: the schema request fails with this HTTP status
		wantEntities   *wantEntities // nil: withSchema
		wantSearched   []string
		wantInsights   float64
		wantByCategory map[string]float64
		wantIncomplete []any
		wantStderr     string
		wantErr        string // non-empty: the command fails with an error containing this
	}{
		{
			name: "follows pages, counts a repeated row once, counts insights on catalog records",
			pages: map[string][]statsPage{
				"TypeX": {
					{entities: []map[string]any{statsEntity("TypeX", "x-1", errInsight), statsEntity("TypeX", "x-2", amend)}},
					{entities: []map[string]any{statsEntity("TypeX", "x-2", amend), statsEntity("TypeX", "x-3", errInsight)}, last: true},
				},
				"TypeY":   {yPage},
				"Account": {{entities: []map[string]any{statsEntity("Account", "record-1", statsInsight("InsightD", "failure"))}, last: true}},
			},
			wantSearched:   allTypes,
			wantInsights:   5,
			wantByCategory: map[string]float64{"error": 3, "amend": 1, "failure": 1},
		},
		{
			name:     "a type past the page bound is listed as incomplete",
			maxPages: 2,
			pages: map[string][]statsPage{
				"TypeX": {
					{entities: []map[string]any{statsEntity("TypeX", "x-1", errInsight)}},
					{entities: []map[string]any{statsEntity("TypeX", "x-2", errInsight)}},
					{entities: []map[string]any{statsEntity("TypeX", "x-3", errInsight)}, last: true},
				},
				"TypeY": {yPage},
			},
			wantSearched:   allTypes,
			wantInsights:   3,
			wantByCategory: map[string]float64{"error": 3},
			wantIncomplete: []any{"TypeX"},
		},
		{
			name: "a server error on the first page skips the type and marks it",
			pages: map[string][]statsPage{
				"TypeX": {{status: http.StatusInternalServerError}},
				"TypeY": {yPage},
			},
			wantSearched:   allTypes,
			wantInsights:   1,
			wantByCategory: map[string]float64{"error": 1},
			wantIncomplete: []any{"TypeX"},
			wantStderr:     `skipping entity type "TypeX"`,
		},
		{
			name: "a server error on a later page keeps the earlier rows and marks the type",
			pages: map[string][]statsPage{
				"TypeX": {
					{entities: []map[string]any{statsEntity("TypeX", "x-1", errInsight)}},
					{status: http.StatusInternalServerError},
				},
				"TypeY": {yPage},
			},
			wantSearched:   allTypes,
			wantInsights:   2,
			wantByCategory: map[string]float64{"error": 2},
			wantIncomplete: []any{"TypeX"},
			wantStderr:     `entity type "TypeX": page 1 failed`,
		},
		{
			name: "a rate limit on the first page skips the type and marks it",
			pages: map[string][]statsPage{
				"TypeX": {{status: http.StatusTooManyRequests}},
				"TypeY": {yPage},
			},
			wantSearched:   allTypes,
			wantInsights:   1,
			wantByCategory: map[string]float64{"error": 1},
			wantIncomplete: []any{"TypeX"},
			wantStderr:     `skipping entity type "TypeX" — rate limited`,
		},
		{
			name: "a rate limit on a later page keeps the earlier rows and marks the type",
			pages: map[string][]statsPage{
				"TypeX": {
					{entities: []map[string]any{statsEntity("TypeX", "x-1", errInsight)}},
					{status: http.StatusTooManyRequests},
				},
				"TypeY": {yPage},
			},
			wantSearched:   allTypes,
			wantInsights:   2,
			wantByCategory: map[string]float64{"error": 2},
			wantIncomplete: []any{"TypeX"},
			wantStderr:     `entity type "TypeX": page 1 failed — rate limited`,
		},
		{
			name: "any other client error fails the command",
			pages: map[string][]statsPage{
				"TypeX": {{status: http.StatusBadRequest}},
				"TypeY": {yPage},
			},
			wantErr: "search entity type TypeX",
		},
		{
			name: "a server-side result limit marks the type",
			pages: map[string][]statsPage{
				"TypeX": {{entities: []map[string]any{statsEntity("TypeX", "x-1", amend)}, last: true, limitHit: true}},
				"TypeY": {yPage},
			},
			wantSearched:   allTypes,
			wantInsights:   2,
			wantByCategory: map[string]float64{"amend": 1, "error": 1},
			wantIncomplete: []any{"TypeX"},
		},
		{
			name: "a page at the server's result limit is followed by the next page",
			pages: map[string][]statsPage{
				"TypeX": {
					{entities: []map[string]any{statsEntity("TypeX", "x-1", amend)}, limitHit: true},
					{entities: []map[string]any{statsEntity("TypeX", "x-2", errInsight)}, last: true},
				},
				"TypeY": {yPage},
			},
			wantSearched:   allTypes,
			wantInsights:   3,
			wantByCategory: map[string]float64{"amend": 1, "error": 2},
		},
		{
			name: "an empty page after a page at the result limit marks the type",
			pages: map[string][]statsPage{
				"TypeX": {{entities: []map[string]any{statsEntity("TypeX", "x-1", amend)}, limitHit: true}},
				"TypeY": {yPage},
			},
			wantSearched:   allTypes,
			wantInsights:   2,
			wantByCategory: map[string]float64{"amend": 1, "error": 1},
			wantIncomplete: []any{"TypeX"},
		},
		{
			name:         "a schema that cannot be read leaves out only the catalog record types",
			schemaStatus: http.StatusInternalServerError,
			pages: map[string][]statsPage{
				"TypeY": {yPage},
			},
			wantEntities: &wantEntities{
				total:    10,
				byType:   map[string]float64{"TypeX": 3, "TypeY": 2, "RecordTypeA": 5},
				internal: 3,
			},
			wantSearched:   allTypes,
			wantInsights:   1,
			wantByCategory: map[string]float64{"error": 1},
			wantStderr:     "could not read the graph schema",
		},
		{
			name: "--type limits the insight search but not the entity counts",
			args: []string{"--type", "TypeY"},
			pages: map[string][]statsPage{
				"TypeY": {yPage},
			},
			wantSearched:   []string{"TypeY"},
			wantInsights:   1,
			wantByCategory: map[string]float64{"error": 1},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pinHumanMode(t)
			if tt.maxPages > 0 {
				defer kg.SetStatsMaxSearchPages(tt.maxPages)()
			}
			fake := &statsFake{
				t: t, counts: counts, schema: schema, fromMs: fromMs, toMs: toMs,
				schemaStatus: tt.schemaStatus, pages: tt.pages,
			}
			server := httptest.NewServer(fake)
			defer server.Close()
			loader := &kg.FakeWriteLoader{Cfg: config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}}}
			args := append([]string{"--from", "1757000000", "--to", "1757003600"}, tt.args...)
			out, stderr, err := runKgCommand(t, func() *cobra.Command { return kg.NewSummaryCommand(loader) }, args, "")
			assert.Equal(t, 1, fake.maxConcurrentSearches(), "entity types are searched one at a time")
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			if tt.wantStderr != "" {
				assert.Contains(t, stderr, tt.wantStderr)
			}

			var got statsOutput
			require.NoError(t, json.Unmarshal([]byte(out), &got), out)
			want := withSchema
			if tt.wantEntities != nil {
				want = *tt.wantEntities
			}
			assert.InDelta(t, want.total, got.Entities.Total, 0, "entities.total leaves out the catalog records, whatever --type is")
			assert.Equal(t, want.byType, got.Entities.ByType, "entities.byType holds the types the schema lists for the --from/--to window")
			assert.InDelta(t, want.internal, got.Entities.Internal.Total, 0)
			assert.InDelta(t, tt.wantInsights, got.Insights.Total, 0)
			gotCats := map[string]float64{}
			for k, v := range got.Insights.ByCategory {
				gotCats[k] = v.Total
			}
			assert.Equal(t, tt.wantByCategory, gotCats)
			assert.Equal(t, tt.wantIncomplete, got.Insights.IncompleteTypes)
			assert.Equal(t, tt.wantSearched, fake.searchedTypes())
		})
	}
}
