package metrics_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/grafana/gcx/internal/providers"
	"github.com/stretchr/testify/require"
)

const (
	fakeGrafanaToken = "test-grafana-token"
	fakeCloudToken   = "test-cloud-token"
	fakeTenantID     = 42

	viaProxy  = "proxy"
	viaDirect = "direct"

	proxyRoot  = "/api/plugin-proxy/grafana-adaptive-metrics-app"
	directRoot = "/adaptive-metrics/api/v2"
)

type wireLabelPolicy struct {
	Mode   string   `json:"mode"`
	Labels []string `json:"labels"`
}

type wireRule struct {
	Metric      string           `json:"metric"`
	MatchType   string           `json:"match_type,omitempty"`
	Action      string           `json:"action"`
	Functions   []string         `json:"functions,omitempty"`
	Interval    string           `json:"interval,omitempty"`
	Delay       string           `json:"delay,omitempty"`
	LabelPolicy *wireLabelPolicy `json:"label_policy,omitempty"`
}

type fakeReq struct {
	Via    string
	Method string
	Path   string
	Query  url.Values
	Auth   string
	Body   string
	Status int
}

type fakeFailure struct {
	Via    string
	Method string
	Status int
	Body   string
}

// fakeRules is an in-memory Adaptive Metrics v2 rules API reachable through
// both the Grafana plugin proxy and the direct endpoint, plus the GCOM stack
// lookup the direct path needs. Every request is recorded.
type fakeRules struct {
	t   *testing.T
	srv *httptest.Server

	mu       sync.Mutex
	rules    map[string]map[string]wireRule
	recs     map[string]map[string]fakeRec
	pageSize int
	failures []fakeFailure
	reqs     []fakeReq
	gcom     int
}

// fakeRec is a catalog entry: the recommended action and, for add and update,
// the recommended rule.
type fakeRec struct {
	Action             string
	Rule               *wireRule
	CurrentSeries      int
	RecommendedSeries  int
	RawSeries          int
	CurrentReduction   int
	ProjectedChange    int
	StatsUpdatedAt     string
	UsagesInRules      int
	UsagesInQueries    int
	UsagesInDashboards int
}

func newFakeRules(t *testing.T) *fakeRules {
	t.Helper()
	f := &fakeRules{t: t, rules: map[string]map[string]wireRule{"default": {}}, recs: map[string]map[string]fakeRec{}}
	f.srv = httptest.NewServer(f)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeRules) seed(segment string, rules ...wireRule) *fakeRules {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.rules[segment] == nil {
		f.rules[segment] = map[string]wireRule{}
	}
	for _, r := range rules {
		f.rules[segment][r.Metric] = r
	}
	return f
}

func (f *fakeRules) recommend(segment, metric string, rec fakeRec) *fakeRules {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.rules[segment] == nil {
		f.rules[segment] = map[string]wireRule{}
	}
	if f.recs[segment] == nil {
		f.recs[segment] = map[string]fakeRec{}
	}
	f.recs[segment][metric] = rec
	return f
}

// fail makes matching requests fail with status and body. A zero status drops
// the connection without a response.
func (f *fakeRules) fail(via, method string, status int, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failures = append(f.failures, fakeFailure{Via: via, Method: method, Status: status, Body: body})
}

func (f *fakeRules) stored(segment, metric string) (wireRule, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rules[segment][metric]
	return r, ok
}

func (f *fakeRules) metrics(segment string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.rules[segment]))
	for m := range f.rules[segment] {
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}

// requests returns recorded requests, optionally filtered by transport and
// method, in arrival order.
func (f *fakeRules) requests(via, method string) []fakeReq {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []fakeReq
	for _, r := range f.reqs {
		if (via == "" || r.Via == via) && (method == "" || r.Method == method) {
			out = append(out, r)
		}
	}
	return out
}

// applied returns requests that reached the rules handlers and succeeded.
func (f *fakeRules) applied(via, method string) []fakeReq {
	var out []fakeReq
	for _, r := range f.requests(via, method) {
		if r.Status < 400 {
			out = append(out, r)
		}
	}
	return out
}

func (f *fakeRules) gcomCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.gcom
}

type fakeConfig struct {
	grafana bool
	cloud   bool
}

func (f *fakeRules) loader(t *testing.T, c fakeConfig) *providers.ConfigLoader {
	t.Helper()
	var b strings.Builder
	b.WriteString("contexts:\n  default:\n")
	if c.grafana {
		fmt.Fprintf(&b, "    grafana:\n      server: %s\n      token: %s\n      org-id: 1\n", f.srv.URL, fakeGrafanaToken)
	}
	if c.cloud {
		fmt.Fprintf(&b, "    cloud:\n      token: %s\n      stack: teststack\n      api-url: %s\n", fakeCloudToken, f.srv.URL)
	}
	b.WriteString("current-context: default\n")
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(b.String()), 0o600))
	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(path)
	return loader
}

