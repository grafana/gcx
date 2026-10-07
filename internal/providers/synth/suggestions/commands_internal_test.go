package suggestions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/query/synth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

const twoSuggestions = `{"suggestions":[` +
	`{"id":"s1","target":"https://a.example.com","checkType":"http","namespace":"payments",` +
	`"confidence":"high","score":0.91,"reachability":"public","reachabilitySource":"probe",` +
	`"dedupStatus":"new","authRequired":false,"prompt":"p","rationale":"busy and slow",` +
	`"evidence":{"reqPerS":12.5,"errorRatio":0.02,"p99Ms":840}},` +
	`{"id":"s2","target":"https://b.example.com","checkType":"grpc","confidence":"low","score":0.4,` +
	`"reachability":"unknown","reachabilitySource":"none","dedupStatus":"new","authRequired":true,"prompt":"p",` +
	`"evidence":{}}],"warnings":[]}`

type call struct {
	method, path string
	body         []byte
}

// fakeCaller answers CallResource from a script keyed by path and records calls.
type fakeCaller struct {
	responses map[string]*synth.Response
	errs      map[string]error
	calls     []call
}

func (f *fakeCaller) CallResource(_ context.Context, _, method, path string, body []byte) (*synth.Response, error) {
	f.calls = append(f.calls, call{method, path, body})
	if err := f.errs[path]; err != nil {
		return nil, err
	}

	return f.responses[path], nil
}

func healthy() map[string]*synth.Response {
	return map[string]*synth.Response{
		healthPath:      {StatusCode: http.StatusOK, Body: []byte(`{}`)},
		suggestionsPath: {StatusCode: http.StatusOK, Body: []byte(twoSuggestions)},
	}
}

func TestFetch_ProbesHealthBeforeGenerating(t *testing.T) {
	f := &fakeCaller{responses: healthy()}

	res, err := fetch(context.Background(), f, "sm-uid")
	require.NoError(t, err)

	require.Len(t, f.calls, 2)
	assert.Equal(t, call{http.MethodGet, healthPath, nil}, f.calls[0])
	assert.Equal(t, http.MethodPost, f.calls[1].method)
	assert.Equal(t, suggestionsPath, f.calls[1].path)
	assert.JSONEq(t, `{}`, string(f.calls[1].body))

	require.Len(t, res.Suggestions, 2)
	assert.Equal(t, "s1", res.Suggestions[0].ID)
	assert.Equal(t, "payments", res.Suggestions[0].Namespace)
	assert.InDelta(t, 0.91, res.Suggestions[0].Score, 1e-9)
}

// A confirmed 404 on health is the only "not available" answer. The paid
// generation call must not be made when the service is not there.
func TestFetch_HealthNotFoundMeansUnavailableAndSkipsGeneration(t *testing.T) {
	r := healthy()
	r[healthPath] = &synth.Response{StatusCode: http.StatusNotFound, Body: []byte(`{"message":"x"}`)}
	f := &fakeCaller{responses: r}

	_, err := fetch(context.Background(), f, "sm-uid")
	require.ErrorIs(t, err, ErrUnavailable)
	assert.Len(t, f.calls, 1, "generation must not be called")
}

// A 404 can also come from a plugin too old to proxy the inbox ("resource not
// found"), where "not in this region" would send the user the wrong way. The
// server's own message must survive so the two can be told apart.
func TestFetch_HealthNotFoundKeepsServerMessage(t *testing.T) {
	r := healthy()
	r[healthPath] = &synth.Response{StatusCode: http.StatusNotFound, Body: []byte(`{"message":"resource not found"}`)}

	_, err := fetch(context.Background(), &fakeCaller{responses: r}, "sm-uid")
	require.ErrorIs(t, err, ErrUnavailable)
	assert.Contains(t, err.Error(), "resource not found")
}

// Anything but 200/404 on health is inconclusive: report it as a failure rather
// than as "not available", and still do not generate.
func TestFetch_HealthInconclusiveIsAnErrorNotUnavailable(t *testing.T) {
	r := healthy()
	r[healthPath] = &synth.Response{StatusCode: http.StatusBadGateway, Body: []byte(`{"message":"unreachable"}`)}
	f := &fakeCaller{responses: r}

	_, err := fetch(context.Background(), f, "sm-uid")
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrUnavailable)
	assert.Contains(t, err.Error(), "502")
	assert.Len(t, f.calls, 1)
}

func TestFetch_TransportErrorPropagates(t *testing.T) {
	boom := errors.New("boom")
	f := &fakeCaller{responses: healthy(), errs: map[string]error{healthPath: boom}}

	_, err := fetch(context.Background(), f, "sm-uid")
	require.ErrorIs(t, err, boom)
}

