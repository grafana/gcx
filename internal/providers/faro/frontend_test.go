package faro //nolint:testpackage // Tests unexported builders, parsers, and fetchers.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/query/pinot"
	querysql "github.com/grafana/gcx/internal/query/sql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

// recordingPinot answers by statement name (from the "-- statement" lookup
// below) and records every RawSQL it receives.
type recordingPinot struct {
	mu      sync.Mutex
	sent    []string
	answers map[string]*querysql.QueryResponse
}

func (r *recordingPinot) Query(_ context.Context, _ string, req pinot.QueryRequest) (*querysql.QueryResponse, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, req.RawSQL)
	for marker, resp := range r.answers {
		if strings.Contains(req.RawSQL, marker) {
			return resp, nil
		}
	}
	return &querysql.QueryResponse{}, nil
}

type stubFirstSeen struct {
	records []ErrorFirstSeen
	capped  bool
	get     *ErrorFirstSeen

	gotStopBefore time.Time
}

func (s *stubFirstSeen) ListErrorFirstSeen(_ context.Context, _ string, stopBefore time.Time, _ int) ([]ErrorFirstSeen, bool, error) {
	s.gotStopBefore = stopBefore
	return s.records, s.capped, nil
}

func (s *stubFirstSeen) GetErrorFirstSeen(context.Context, string, string) (*ErrorFirstSeen, error) {
	return s.get, nil
}

func resp(cols []string, rows ...[]any) *querysql.QueryResponse {
	r := &querysql.QueryResponse{}
	for _, c := range cols {
		r.Columns = append(r.Columns, querysql.Column{Name: c})
	}
	r.Rows = rows
	return r
}

func TestNewStatement(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		tmpl    string
		params  []sqlParam
		want    string
		wantErr string
	}{
		{
			name:   "substitutes literals and keeps the time macro",
			tmpl:   `SELECT 1 FROM t WHERE appId = {{APP_ID}} AND h = {{HASH}} AND $__timeFilter("timestamp"){{FILTERS}}`,
			params: []sqlParam{intParam("APP_ID", 187), stringParam("HASH", "42"), rawParam("FILTERS", "\n  AND pageId = '/cart'")},
			want:   "/* gcx test\n   params: app_id=187 hash='42' */\nSELECT 1 FROM t WHERE appId = 187 AND h = '42' AND $__timeFilter(\"timestamp\")\n  AND pageId = '/cart'",
		},
		{
			name:   "escapes quotes in strings",
			tmpl:   `SELECT 1 FROM t WHERE h = {{HASH}}`,
			params: []sqlParam{stringParam("HASH", "a'b")},
			want:   "/* gcx test\n   params: hash='a''b' */\nSELECT 1 FROM t WHERE h = 'a''b'",
		},
		{
			name:   "neutralises comment terminators in the header",
			tmpl:   `SELECT 1 FROM t WHERE p = {{PAGE}}`,
			params: []sqlParam{stringParam("PAGE", "/a*/b")},
			want:   "/* gcx test\n   params: page='/a* /b' */\nSELECT 1 FROM t WHERE p = '/a*/b'",
		},
		{
			name:    "rejects a non-integer app id",
			tmpl:    `SELECT 1 FROM t WHERE appId = {{APP_ID}}`,
			params:  []sqlParam{intStringParam("APP_ID", "66; DROP TABLE x")},
			wantErr: "invalid app_id",
		},
		{
			name:    "rejects unsubstituted placeholders",
			tmpl:    `SELECT 1 FROM t WHERE appId = {{APP_ID}} AND x = {{MISSING}}`,
			params:  []sqlParam{intParam("APP_ID", 1)},
			wantErr: "unsubstituted placeholder {{MISSING}}",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := newStatement("s", "t", "gcx test", tc.tmpl, tc.params)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got.SQL)
			assert.Equal(t, engineSingle, got.Engine)
		})
	}
}