type statusRecorder struct {
	http.ResponseWriter

	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (f *fakeRules) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.EscapedPath()
	if strings.HasPrefix(path, "/api/instances/") {
		f.mu.Lock()
		f.gcom++
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"id":1,"hmInstancePromId":%d,"hmInstancePromUrl":%q}`, fakeTenantID, f.srv.URL)
		return
	}

	via, rest := "", ""
	switch {
	case strings.HasPrefix(path, proxyRoot+"/"):
		via, rest = viaProxy, strings.TrimPrefix(path, proxyRoot)
	case strings.HasPrefix(path, directRoot+"/"):
		via, rest = viaDirect, "/v2"+strings.TrimPrefix(path, directRoot)
	default:
		http.NotFound(w, r)
		return
	}

	raw, _ := io.ReadAll(r.Body)
	body := string(raw)
	rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
	defer func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.reqs = append(f.reqs, fakeReq{Via: via, Method: r.Method, Path: rest, Query: r.URL.Query(), Auth: r.Header.Get("Authorization"), Body: body, Status: rec.status})
	}()
	orig := w
	w = rec

	if fl, ok := f.failureFor(via, r.Method); ok {
		if fl.Status == 0 {
			conn, _, err := http.NewResponseController(orig).Hijack()
			if err != nil {
				f.t.Errorf("drop connection: %v", err)
				return
			}
			_ = conn.Close()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(fl.Status)
		fmt.Fprint(w, fl.Body)
		return
	}

	if !f.authorized(via, r) {
		writeRaw(w, http.StatusUnauthorized, `{"error":"unauthorized"}`)
		return
	}

	switch {
	case strings.HasPrefix(rest, "/v2/segments/"):
		f.segmentRoute(w, r, strings.TrimPrefix(rest, "/v2/segments/"), body)
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeRules) failureFor(via, method string) (fakeFailure, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, fl := range f.failures {
		if fl.Via == via && (fl.Method == "" || fl.Method == method) {
			return fl, true
		}
	}
	return fakeFailure{}, false
}

func (f *fakeRules) authorized(via string, r *http.Request) bool {
	switch via {
	case viaProxy:
		return r.Header.Get("Authorization") == "Bearer "+fakeGrafanaToken
	default:
		user, pass, ok := r.BasicAuth()
		return ok && user == strconv.Itoa(fakeTenantID) && pass == fakeCloudToken
	}
}

func writeRaw(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	fmt.Fprint(w, body)
}

func (f *fakeRules) segmentRoute(w http.ResponseWriter, r *http.Request, tail, body string) {
	parts := strings.SplitN(tail, "/", 3)
	segment, err := url.PathUnescape(parts[0])
	if err != nil || len(parts) < 2 {
		http.NotFound(w, r)
		return
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	segRules, ok := f.rules[segment]
	if !ok {
		writeRaw(w, http.StatusNotFound, fmt.Sprintf(`{"code":"not_found","error":"segment not found: %s"}`, segment))
		return
	}

	switch {
	case parts[1] == "metrics" && len(parts) == 2 && r.Method == http.MethodGet:
		f.metricList(w, r, segment)
		return
	case parts[1] == "metrics" && len(parts) == 3 && r.Method == http.MethodGet:
		metric, _ := url.PathUnescape(parts[2])
		f.metricDetail(w, segment, metric, segRules)
		return
	case parts[1] == "metrics:summary" && r.Method == http.MethodGet:
		counts := map[string]int{}
		for _, rec := range f.recs[segment] {
			counts[rec.Action]++
		}
		out, err := json.Marshal(map[string]any{"count": len(f.recs[segment]), "counts_by_action": counts})
		require.NoError(f.t, err)
		writeRaw(w, http.StatusOK, string(out))
		return
	case parts[1] != "rules":
		http.NotFound(w, r)
		return
	}

	if len(parts) == 2 {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		f.list(w, r, segRules)
		return
	}

	metric, err := url.PathUnescape(parts[2])
	if err != nil {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		rule, ok := segRules[metric]
		if !ok {
			writeRaw(w, http.StatusNotFound, fmt.Sprintf(`{"code":"not_found","error":"rule not found: %s"}`, metric))
			return
		}
		out, err := json.Marshal(rule)
		require.NoError(f.t, err)
		writeRaw(w, http.StatusOK, string(out))
	case http.MethodPut:
		var rule wireRule
		if err := json.Unmarshal([]byte(body), &rule); err != nil {
			writeRaw(w, http.StatusBadRequest, `{"code":"invalid_argument","error":"failed to decode request body"}`)
			return
		}
		if msg := validateWire(rule, metric); msg != "" {
			writeRaw(w, http.StatusBadRequest, fmt.Sprintf(`{"code":"invalid_argument","error":%q}`, msg))
			return
		}
		segRules[metric] = rule
		out, err := json.Marshal(rule)
		require.NoError(f.t, err)
		writeRaw(w, http.StatusOK, string(out))
	case http.MethodDelete:
		delete(segRules, metric)
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func validateWire(r wireRule, metric string) string {
	if r.Metric != "" && r.Metric != metric {
		return "payload mismatch"
	}
	switch r.Action {
	case "drop":
		if len(r.Functions) > 0 || r.Interval != "" || r.Delay != "" || r.LabelPolicy != nil {
			return "a drop rule must not set functions, interval, delay, or label_policy"
		}
	case "aggregate":
		if len(r.Functions) == 0 {
			return "an aggregate rule requires at least one function"
		}
		if r.LabelPolicy == nil || len(r.LabelPolicy.Labels) == 0 || (r.LabelPolicy.Mode != "keep" && r.LabelPolicy.Mode != "drop") {
			return "an aggregate rule requires a valid label_policy"
		}
	default:
		return `action must be "aggregate" or "drop"`
	}
	return ""
}

func (f *fakeRules) list(w http.ResponseWriter, r *http.Request, segRules map[string]wireRule) {
	q := r.URL.Query()
	size := 1000
	if v := q.Get("page_size"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 1000 {
			writeRaw(w, http.StatusBadRequest, `{"code":"invalid_argument","error":"bad page_size"}`)
			return
		}
		size = n
	}
	if f.pageSize > 0 && f.pageSize < size {
		size = f.pageSize
	}
	after := q.Get("page_token")

	names := make([]string, 0, len(segRules))
	for m := range segRules {
		if m > after {
			names = append(names, m)
		}
	}
	sort.Strings(names)

	next := ""
	if len(names) > size {
		names = names[:size]
		next = names[len(names)-1]
	}
	page := struct {
		Rules         []wireRule `json:"rules"`
		NextPageToken string     `json:"next_page_token,omitempty"`
	}{Rules: make([]wireRule, 0, len(names)), NextPageToken: next}
	for _, m := range names {
		page.Rules = append(page.Rules, segRules[m])
	}
	out, err := json.Marshal(page)
	require.NoError(f.t, err)
	writeRaw(w, http.StatusOK, string(out))
}

func (f *fakeRules) metricList(w http.ResponseWriter, r *http.Request, segment string) {
	q := r.URL.Query()
	size := 1000
	if v := q.Get("page_size"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 1000 {
			writeRaw(w, http.StatusBadRequest, `{"code":"invalid_argument","error":"bad page_size"}`)
			return
		}
		size = n
	}
	if f.pageSize > 0 && f.pageSize < size {
		size = f.pageSize
	}
	after := q.Get("page_token")
	actions := map[string]bool{}
	for _, action := range q["action"] {
		actions[action] = true
	}

	names := make([]string, 0, len(f.recs[segment]))
	for m, rec := range f.recs[segment] {
		if after != "" && m <= after {
			continue
		}
		if len(actions) > 0 && !actions[rec.Action] {
			continue
		}
		names = append(names, m)
	}
	sort.Strings(names)

	next := ""
	if len(names) > size {
		names = names[:size]
		next = names[len(names)-1]
	}
	items := make([]map[string]any, 0, len(names))
	for _, m := range names {
		rec := f.recs[segment][m]
		item := map[string]any{
			"metric":                         m,
			"recommended_action":             rec.Action,
			"raw_series_count":               rec.RawSeries,
			"current_series_count_reduction": rec.CurrentReduction,
			"projected_series_count_change":  rec.ProjectedChange,
		}
		if rec.StatsUpdatedAt != "" {
			item["stats_updated_at"] = rec.StatsUpdatedAt
		}
		items = append(items, item)
	}
	page := map[string]any{"metrics": items}
	if next != "" {
		page["next_page_token"] = next
	}
	out, err := json.Marshal(page)
	require.NoError(f.t, err)
	writeRaw(w, http.StatusOK, string(out))
}

func (f *fakeRules) metricDetail(w http.ResponseWriter, segment, metric string, segRules map[string]wireRule) {
	rec, hasRec := f.recs[segment][metric]
	rule, hasRule := segRules[metric]
	if !hasRec && !hasRule {
		writeRaw(w, http.StatusNotFound, fmt.Sprintf(`{"code":"not_found","error":"metric not found: %s"}`, metric))
		return
	}
	detail := map[string]any{"metric": metric}
	if hasRule {
		detail["current"] = rule
	}
	if hasRec {
		verdict := map[string]any{
			"recommended_action":             rec.Action,
			"urgent":                         false,
			"raw_series_count":               rec.RawSeries,
			"current_series_count_reduction": rec.CurrentReduction,
			"projected_series_count_change":  rec.ProjectedChange,
		}
		if rec.StatsUpdatedAt != "" {
			verdict["stats_updated_at"] = rec.StatsUpdatedAt
		}
		detail["verdict"] = verdict
		if rec.Rule != nil {
			detail["recommended"] = rec.Rule
		}
		detail["stats"] = map[string]any{
			"raw_series_count":         rec.RawSeries,
			"current_series_count":     rec.CurrentSeries,
			"recommended_series_count": rec.RecommendedSeries,
			"usages_in_rules":          rec.UsagesInRules,
			"usages_in_queries":        rec.UsagesInQueries,
			"usages_in_dashboards":     rec.UsagesInDashboards,
		}
	}
	out, err := json.Marshal(detail)
	require.NoError(f.t, err)
	writeRaw(w, http.StatusOK, string(out))
}