func TestFetch_GenerationStatusMapping(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		wantIs     error
		wantSubstr string
	}{
		// Health already confirmed the service exists, so a 404 here is not a
		// confirmed negative: it is reported as the failure it is.
		{"not found after a healthy probe is a plain failure", http.StatusNotFound, `{"message":"resource not found"}`, nil, "404"},
		{"service unavailable is not configured", http.StatusServiceUnavailable, `{"message":"synthetic monitoring is not configured"}`, ErrNotReady, "synthetic monitoring is not configured"},
		{"a 503 keeps its body so a gateway outage is distinguishable", http.StatusServiceUnavailable, `upstream connect error`, ErrNotReady, "upstream connect error"},
		{"other statuses carry status and message", http.StatusInternalServerError, `{"message":"kaboom"}`, nil, "500"},
		{"message is surfaced", http.StatusBadGateway, `{"message":"reliability inbox response is too large"}`, nil, "too large"},
		// The service itself (not the plugin) answers errors as {"error": "..."}.
		{"the service's error field is surfaced", http.StatusUnauthorized, `{"error":"the Synthetic Monitoring API rejected this token"}`, nil, "rejected this token"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := healthy()
			r[suggestionsPath] = &synth.Response{StatusCode: tt.status, Body: []byte(tt.body)}
			f := &fakeCaller{responses: r}

			_, err := fetch(context.Background(), f, "sm-uid")
			require.Error(t, err)

			if tt.wantIs != nil {
				require.ErrorIs(t, err, tt.wantIs)
			} else {
				require.NotErrorIs(t, err, ErrUnavailable)
				require.NotErrorIs(t, err, ErrNotReady)
			}
			if tt.wantSubstr != "" {
				assert.Contains(t, err.Error(), tt.wantSubstr)
			}
		})
	}
}

// The service reports degraded failures as HTTP 200 with warnings and nothing
// else. The app treats that as an error rather than an empty inbox; so do we.
func TestFetch_WarningsWithNoSuggestionsIsAnError(t *testing.T) {
	r := healthy()
	r[suggestionsPath] = &synth.Response{
		StatusCode: http.StatusOK,
		Body:       []byte(`{"suggestions":[],"warnings":["llm timed out","no telemetry"]}`),
	}

	_, err := fetch(context.Background(), &fakeCaller{responses: r}, "sm-uid")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "llm timed out")
	assert.Contains(t, err.Error(), "no telemetry")
}

// Warnings next to real suggestions are not an error; they are kept for the caller.
func TestFetch_WarningsWithSuggestionsAreKept(t *testing.T) {
	r := healthy()
	r[suggestionsPath] = &synth.Response{
		StatusCode: http.StatusOK,
		Body:       []byte(`{"suggestions":[{"id":"s1","target":"t","checkType":"http"}],"warnings":["partial"]}`),
	}

	res, err := fetch(context.Background(), &fakeCaller{responses: r}, "sm-uid")
	require.NoError(t, err)
	assert.Equal(t, []string{"partial"}, res.Warnings)
	assert.Len(t, res.Suggestions, 1)
}

func TestFetch_EmptyInboxIsNotAnError(t *testing.T) {
	r := healthy()
	r[suggestionsPath] = &synth.Response{StatusCode: http.StatusOK, Body: []byte(`{"suggestions":[],"warnings":[]}`)}

	res, err := fetch(context.Background(), &fakeCaller{responses: r}, "sm-uid")
	require.NoError(t, err)
	assert.Empty(t, res.Suggestions)
}

func TestFetch_MalformedBodyIsAnError(t *testing.T) {
	r := healthy()
	r[suggestionsPath] = &synth.Response{StatusCode: http.StatusOK, Body: []byte(`not json`)}

	_, err := fetch(context.Background(), &fakeCaller{responses: r}, "sm-uid")
	require.Error(t, err)
}

func TestSuggestionTable_Columns(t *testing.T) {
	var res result
	require.NoError(t, res.decode([]byte(twoSuggestions)))

	var buf bytes.Buffer
	require.NoError(t, suggestionTable().Codec("table").Encode(&buf, res.Suggestions))

	out := buf.String()
	for _, h := range []string{"ID", "TARGET", "TYPE", "CONFIDENCE", "SCORE", "REACHABILITY", "NAMESPACE"} {
		assert.Contains(t, out, h)
	}
	assert.Contains(t, out, "https://a.example.com")
	assert.Contains(t, out, "payments")
	assert.Contains(t, out, "0.91")
	// s2 has no namespace: unattributed, not an error and not the string "<nil>".
	assert.NotContains(t, out, "<nil>")
}

// ---------------------------------------------------------------------------
// End to end through cobra, against an httptest Grafana.
// ---------------------------------------------------------------------------

type fakeLoader struct {
	host, uid string
}

func (l fakeLoader) LoadSMConfig(context.Context) (string, string, string, error) {
	return "", "", "", errors.New("direct SM API must not be used")
}

func (l fakeLoader) LoadSMProxyConfig(context.Context) (config.NamespacedRESTConfig, string, string, error) {
	return config.NamespacedRESTConfig{Config: rest.Config{Host: l.host}, Namespace: "default"}, l.uid, "default", nil
}

func runList(t *testing.T, loader fakeLoader, args ...string) (string, string, error) {
	t.Helper()

	cmd := Commands(loader)
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(append([]string{"list"}, args...))
	cmd.SetContext(context.Background())

	err := cmd.Execute()

	return out.String(), errOut.String(), err
}

