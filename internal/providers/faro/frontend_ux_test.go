package faro //nolint:testpackage // Tests unexported error builders and output shaping.

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/gcxerrors"
	querysql "github.com/grafana/gcx/internal/query/sql"
	"github.com/grafana/gcx/internal/queryerror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func detailed(t *testing.T, err error) *gcxerrors.DetailedError {
	t.Helper()
	var d *gcxerrors.DetailedError
	require.ErrorAs(t, err, &d)
	return d
}

func TestFetchErrorsListReportsGroupCap(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 10, 7, 10, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	stmt, err := buildErrorsListStatement(187, start, end, filterOpts{}, "gcx")
	require.NoError(t, err)
	assert.Contains(t, stmt.SQL, "LIMIT "+strconv.Itoa(errorsListGroupLimit))

	cols := []string{"hash", "type", "value_template", "count", "affected_sessions", "affected_pages", "prev_count", "curr_count", "last_seen"}
	capped := &querysql.QueryResponse{}
	for _, c := range cols {
		capped.Columns = append(capped.Columns, querysql.Column{Name: c})
	}
	for i := range errorsListGroupLimit {
		capped.Rows = append(capped.Rows, []any{strconv.Itoa(i + 1), "Error", "t", float64(1), float64(1), float64(1), float64(0), float64(1), float64(0)})
	}

	tests := []struct {
		name       string
		resp       *querysql.QueryResponse
		wantCapped bool
	}{
		{name: "below the cap", resp: errorsListFixture(), wantCapped: false},
		{name: "at the cap", resp: capped, wantCapped: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pq := &recordingPinot{answers: map[string]*querysql.QueryResponse{"FROM": tc.resp}}
			got, err := fetchErrorsList(t.Context(), pq, &stubFirstSeen{}, errorsListInput{
				app: frontendApp{ID: 187}, start: start, end: end, stmt: stmt, sort: errorsSortCount, limit: 20,
			})
			require.NoError(t, err)
			assert.Equal(t, tc.wantCapped, got.result.GroupsCapped)
			if tc.wantCapped {
				assert.Equal(t, errorsListGroupLimit, got.result.TotalGroups)
				assert.Len(t, got.result.Errors, 20)
			}
		})
	}
}

func TestErrorNotFoundError(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 10, 7, 10, 0, 0, 0, time.UTC)
	seen := time.Date(2026, 9, 22, 18, 40, 17, 0, time.UTC)
	in := func(span time.Duration) errorsGetInput {
		return errorsGetInput{app: frontendApp{ID: 67}, hash: "123", start: start, end: start.Add(span)}
	}

	tests := []struct {
		name            string
		in              errorsGetInput
		firstSeen       *ErrorFirstSeen
		wantDetails     string
		wantSuggestions []string
	}{
		{
			name:        "known group outside a 24h window",
			in:          in(24 * time.Hour),
			firstSeen:   &ErrorFirstSeen{ErrorHash: "123", FirstSeenAt: seen},
			wantDetails: "first seen 2026-09-22T18:40:17Z",
			wantSuggestions: []string{
				"Widen the window: gcx frontend errors get 123 --app 67 --since 7d",
				"Pick a current group: gcx frontend errors list --app 67",
			},
		},
		{
			name:            "known group outside a week",
			in:              in(7 * 24 * time.Hour),
			firstSeen:       &ErrorFirstSeen{ErrorHash: "123", FirstSeenAt: seen},
			wantDetails:     "first seen",
			wantSuggestions: []string{"Widen the window: gcx frontend errors get 123 --app 67 --since 30d", "Pick a current group: gcx frontend errors list --app 67"},
		},
		{
			name:            "known group with the full retention already searched",
			in:              in(frontendMaxRange),
			firstSeen:       &ErrorFirstSeen{ErrorHash: "123", FirstSeenAt: seen},
			wantDetails:     "first seen",
			wantSuggestions: []string{"Pick a current group: gcx frontend errors list --app 67"},
		},
		{
			name:            "unknown hash",
			in:              in(24 * time.Hour),
			wantDetails:     "no first-seen record for this hash either; check that the hash belongs to app 67",
			wantSuggestions: []string{"Pick a current group: gcx frontend errors list --app 67"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := errorNotFoundError(tc.in, tc.firstSeen)
			require.ErrorIs(t, err, errErrorNotFound)
			d := detailed(t, err)
			assert.Equal(t, "Error group 123 has no events between 2026-10-07T10:00:00Z and "+tc.in.end.Format(time.RFC3339), d.Summary)
			assert.Contains(t, d.Details, tc.wantDetails)
			assert.Equal(t, tc.wantSuggestions, d.Suggestions)
		})
	}
}

