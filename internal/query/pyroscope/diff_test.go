package pyroscope_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/query/pyroscope"
	"github.com/grafana/gcx/internal/queryerror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const recursiveDiff = `{"flamegraph":{"names":["total","A","B"],"leftTicks":"100","rightTicks":"120","levels":[
{"values":["0","100","0","0","120","0","0"]},
{"values":["0","100","10","0","120","20","1"]},
{"values":["10","40","40","20","0","0","1","0","50","50","0","100","100","2"]}]}}`

func diffRequest() pyroscope.DiffRequest {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return pyroscope.DiffRequest{
		Before:      pyroscope.ProfileSelection{Selector: `{service_name="frontend",version="v1"}`, From: start, To: start.Add(time.Hour)},
		After:       pyroscope.ProfileSelection{Selector: `{service_name="frontend",version="v2"}`, From: start.Add(time.Hour), To: start.Add(2 * time.Hour)},
		ProfileType: "process_cpu:cpu:nanoseconds:cpu:nanoseconds", MaxNodes: 4096,
	}
}

func TestClient_Diff(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		err    string
	}{
		{"success", 200, recursiveDiff, ""},
		{"unavailable", 404, `{"code":"unimplemented","message":"not supported"}`, "not supported"},
		{"permission denied", 403, `{"message":"access denied"}`, "access denied"},
		{"rate limited", 429, `{"message":"query limit exceeded"}`, "query limit exceeded"},
		{"malformed response", 200, `{`, "failed to parse response"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := diffRequest()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "/api/datasources/proxy/uid/pyro/querier.v1.QuerierService/Diff", r.URL.Path)
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
				assert.Contains(t, strings.Join(r.Header.Values("Accept"), ","), "allow-utf8-labelnames=true")
				var body map[string]map[string]string
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				for name, side := range map[string]pyroscope.ProfileSelection{"left": req.Before, "right": req.After} {
					assert.Equal(t, map[string]string{
						"labelSelector": side.Selector, "profileTypeID": req.ProfileType,
						"start": strconv.FormatInt(side.From.UnixMilli(), 10), "end": strconv.FormatInt(side.To.UnixMilli(), 10), "maxNodes": "4096",
					}, body[name])
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			resp, err := newTestClient(t, server).Diff(context.Background(), "pyro", req)
			if tc.err != "" {
				require.ErrorContains(t, err, tc.err)
				if tc.status != 200 {
					var apiErr *queryerror.APIError
					require.ErrorAs(t, err, &apiErr)
					assert.Equal(t, "profile diff", apiErr.Operation)
				}
				return
			}
			require.NoError(t, err)
			assert.EqualValues(t, 100, resp.Flamegraph.LeftTicks)
			assert.EqualValues(t, 120, resp.Flamegraph.RightTicks)
		})
	}
}

func TestBuildDiffResult(t *testing.T) {
	for _, tc := range []struct {
		name       string
		body       string
		wantBefore int64
		wantAfter  int64
		wantNames  []string
		complete   bool
		warnings   int
	}{
		{"recursion and signed ordering", recursiveDiff, 100, 120, []string{"B", "A"}, true, 0},
		{"omitted costs are not symbols", `{"flamegraph":{"names":["total","fn","other"],"leftTicks":"100","rightTicks":"150","levels":[{"values":["0","100","0","0","150","0","0"]},{"values":["0","70","70","0","90","90","1","0","30","30","0","60","60","2"]}]}}`, 100, 150, []string{"fn"}, false, 1},
		{"empty before and absent proto zero", `{"flamegraph":{"names":["total","fn"],"rightTicks":"120","levels":[{"values":["0","0","0","0","120","0","0"]},{"values":["0","0","0","0","120","120","1"]}]}}`, 0, 120, []string{"fn"}, true, 1},
		{"empty after and absent proto zero", `{"flamegraph":{"names":["total","fn"],"leftTicks":"100","levels":[{"values":["0","100","0","0","0","0","0"]},{"values":["0","100","100","0","0","0","1"]}]}}`, 100, 0, []string{"fn"}, true, 1},
		{"both empty", `{"flamegraph":{"names":["total"],"levels":[{"values":["0","0","0","0","0","0","0"]}]}}`, 0, 0, []string{}, true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var resp pyroscope.DiffResponse
			require.NoError(t, json.Unmarshal([]byte(tc.body), &resp))
			result, err := pyroscope.BuildDiffResult(&resp, diffRequest())
			require.NoError(t, err)
			assert.Equal(t, "nanoseconds", result.Unit)
			assert.Equal(t, tc.wantBefore, result.Total.Before)
			assert.Equal(t, tc.wantAfter, result.Total.After)
			assert.Equal(t, tc.wantAfter-tc.wantBefore, result.Total.Delta)
			assert.Equal(t, tc.complete, result.Complete)
			assert.Len(t, result.Warnings, tc.warnings)
			names := make([]string, 0, len(result.Symbols))
			for _, symbol := range result.Symbols {
				names = append(names, symbol.Name)
			}
			assert.Equal(t, tc.wantNames, names)
			if tc.wantBefore == 0 || tc.wantAfter == 0 {
				assert.Nil(t, result.Total.ChangePercent)
				for _, symbol := range result.Symbols {
					assert.Nil(t, symbol.Self.ChangePercent)
				}
			}
			if !tc.complete {
				assert.EqualValues(t, 30, result.Omitted.Before)
				assert.EqualValues(t, 60, result.Omitted.After)
			}
			if tc.name == "recursion and signed ordering" {
				a := result.Symbols[1]
				assert.Equal(t, "A", a.Name)
				assert.EqualValues(t, 50, a.Self.Before)
				assert.EqualValues(t, 20, a.Self.After)
				assert.EqualValues(t, 100, a.Total.Before, "recursive frame must not be counted twice")
				assert.EqualValues(t, 120, a.Total.After)
				require.NotNil(t, a.Self.ChangePercent)
				assert.InDelta(t, -60, *a.Self.ChangePercent, 0.001)
			}
			encoded, err := json.Marshal(result)
			require.NoError(t, err)
			assert.NotContains(t, string(encoded), `"symbols":null`)
		})
	}
}

func TestBuildDiffResult_InvalidGraphs(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*pyroscope.DiffResponse)
		err  string
	}{
		{"missing graph", func(r *pyroscope.DiffResponse) { r.Flamegraph = nil }, "missing its flamegraph"},
		{"wrong tuple width", func(r *pyroscope.DiffResponse) { r.Flamegraph.Levels[1].Values = []string{"0"} }, "seven values"},
		{"invalid integer", func(r *pyroscope.DiffResponse) { r.Flamegraph.Levels[1].Values[2] = "oops" }, "invalid profile diff value"},
		{"negative cost", func(r *pyroscope.DiffResponse) { r.Flamegraph.Levels[1].Values[2] = "-1" }, "invalid profile diff value"},
		{"bad name index", func(r *pyroscope.DiffResponse) { r.Flamegraph.Levels[1].Values[6] = "99" }, "invalid profile diff node"},
		{"self larger than total", func(r *pyroscope.DiffResponse) { r.Flamegraph.Levels[1].Values[2] = "101" }, "invalid profile diff node"},
		{"negative ticks", func(r *pyroscope.DiffResponse) { r.Flamegraph.LeftTicks = -1 }, "negative tick"},
		{"huge offset", func(r *pyroscope.DiffResponse) { r.Flamegraph.Levels[1].Values[0] = "9223372036854775807" }, "offset exceeds"},
		{"lost samples", func(r *pyroscope.DiffResponse) { r.Flamegraph.Levels = r.Flamegraph.Levels[:2] }, "self costs do not match"},
		{"no parent", func(r *pyroscope.DiffResponse) {
			r.Flamegraph.Levels[2].Values[0] = "0"
			r.Flamegraph.Levels[2].Values[3] = "0"
			r.Flamegraph.Levels[1].Values[0] = "1"
			r.Flamegraph.Levels[1].Values[1] = "99"
		}, "no parent"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var resp pyroscope.DiffResponse
			require.NoError(t, json.Unmarshal([]byte(recursiveDiff), &resp))
			tc.edit(&resp)
			_, err := pyroscope.BuildDiffResult(&resp, diffRequest())
			require.ErrorContains(t, err, tc.err)
		})
	}
}

func TestDiffLargeCostsAndTableProjection(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
	}{
		{"large int64 costs", `{"flamegraph":{"names":["total","fn"],"leftTicks":"9223372036854775806","rightTicks":"9223372036854775807","levels":[{"values":["0","9223372036854775806","0","0","9223372036854775807","0","0"]},{"values":["0","9223372036854775806","9223372036854775806","0","9223372036854775807","9223372036854775807","1"]}]}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var resp pyroscope.DiffResponse
			require.NoError(t, json.Unmarshal([]byte(tc.raw), &resp))
			result, err := pyroscope.BuildDiffResult(&resp, diffRequest())
			require.NoError(t, err)
			assert.EqualValues(t, 1, result.Total.Delta)
			require.NotNil(t, result.Total.ChangePercent)
			assert.Positive(t, *result.Total.ChangePercent)
			// Projection limits must not change the structured result.
			for len(result.Symbols) < 21 {
				result.Symbols = append(result.Symbols, pyroscope.SymbolDiff{Name: "another.symbol"})
			}
			var table bytes.Buffer
			require.NoError(t, pyroscope.FormatDiffTable(&table, result))
			assert.Contains(t, table.String(), "Showing 20 of 21")
			assert.Len(t, result.Symbols, 21)
		})
	}
}

func TestDiffTableAndUnequalWindows(t *testing.T) {
	var resp pyroscope.DiffResponse
	require.NoError(t, json.Unmarshal([]byte(recursiveDiff), &resp))
	req := diffRequest()
	req.After.To = req.After.To.Add(time.Hour)
	result, err := pyroscope.BuildDiffResult(&resp, req)
	require.NoError(t, err)
	require.Len(t, result.Warnings, 1)
	assert.Contains(t, result.Warnings[0], "not normalized")
	var buf bytes.Buffer
	require.NoError(t, pyroscope.FormatDiffTable(&buf, result))
	assert.Contains(t, buf.String(), "nanoseconds")
	assert.Contains(t, buf.String(), "+50")
	assert.Contains(t, buf.String(), "-60.0%")
}