func TestFilterClauses(t *testing.T) {
	t.Parallel()
	f := filterOpts{Page: "/it's", Version: "4.18.0", Environment: "prod"}
	assert.Equal(t, "\n  AND pageId = '/it''s'\n  AND appVersion = '4.18.0'\n  AND appEnvironment = 'prod'", f.clauses())
	assert.Equal(t, []string{"--page", `'/it'\''s'`, "--version", "4.18.0", "--environment", "prod"}, f.flagArgs())
	assert.Empty(t, filterOpts{}.clauses())
}

func TestParseStoredStacktrace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		text         string
		want         []stackFrame
		symbolicated bool
	}{
		{
			name: "source-mapped frames with library frames",
			text: "computeTotal (src/cart/total.ts:48:11)\n  at HTMLButtonElement.onClick (webpack://./src/cart/CartButton.tsx:112:5)\n  at invokeGuardedCallback (node_modules/react-dom/cjs/react-dom.development.js:4213:9)\n  at (<anonymous>:1:1)",
			want: []stackFrame{
				{Function: "computeTotal", File: "src/cart/total.ts", Line: 48, Col: 11, InApp: true},
				{Function: "HTMLButtonElement.onClick", File: "webpack://./src/cart/CartButton.tsx", Path: "src/cart/CartButton.tsx", Line: 112, Col: 5, InApp: true},
				{Function: "invokeGuardedCallback", File: "node_modules/react-dom/cjs/react-dom.development.js", Line: 4213, Col: 9, InApp: false},
				{File: "<anonymous>", Line: 1, Col: 1, InApp: false},
			},
			symbolicated: true,
		},
		{
			name: "minified bundle",
			text: "? (https://example.com/static/main.4f2a1c.js:1:48213)\n  at e (https://example.com/static/main.4f2a1c.js:1:1022)",
			want: []stackFrame{
				{Function: "?", File: "https://example.com/static/main.4f2a1c.js", Line: 1, Col: 48213, InApp: true},
				{Function: "e", File: "https://example.com/static/main.4f2a1c.js", Line: 1, Col: 1022, InApp: true},
			},
			symbolicated: false,
		},
		{
			name: "jvm module|filename location",
			text: "CartActivity.onCreate (com.example.cart|CartActivity.kt:88:0)",
			want: []stackFrame{
				{Function: "CartActivity.onCreate", Module: "com.example.cart", File: "CartActivity.kt", Line: 88, InApp: true},
			},
			symbolicated: true,
		},
		{
			name:         "garbage yields no frames",
			text:         "not a stack trace",
			symbolicated: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := parseStoredStacktrace(tc.text)
			assert.Equal(t, tc.want, got)
			assert.Equal(t, tc.symbolicated, framesSymbolicated(got))
		})
	}
}

func TestRepoPath(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"webpack://grafana/./public/app/features/plugins/extensions/logs/log.ts": "public/app/features/plugins/extensions/logs/log.ts",
		"webpack:///src/a.ts":          "src/a.ts",
		"webpack://./src/a.ts":         "src/a.ts",
		"webpack://app/src/a.ts":       "src/a.ts",
		"src/a.ts":                     "src/a.ts",
		"https://x.com/static/main.js": "https://x.com/static/main.js",
	}
	for in, want := range tests {
		assert.Equal(t, want, repoPath(in), in)
	}
}

func TestOneLine(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "Loading chunk <N> failed. (timeout: <URL>)", oneLine("Loading chunk <N> failed.\n(timeout: <URL>)"))
}

type flakyPinot struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (f *flakyPinot) Query(context.Context, string, pinot.QueryRequest) (*querysql.QueryResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.calls == 1 {
		return nil, f.err
	}
	return &querysql.QueryResponse{}, nil
}

func TestRunStatementsRetriesTransientErrors(t *testing.T) {
	t.Parallel()
	stmt := statement{Name: "s", Table: tableExceptions, SQL: "SELECT 1 FROM faro_pinot_exceptions_v1"}

	transient := &flakyPinot{err: errors.New("Broker request completed with exceptions:\nCode 200: Caught exception while doing operator: FilteredGroupByOperator: Index 8951 out of bounds")}
	_, err := runStatements(t.Context(), transient, "ds", time.Now().Add(-time.Hour), time.Now(), []statement{stmt})
	require.NoError(t, err)
	assert.Equal(t, 2, transient.calls)

	parse := &flakyPinot{err: errors.New("Code 150: SQLParsingError")}
	_, err = runStatements(t.Context(), parse, "ds", time.Now().Add(-time.Hour), time.Now(), []statement{stmt})
	require.ErrorContains(t, err, "s query failed")
	assert.Equal(t, 1, parse.calls)
}