func TestAppErrorsCarrySuggestions(t *testing.T) {
	t.Parallel()

	o := &frontendOpts{appRequired: true}
	o.IO.OutputFormat = "json"
	d := detailed(t, o.Validate())
	assert.Equal(t, "--app is required", d.Summary)
	assert.Equal(t, []string{appsListSuggestion}, d.Suggestions)
	require.NotNil(t, d.ExitCode)
	assert.Equal(t, gcxerrors.ExitUsageError, *d.ExitCode)

	apps := stubApps{byID: map[string]*FaroApp{}, byName: map[string]*FaroApp{}}
	_, err := resolveFrontendApp(t.Context(), apps, "no-such-app")
	d = detailed(t, err)
	assert.Equal(t, `Frontend Observability app "no-such-app" not found`, d.Summary)
	assert.Equal(t, "not found by name", d.Details)
	assert.Equal(t, []string{appsListSuggestion}, d.Suggestions)

	_, err = resolveFrontendApp(t.Context(), apps, "999999")
	d = detailed(t, err)
	assert.Equal(t, `Frontend Observability app "999999" not found`, d.Summary)
	assert.Equal(t, "not found", d.Details, "numeric IDs are not retried as names")
	assert.Equal(t, []string{appsListSuggestion}, d.Suggestions)
}

func TestPageWithMostErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		pages  []pageRow
		want   string
		wantOK bool
	}{
		{name: "no pages"},
		{name: "no errors anywhere", pages: []pageRow{{Page: "/a"}, {Page: "/b"}}},
		{name: "picks the page with most errors, not the first", pages: []pageRow{{Page: "/a", Errors: 2}, {Page: "/b", Errors: 9}, {Page: "/c", Errors: 9}}, want: "/b", wantOK: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := pageWithMostErrors(tc.pages)
			assert.Equal(t, tc.wantOK, ok)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestVitalValueJSON(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		v    vitalValue
		want string
	}{
		{name: "timing present", v: newVitalValue(vitalLCP, 3120.4, true), want: `{"p75_ms":3120,"rating":"needs-improvement"}`},
		{name: "timing missing keeps the key", v: newVitalValue(vitalFCP, 0, false), want: `{"p75_ms":null,"rating":null}`},
		{name: "cls present", v: newVitalValue(vitalCLS, 0.3104, true), want: `{"p75":0.31,"rating":"poor"}`},
		{name: "cls missing keeps the key", v: newVitalValue(vitalCLS, 0, false), want: `{"p75":null,"rating":null}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			b, err := json.Marshal(tc.v)
			require.NoError(t, err)
			assert.JSONEq(t, tc.want, string(b))
		})
	}

	// The vitals object keeps every key so --json list and jq paths are stable.
	b, err := json.Marshal(pageVitals{LCP: newVitalValue(vitalLCP, 100, true), CLS: newVitalValue(vitalCLS, 0, false)})
	require.NoError(t, err)
	assert.JSONEq(t, `{"lcp":{"p75_ms":100,"rating":"good"},"inp":{"p75_ms":null,"rating":null},"cls":{"p75":null,"rating":null},"fcp":{"p75_ms":null,"rating":null},"ttfb":{"p75_ms":null,"rating":null}}`, string(b))
}

func TestHasTimeFilter(t *testing.T) {
	t.Parallel()
	tests := []struct {
		sql  string
		want bool
	}{
		{`SELECT 1 FROM t WHERE appId = 1 AND $__timeFilter("timestamp")`, true},
		{`SELECT 1 FROM t WHERE $__TIMEFILTER ("timestamp_30s")`, true},
		{`SELECT 1 FROM t WHERE "timestamp" > ago('PT24H')`, true},
		{`SELECT 1 FROM t WHERE appId = 1`, false},
		{`SELECT pagoda FROM t`, false},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.want, hasTimeFilter(tc.sql), tc.sql)
	}
}

func TestQueryFailureError(t *testing.T) {
	t.Parallel()

	plain := errors.New("boom")
	d := detailed(t, queryFailureError(plain, "Check the column name"))
	assert.Equal(t, "Boom", d.Summary, "the converter's rendering is kept")
	assert.Equal(t, []string{"Check the column name"}, d.Suggestions)
	require.ErrorIs(t, d, plain)

	apiErr := queryerror.New("pinot", "query", http.StatusBadRequest, "Code 710: UnknownColumnError: Unknown columnName 'nosuchcol'", "")
	d = detailed(t, queryFailureError(apiErr, brokerErrorHint(apiErr.Error(), true)))
	assert.Contains(t, d.Summary, "Pinot")
	assert.Contains(t, d.Details, "nosuchcol")
	require.NotEmpty(t, d.Suggestions)
	assert.Contains(t, d.Suggestions[len(d.Suggestions)-1], "Check the column name")

	d = detailed(t, queryFailureError(apiErr, ""))
	for _, s := range d.Suggestions {
		assert.NotContains(t, s, "column name")
	}
}