func newGrafana(t *testing.T, suggestionsBody string) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/datasources/uid/sm-uid/resources/reliability-inbox/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/api/datasources/uid/sm-uid/resources/reliability-inbox/suggestions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(suggestionsBody))
	})

	s := httptest.NewServer(mux)
	t.Cleanup(s.Close)

	return s
}

func TestCommand_ListTable(t *testing.T) {
	s := newGrafana(t, twoSuggestions)

	stdout, _, err := runList(t, fakeLoader{host: s.URL, uid: "sm-uid"})
	require.NoError(t, err)

	assert.Contains(t, stdout, "https://a.example.com")
	assert.Contains(t, stdout, "https://b.example.com")
}

func TestCommand_ListJSON(t *testing.T) {
	s := newGrafana(t, twoSuggestions)

	stdout, _, err := runList(t, fakeLoader{host: s.URL, uid: "sm-uid"}, "-o", "json")
	require.NoError(t, err)

	assert.Contains(t, stdout, `"id": "s1"`)
	assert.Contains(t, stdout, `"checkType": "http"`)
}

// Warnings go to stderr so stdout stays parseable data.
func TestCommand_WarningsGoToStderr(t *testing.T) {
	s := newGrafana(t, `{"suggestions":[{"id":"s1","target":"t","checkType":"http"}],"warnings":["partial result"]}`)

	stdout, stderr, err := runList(t, fakeLoader{host: s.URL, uid: "sm-uid"}, "-o", "json")
	require.NoError(t, err)

	assert.Contains(t, stderr, "partial result")
	assert.NotContains(t, stdout, "partial result")
}

// nSuggestions builds a response body with n suggestions, ids s1..sn.
func nSuggestions(n int) string {
	items := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		items = append(items, fmt.Sprintf(`{"id":"s%d","target":"https://t%d.example.com","checkType":"http"}`, i, i))
	}

	return `{"suggestions":[` + strings.Join(items, ",") + `],"warnings":[]}`
}

func idsOf(t *testing.T, stdout string) []string {
	t.Helper()

	var got []Suggestion
	require.NoError(t, json.Unmarshal([]byte(stdout), &got))

	ids := make([]string, 0, len(got))
	for _, s := range got {
		ids = append(ids, s.ID)
	}

	return ids
}

// The service has no pagination and no request parameters, so the limit is
// applied here, keeping the server's order (highest confidence first).
func TestCommand_LimitDefaultsToTenAndKeepsServerOrder(t *testing.T) {
	s := newGrafana(t, nSuggestions(22))

	stdout, stderr, err := runList(t, fakeLoader{host: s.URL, uid: "sm-uid"}, "-o", "json")
	require.NoError(t, err)

	ids := idsOf(t, stdout)
	require.Len(t, ids, defaultLimit)
	assert.Equal(t, "s1", ids[0])
	assert.Equal(t, "s10", ids[9])

	// Truncation is never silent, and the note stays off stdout.
	assert.Contains(t, stderr, "showing 10 of 22")
	assert.NotContains(t, stdout, "showing")
}

func TestCommand_LimitFlag(t *testing.T) {
	s := newGrafana(t, nSuggestions(5))

	stdout, stderr, err := runList(t, fakeLoader{host: s.URL, uid: "sm-uid"}, "-o", "json", "--limit", "3")
	require.NoError(t, err)

	assert.Equal(t, []string{"s1", "s2", "s3"}, idsOf(t, stdout))
	assert.Contains(t, stderr, "showing 3 of 5")
}

func TestCommand_LimitZeroMeansAll(t *testing.T) {
	s := newGrafana(t, nSuggestions(22))

	stdout, stderr, err := runList(t, fakeLoader{host: s.URL, uid: "sm-uid"}, "-o", "json", "--limit", "0")
	require.NoError(t, err)

	assert.Len(t, idsOf(t, stdout), 22)
	assert.NotContains(t, stderr, "showing")
}

func TestCommand_NoTruncationNoteWhenUnderLimit(t *testing.T) {
	s := newGrafana(t, nSuggestions(4))

	stdout, stderr, err := runList(t, fakeLoader{host: s.URL, uid: "sm-uid"}, "-o", "json")
	require.NoError(t, err)

	assert.Len(t, idsOf(t, stdout), 4)
	assert.NotContains(t, stderr, "showing")
}

func TestCommand_NegativeLimitIsRejectedBeforeAnyCall(t *testing.T) {
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	t.Cleanup(s.Close)

	_, _, err := runList(t, fakeLoader{host: s.URL, uid: "sm-uid"}, "--limit", "-1")
	require.Error(t, err)
	assert.Equal(t, int32(0), calls.Load(), "a bad flag must not trigger a paid generation")
}

func TestCommand_NoDatasourceIsAnError(t *testing.T) {
	_, _, err := runList(t, fakeLoader{host: "http://unused", uid: ""})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "datasource")
}