func TestLongestStacktrace(t *testing.T) {
	t.Parallel()
	short := "a (src/a.ts:2:1)"
	long := "a (src/a.ts:2:1)\n  at b (src/b.ts:3:1)"
	assert.Len(t, longestStacktrace([]string{short, long, "junk"}), 2)
	assert.Nil(t, longestStacktrace([]string{"junk"}))
}

func TestTrendDirection(t *testing.T) {
	t.Parallel()
	tests := []struct {
		prev, curr int64
		want       string
	}{
		{100, 121, trendUp},
		{100, 120, trendFlat},
		{100, 80, trendFlat},
		{100, 79, trendDown},
		{0, 0, trendFlat},
		{0, 1, trendUp},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.want, trendDirection(tc.prev, tc.curr), "prev=%d curr=%d", tc.prev, tc.curr)
	}
}

func errorsListFixture() *querysql.QueryResponse {
	cols := []string{"hash", "type", "value_template", "count", "affected_sessions", "affected_pages", "prev_count", "curr_count", "last_seen"}
	return resp(cols,
		[]any{"111", "TypeError", "<NULL_ACCESS:price>", float64(1842), float64(412), float64(3), float64(611), float64(1231), float64(1759917492000)},
		[]any{"222", "Error", "boom", float64(900), float64(800), float64(1), float64(450), float64(450), float64(1759917000000)},
		[]any{"333", "RangeError", "<RANGE>", float64(50), float64(10), float64(1), float64(40), float64(10), float64(1759916000000)},
	)
}

func TestFetchErrorsList(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 10, 7, 10, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	deploy := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	records := []ErrorFirstSeen{
		{ErrorHash: "333", FirstSeenAt: deploy.Add(3 * time.Hour), GitHash: new("9f1c2ab")},
		{ErrorHash: "111", FirstSeenAt: deploy.Add(time.Hour), GitHash: new("abc1234"), BundleID: new("cart-5f2c1a")},
		{ErrorHash: "222", FirstSeenAt: deploy.Add(-48 * time.Hour)},
	}

	tests := []struct {
		name      string
		newSince  time.Time
		sort      string
		limit     int
		records   []ErrorFirstSeen
		wantHash  []string
		wantTotal int
	}{
		{name: "count order", sort: errorsSortCount, limit: 20, records: records, wantHash: []string{"111", "222", "333"}, wantTotal: 3},
		{name: "sessions order", sort: errorsSortSessions, limit: 20, records: records, wantHash: []string{"222", "111", "333"}, wantTotal: 3},
		{name: "newest first", sort: errorsSortNewest, limit: 20, records: records, wantHash: []string{"333", "111", "222"}, wantTotal: 3},
		{name: "newest puts unknown first-seen last", sort: errorsSortNewest, limit: 20, records: records[1:2], wantHash: []string{"111", "222", "333"}, wantTotal: 3},
		{name: "new-since drops older and unknown groups", newSince: deploy, sort: errorsSortCount, limit: 20, records: records, wantHash: []string{"111", "333"}, wantTotal: 2},
		{name: "limit truncates but total counts all", sort: errorsSortCount, limit: 1, records: records, wantHash: []string{"111"}, wantTotal: 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stmt, err := buildErrorsListStatement(187, start, end, filterOpts{Page: "/cart"}, "gcx frontend errors list --app 187")
			require.NoError(t, err)
			pq := &recordingPinot{answers: map[string]*querysql.QueryResponse{"FROM faro_pinot_exceptions_v1": errorsListFixture()}}
			fs := &stubFirstSeen{records: tc.records}

			got, err := fetchErrorsList(t.Context(), pq, fs, errorsListInput{
				app: frontendApp{ID: 187}, dsUID: "ds", start: start, end: end, stmt: stmt,
				newSince: tc.newSince, sort: tc.sort, limit: tc.limit,
			})
			require.NoError(t, err)

			hashes := make([]string, len(got.result.Errors))
			for i, g := range got.result.Errors {
				hashes[i] = g.Hash
			}
			assert.Equal(t, tc.wantHash, hashes)
			assert.Equal(t, tc.wantTotal, got.result.TotalGroups)
			assert.Equal(t, tc.newSince, fs.gotStopBefore)

			// The SQL that ran is exactly the statement --sql prints.
			require.Len(t, pq.sent, 1)
			assert.Equal(t, stmt.SQL, pq.sent[0])
			assert.Contains(t, pq.sent[0], "AND pageId = '/cart'")
			assert.Contains(t, pq.sent[0], `$__timeFilter("timestamp")`)
			assert.Contains(t, pq.sent[0], "exceptionHash <> '0'")
		})
	}
}

func TestFetchErrorsListJoinsFirstSeen(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 10, 7, 10, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	stmt, err := buildErrorsListStatement(187, start, end, filterOpts{}, "gcx")
	require.NoError(t, err)
	assert.Contains(t, stmt.SQL, `"timestamp" < `+strconv.FormatInt(start.Add(12*time.Hour).UnixMilli(), 10), "midpoint of the range in epoch ms")

	at := time.Date(2026, 10, 7, 14, 2, 31, 0, time.UTC)
	pq := &recordingPinot{answers: map[string]*querysql.QueryResponse{"FROM": errorsListFixture()}}
	fs := &stubFirstSeen{records: []ErrorFirstSeen{{ErrorHash: "111", FirstSeenAt: at, GitHash: new("9f1c2ab"), BundleID: new("cart-5f2c1a")}}}
	got, err := fetchErrorsList(t.Context(), pq, fs, errorsListInput{app: frontendApp{ID: 187}, start: start, end: end, stmt: stmt, sort: errorsSortCount, limit: 20})
	require.NoError(t, err)

	top := got.result.Errors[0]
	assert.Equal(t, "TypeError", top.Type)
	assert.Equal(t, "<NULL_ACCESS:price>", top.Template)
	assert.Equal(t, int64(1842), top.Count)
	assert.Equal(t, errorTrend{Prev: 611, Curr: 1231, Direction: trendUp}, top.Trend)
	require.NotNil(t, top.FirstSeen)
	assert.Equal(t, at, top.FirstSeen.At)
	assert.Equal(t, "9f1c2ab", *top.FirstSeen.GitHash)
	assert.Nil(t, got.result.Errors[1].FirstSeen)
}

func TestFetchErrorDetail(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 10, 7, 10, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	stmts, err := buildErrorsGetStatements(187, "111", start, end, 3, "gcx frontend errors get 111 --app 187")
	require.NoError(t, err)
	assert.Equal(t, int64(2_880_000), stmts.bucketMS)

	bucket := start.UnixMilli() / stmts.bucketMS * stmts.bucketMS
	pq := &recordingPinot{answers: map[string]*querysql.QueryResponse{
		"AS total_events": resp([]string{"type", "value_template", "total_events", "affected_users", "affected_pages", "affected_sessions", "first_seen_in_range", "last_seen"},
			[]any{"TypeError", "<NULL_ACCESS:price>", float64(1842), float64(388), float64(3), float64(412), float64(start.Add(time.Hour).UnixMilli()), float64(end.Add(-time.Minute).UnixMilli())}),
		"DATETIMECONVERT": resp([]string{"time", "cnt"},
			[]any{float64(bucket + 2*stmts.bucketMS), float64(3)},
			[]any{float64(bucket + 3*stmts.bucketMS), float64(41)}),
		"exceptionStacktrace AS stacktrace": resp([]string{"stacktrace"},
			[]any{"computeTotal (src/cart/total.ts:48:11)"},
			[]any{"computeTotal (src/cart/total.ts:48:11)\n  at onClick (src/cart/CartButton.tsx:112:5)"}),
		"exceptionValue AS value": resp([]string{"value", "cnt"}, []any{"Cannot read properties of undefined (reading 'price')", float64(1800)}),
		"appVersion AS value":     resp([]string{"value", "cnt"}, []any{"4.18.0", float64(1839)}, []any{"4.17.2", float64(3)}),
		"browserName AS value":    resp([]string{"value", "cnt"}, []any{"Chrome", float64(1500)}),
		"pageId AS value":         resp([]string{"value", "cnt"}, []any{"/cart", float64(1811)}),
		"osName AS value":         resp([]string{"value", "cnt"}, []any{"Mac OS", float64(900)}),
		"sessionId AS session_id": resp([]string{"timestamp", "session_id", "user_id", "page_id", "app_version", "browser_name"},
			[]any{float64(end.Add(-time.Minute).UnixMilli()), "k3Rt9wQp2Z", "u-4821", "/cart", "4.18.0", "Chrome"}),
	}}
	fs := &stubFirstSeen{get: &ErrorFirstSeen{ErrorHash: "111", FirstSeenAt: start, GitHash: new("9f1c2ab")}}

	d, err := fetchErrorDetail(t.Context(), pq, fs, errorsGetInput{app: frontendApp{ID: 187}, dsUID: "ds", start: start, end: end, hash: "111", stmts: stmts})
	require.NoError(t, err)

	assert.Equal(t, errorCounts{Events: 1842, Sessions: 412, Users: 388, Pages: 3}, d.Counts)
	assert.Equal(t, []string{"Cannot read properties of undefined (reading 'price')"}, d.ExampleValues)
	require.NotNil(t, d.FirstSeen)
	assert.Equal(t, "9f1c2ab", *d.FirstSeen.GitHash)
	assert.True(t, d.Stacktrace.Symbolicated)
	assert.Len(t, d.Stacktrace.Frames, 2)
	assert.Equal(t, "src/cart/total.ts", d.Stacktrace.Frames[0].File)
	assert.Empty(t, d.Stacktrace.Hint)
	assert.Equal(t, []valueCount{{"4.18.0", 1839}, {"4.17.2", 3}}, d.Breakdown.AppVersion)
	assert.Equal(t, []valueCount{{"/cart", 1811}}, d.Breakdown.Page)
	require.Len(t, d.Instances, 1)
	assert.Equal(t, "k3Rt9wQp2Z", d.Instances[0].SessionID)
	assert.Len(t, d.Sparkline.Counts, 31)
	assert.Equal(t, []int64{0, 0, 3, 41, 0}, d.Sparkline.Counts[:5])

	// Every executed statement is one of the statements --sql prints.
	all := stmts.all()
	require.Len(t, pq.sent, len(all))
	for _, s := range all {
		assert.Contains(t, pq.sent, s.SQL)
		assert.Contains(t, s.SQL, "exceptionHash = '111'")
	}
}

func TestFetchErrorDetailNotFoundAndUnsymbolicated(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 10, 7, 10, 0, 0, 0, time.UTC)
	end := start.Add(10 * time.Minute)
	stmts, err := buildErrorsGetStatements(187, "111", start, end, 3, "gcx")
	require.NoError(t, err)
	assert.Equal(t, int64(sparklineMinBucketMS), stmts.bucketMS, "short ranges floor the bucket at 30s")

	_, err = fetchErrorDetail(t.Context(), &recordingPinot{}, &stubFirstSeen{}, errorsGetInput{app: frontendApp{ID: 187}, start: start, end: end, hash: "111", stmts: stmts})
	require.ErrorIs(t, err, errErrorNotFound)

	pq := &recordingPinot{answers: map[string]*querysql.QueryResponse{
		"AS total_events":                   resp([]string{"total_events"}, []any{float64(5)}),
		"exceptionStacktrace AS stacktrace": resp([]string{"stacktrace"}, []any{"? (https://x/main.js:1:400)"}),
	}}
	d, err := fetchErrorDetail(t.Context(), pq, &stubFirstSeen{}, errorsGetInput{app: frontendApp{ID: 187}, start: start, end: end, hash: "111", stmts: stmts})
	require.NoError(t, err)
	assert.Nil(t, d.FirstSeen)
	assert.False(t, d.Stacktrace.Symbolicated)
	assert.Contains(t, d.Stacktrace.Hint, "gcx frontend apps list-sourcemaps 187")
}

func TestVitalRating(t *testing.T) {
	t.Parallel()
	tests := []struct {
		vital string
		p75   float64
		want  string
	}{
		{vitalLCP, 2500, ratingGood},
		{vitalLCP, 3100, ratingNeedsImprovement},
		{vitalLCP, 4000, ratingNeedsImprovement},
		{vitalLCP, 4001, ratingPoor},
		{vitalINP, 180, ratingGood},
		{vitalCLS, 0.1, ratingGood},
		{vitalCLS, 0.31, ratingPoor},
		{vitalFCP, 1800.5, ratingNeedsImprovement},
		{vitalTTFB, 1801, ratingPoor},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.want, vitalRating(tc.vital, tc.p75), "%s=%v", tc.vital, tc.p75)
	}
}

func TestFetchPagesList(t *testing.T) {
	t.Parallel()
	vitalsCols := []string{"page_id", "page_loads", "ttfb_p75", "fcp_p75", "lcp_p75", "cls_p75", "inp_p75"}
	pq := &recordingPinot{answers: map[string]*querysql.QueryResponse{
		"FROM faro_pinot_measurements_v1": resp(vitalsCols,
			[]any{"/cart", float64(18422), float64(610), float64(1420), float64(3120.4), float64(0.3104), float64(180)},
			[]any{"/home", float64(30000), float64(400), float64(900), float64(1800), float64(0.01), nil},
			[]any{"/settings", float64(10), nil, nil, nil, nil, nil}),
		"FROM faro_pinot_exceptions_v1": resp([]string{"page_id", "errors"},
			[]any{"/cart", float64(1842)},
			[]any{"/orphan", float64(7)}),
	}}
	stmts, err := buildPagesStatements(187, filterOpts{Version: "4.18.0"}, "gcx")
	require.NoError(t, err)

	got, err := fetchPagesList(t.Context(), pq, frontendApp{ID: 187}, "ds", time.Now().Add(-time.Hour), time.Now(), stmts, vitalLCP, 20)
	require.NoError(t, err)
	assert.Equal(t, 4, got.TotalPages)
	pages := make([]string, len(got.Pages))
	for i, p := range got.Pages {
		pages[i] = p.Page
	}
	// LCP descending, pages with no LCP last (ordered by loads).
	assert.Equal(t, []string{"/cart", "/home", "/settings", "/orphan"}, pages)

	cart := got.Pages[0]
	assert.Equal(t, int64(1842), cart.Errors)
	assert.Equal(t, ratingNeedsImprovement, *cart.Vitals.LCP.Rating)
	assert.InDelta(t, 3120, *cart.Vitals.LCP.P75MS, 0)
	assert.InDelta(t, 0.31, *cart.Vitals.CLS.P75, 0.0001)
	assert.Nil(t, cart.Vitals.CLS.P75MS)
	assert.Equal(t, ratingPoor, *cart.Vitals.CLS.Rating)
	assert.Nil(t, got.Pages[1].Vitals.INP.Rating)

	byErrors, err := fetchPagesList(t.Context(), pq, frontendApp{ID: 187}, "ds", time.Now().Add(-time.Hour), time.Now(), stmts, pagesSortErrors, 2)
	require.NoError(t, err)
	assert.Equal(t, "/cart", byErrors.Pages[0].Page)
	assert.Equal(t, "/orphan", byErrors.Pages[1].Page)
	assert.Equal(t, 4, byErrors.TotalPages)

	for _, s := range []statement{stmts.vitals, stmts.errors} {
		assert.Contains(t, pq.sent, s.SQL)
		assert.Contains(t, s.SQL, "AND appVersion = '4.18.0'")
		assert.Contains(t, s.SQL, `$__timeFilter("timestamp_30s")`)
	}
	assert.Contains(t, stmts.vitals.SQL, `PERCENTILETDIGEST("measurementValues.lcp", 75)`)
}

func TestFrontendQueryGuards(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		sql     string
		table   string
		appID   int64
		wantErr string
	}{
		{name: "allowed table with app filter", sql: `SELECT count(*) FROM faro_pinot_exceptions_v1 WHERE appId = 187`, appID: 187},
		{name: "whitespace and quoted identifier", sql: `SELECT 1 FROM faro_pinot_events_v2 WHERE "appId"=187 AND x = 1`, appID: 187},
		{name: "rejects other tables", sql: `SELECT 1 FROM other_table WHERE appId = 187`, appID: 187, wantErr: `table "other_table" is not a Frontend Observability table`},
		{name: "rejects --table outside allow list", sql: `SELECT 1`, table: "other", appID: 187, wantErr: "not a Frontend Observability table"},
		{name: "missing app filter", sql: `SELECT 1 FROM faro_pinot_exceptions_v1 WHERE appId = 1870`, appID: 187, wantErr: "query must filter on appId = 187; add it to the WHERE clause"},
		{name: "different column suffix does not count", sql: `SELECT 1 FROM faro_pinot_exceptions_v1 WHERE xappId = 187`, appID: 187, wantErr: "query must filter on appId = 187"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := checkFrontendQueryTable(tc.sql, tc.table)
			if err == nil {
				err = checkAppIDPredicate(tc.sql, tc.appID)
			}
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestBrokerErrorHint(t *testing.T) {
	t.Parallel()
	assert.Contains(t, brokerErrorHint("numGroupsLimit reached", true), "narrow --since")
	assert.Contains(t, brokerErrorHint("JOIN is not supported in single-stage", true), "--multistage")
	assert.Empty(t, brokerErrorHint("JOIN is not supported in single-stage", false), "already hinted")
	assert.Contains(t, brokerErrorHint("Unknown column 'foo'", true), "column name")
	assert.Empty(t, brokerErrorHint("something else", true))
}

func TestValidateFrontendRange(t *testing.T) {
	t.Parallel()
	now := time.Now()
	require.NoError(t, validateFrontendRange(now.Add(-30*24*time.Hour), now))
	err := validateFrontendRange(now.Add(-45*24*time.Hour), now)
	require.ErrorContains(t, err, "30-day retention")
}

func TestFrontendOptsDefaultsTo24h(t *testing.T) {
	t.Parallel()
	o := &frontendOpts{App: "187", appRequired: true}
	o.IO.OutputFormat = "json"
	require.NoError(t, o.Validate())
	assert.Equal(t, frontendDefaultSince, o.Since)

	assert.Equal(t, []string{"--since", "24h"}, o.sinceArg())

	withTo := &frontendOpts{App: "187", TimeRangeOpts: dsquery.TimeRangeOpts{Since: "7d", To: "2026-10-08T09:00:00Z"}}
	withTo.IO.OutputFormat = "json"
	require.NoError(t, withTo.Validate())
	assert.Equal(t, []string{"--since", "7d", "--to", "2026-10-08T09:00:00Z"}, withTo.sinceArg())

	missing := &frontendOpts{appRequired: true}
	missing.IO.OutputFormat = "json"
	require.ErrorContains(t, missing.Validate(), "--app is required")

	tooLong := &frontendOpts{App: "187", appRequired: true, TimeRangeOpts: dsquery.TimeRangeOpts{Since: "45d"}}
	tooLong.IO.OutputFormat = "json"
	require.ErrorContains(t, tooLong.Validate(), "30-day retention")
}

type stubApps struct {
	byID   map[string]*FaroApp
	byName map[string]*FaroApp
}

func (s stubApps) Get(_ context.Context, id string) (*FaroApp, error) {
	if a, ok := s.byID[id]; ok {
		return a, nil
	}
	return nil, errors.New("not found")
}

func (s stubApps) GetByName(_ context.Context, name string) (*FaroApp, error) {
	if a, ok := s.byName[name]; ok {
		return a, nil
	}
	return nil, errors.New("not found by name")
}

func TestResolveFrontendApp(t *testing.T) {
	t.Parallel()
	web := &FaroApp{ID: "187", Name: "checkout-web"}
	v2 := &FaroApp{ID: "9", Name: "checkout-v-2"}
	apps := stubApps{byID: map[string]*FaroApp{"187": web}, byName: map[string]*FaroApp{"checkout-web": web, "checkout-v-2": v2}}

	tests := []struct {
		ref     string
		want    frontendApp
		wantErr bool
	}{
		{ref: "187", want: frontendApp{ID: 187, Name: "checkout-web"}},
		{ref: "checkout-web-187", want: frontendApp{ID: 187, Name: "checkout-web"}},
		{ref: "checkout-web", want: frontendApp{ID: 187, Name: "checkout-web"}},
		{ref: "checkout-v-2", want: frontendApp{ID: 9, Name: "checkout-v-2"}},
		{ref: "999", wantErr: true},
	}
	for _, tc := range tests {
		got, err := resolveFrontendApp(t.Context(), apps, tc.ref)
		if tc.wantErr {
			require.Error(t, err, tc.ref)
			continue
		}
		require.NoError(t, err, tc.ref)
		assert.Equal(t, tc.want, got, tc.ref)
	}
}

func TestClientErrorFirstSeen(t *testing.T) {
	t.Parallel()

	newest := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	var offsets []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/plugin-proxy/grafana-kowalski-app/api-proxy/api/v1/app/187/errors":
			offset := r.URL.Query().Get("offset")
			offsets = append(offsets, offset)
			assert.Equal(t, "2000", r.URL.Query().Get("limit"))
			w.WriteHeader(http.StatusPartialContent)
			switch offset {
			case "0":
				_, _ = w.Write([]byte(`{"errors":[{"appId":187,"errorHash":"13273781603667425932","firstSeenAt":"` + newest.Format(time.RFC3339) + `","gitHash":"9f1c2ab","bundleId":null}],"page":{"hasNext":true,"limit":2000,"next":"2000"}}`))
			case "2000":
				_, _ = w.Write([]byte(`{"errors":[{"appId":187,"errorHash":"2","firstSeenAt":"` + newest.Add(-72*time.Hour).Format(time.RFC3339) + `"}],"page":{"hasNext":true,"limit":2000,"next":"4000"}}`))
			default:
				_, _ = w.Write([]byte(`{"errors":[{"appId":187,"errorHash":"3","firstSeenAt":"` + newest.Add(-96*time.Hour).Format(time.RFC3339) + `"}],"page":{"hasNext":false,"limit":2000,"next":""}}`))
			}
		case "/api/plugin-proxy/grafana-kowalski-app/api-proxy/api/v1/app/187/errors/42":
			_, _ = w.Write([]byte(`{"appId":187,"errorHash":"42","firstSeenAt":"2026-10-07T14:02:31.512Z","gitHash":"9f1c2ab","bundleId":"cart-5f2c1a"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"error not found"}`))
		}
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(config.NamespacedRESTConfig{Config: rest.Config{Host: srv.URL}})
	require.NoError(t, err)

	all, capped, err := c.ListErrorFirstSeen(t.Context(), "187", time.Time{}, 0)
	require.NoError(t, err)
	assert.False(t, capped)
	require.Len(t, all, 3)
	assert.Equal(t, "13273781603667425932", all[0].ErrorHash)
	assert.Equal(t, "9f1c2ab", *all[0].GitHash)
	assert.Nil(t, all[0].BundleID)
	assert.Equal(t, []string{"0", "2000", "4000"}, offsets)

	offsets = nil
	early, _, err := c.ListErrorFirstSeen(t.Context(), "187", newest.Add(-24*time.Hour), 0)
	require.NoError(t, err)
	assert.Len(t, early, 2, "stops after the first page whose last record is older than stopBefore")
	assert.Equal(t, []string{"0", "2000"}, offsets)

	offsets = nil
	one, capped, err := c.ListErrorFirstSeen(t.Context(), "187", time.Time{}, 1)
	require.NoError(t, err)
	assert.True(t, capped)
	assert.Len(t, one, 1)

	rec, err := c.GetErrorFirstSeen(t.Context(), "187", "42")
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, "cart-5f2c1a", *rec.BundleID)

	missing, err := c.GetErrorFirstSeen(t.Context(), "187", "7")
	require.NoError(t, err)
	assert.Nil(t, missing)
}
