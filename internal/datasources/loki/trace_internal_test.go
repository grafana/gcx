package loki

import (
	"context"
	"errors"
	"testing"

	"github.com/grafana/gcx/internal/query/loki"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTraceOpts_Validate(t *testing.T) {
	tests := []struct {
		name         string
		opts         traceOpts
		exprProvided bool
		wantErr      string
	}{
		{name: "disabled", opts: traceOpts{}},
		{name: "span without trace", opts: traceOpts{SpanID: "abc"}, wantErr: "--span-id requires --trace-id"},
		{name: "service without trace", opts: traceOpts{Service: "app"}, wantErr: "--trace-service requires --trace-id"},
		{name: "valid trace", opts: traceOpts{TraceID: "0000000000000000237857C3f3e35e37"}},
		{name: "trace with selector", opts: traceOpts{TraceID: "abc123"}, exprProvided: true},
		{name: "non hex trace", opts: traceOpts{TraceID: `abc"} or {`}, wantErr: "invalid --trace-id"},
		{name: "non hex span", opts: traceOpts{TraceID: "abc", SpanID: "zz"}, wantErr: "invalid --span-id"},
		{name: "service and selector", opts: traceOpts{TraceID: "abc", Service: "app"}, exprProvided: true, wantErr: "--trace-service cannot be combined"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.opts.validate(tc.exprProvided)
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestServiceSelectors(t *testing.T) {
	assert.Equal(t, []string{
		`{service_name="app"}`,
		`{job=~"(.*/)?(app)"}`,
	}, serviceSelectors("app"))

	// Regex metacharacters are escaped for the regex and the LogQL string.
	assert.Equal(t, `{job=~"(.*/)?(my\\.svc)"}`, serviceSelectors("my.svc")[1])
	// Quotes cannot break out of the selector.
	assert.Equal(t, `{service_name="a\"b"}`, serviceSelectors(`a"b`)[0])
}

func TestCheckSelector(t *testing.T) {
	require.NoError(t, checkSelector(`{service_name="app"}`))
	require.NoError(t, checkSelector(`{a="b", c=~"d.*"}`))
	require.Error(t, checkSelector(`{a="b"} |= "x"`))
	require.Error(t, checkSelector(`{a="b"} | json`))
	require.Error(t, checkSelector(`rate({a="b"}[1m])`))
	require.Error(t, checkSelector(`app`))
	require.Error(t, checkSelector(`{a="b"} garbage`))
	require.Error(t, checkSelector(`{a="b"}}`))
	require.NoError(t, checkSelector(`  {a="b"}  `))
}

func TestBuildTraceCandidates_Trace(t *testing.T) {
	const id = "0000000000000000237857c3f3e35e37"
	got := buildTraceCandidates(traceOpts{TraceID: id}, []string{`{service_name="app"}`, `{job=~"(.*/)?(app)"}`})

	// 6 field variants x 2 selectors + 1 line filter per selector.
	require.Len(t, got, 6*2+2)
	assert.Equal(t, `{service_name="app"} | logfmt | json | drop __error__ | traceID="`+id+`"`, got[0].Expr)
	assert.Equal(t, `{job=~"(.*/)?(app)"} | logfmt | json | drop __error__ | traceID="`+id+`"`, got[1].Expr)
	assert.Equal(t, `{service_name="app"} | logfmt | json | drop __error__ | trace_id="`+id+`"`, got[2].Expr)
	assert.Equal(t, `{service_name="app"} | logfmt | json | drop __error__ | otel_trace_id="`+id+`"`, got[10].Expr)
	assert.Equal(t, `{service_name="app"} |= "`+id+`"`, got[12].Expr)
	for _, c := range got {
		assert.NotContains(t, c.Expr, "span")
	}
}

func TestBuildTraceCandidates_SpanFallsBackToTrace(t *testing.T) {
	const (
		traceID = "0000000000000000237857c3f3e35e37"
		spanID  = "46be9af5ba3e6a15"
	)
	got := buildTraceCandidates(traceOpts{TraceID: traceID, SpanID: spanID}, []string{`{service_name="app"}`})

	// (6 field variants + 1 line filter) with span, then the same without.
	require.Len(t, got, 14)
	assert.Equal(t, `{service_name="app"} | logfmt | json | drop __error__ | traceID="`+traceID+`" | spanID="`+spanID+`"`, got[0].Expr)
	assert.Equal(t, `{service_name="app"} |= "`+traceID+`" |= "`+spanID+`"`, got[6].Expr)
	assert.Equal(t, `{service_name="app"} | logfmt | json | drop __error__ | traceID="`+traceID+`"`, got[7].Expr)
	assert.Equal(t, `{service_name="app"} |= "`+traceID+`"`, got[13].Expr)
	assert.Contains(t, got[0].Strategy, "trace+span")
	assert.NotContains(t, got[7].Strategy, "trace+span")
}

func TestDiscoverySelectors(t *testing.T) {
	tests := []struct {
		name   string
		labels []string
		want   []string
	}{
		{
			name:   "prefers well-known labels in order, capped",
			labels: []string{"pod", "job", "zone", "service_name", "app"},
			want:   []string{`{service_name=~".+"}`, `{job=~".+"}`},
		},
		{
			name:   "falls back to first sorted label",
			labels: []string{"zone", "__stream_shard__", "env"},
			want:   []string{`{env=~".+"}`},
		},
		{name: "no labels", labels: nil, want: []string{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, discoverySelectors(tc.labels))
		})
	}
}

func respWithLines(n int) *loki.QueryResponse {
	resp := &loki.QueryResponse{}
	if n > 0 {
		entries := make([]loki.LogEntry, n)
		resp.Data.Result = []loki.StreamEntry{{Values: entries}}
	}
	return resp
}

func TestProbeTraceCandidates(t *testing.T) {
	cands := []traceCandidate{{Expr: "a"}, {Expr: "b"}, {Expr: "c"}}

	t.Run("stops at first match", func(t *testing.T) {
		var seen []string
		m, err := probeTraceCandidates(context.Background(), cands, func(_ context.Context, e string) (*loki.QueryResponse, error) {
			seen = append(seen, e)
			if e == "b" {
				return respWithLines(2), nil
			}
			return respWithLines(0), nil
		})
		require.NoError(t, err)
		assert.True(t, m.Found)
		assert.Equal(t, "b", m.Expr)
		assert.Equal(t, 2, m.Tried)
		assert.Equal(t, []string{"a", "b"}, seen)
	})

	t.Run("no match returns last empty response", func(t *testing.T) {
		m, err := probeTraceCandidates(context.Background(), cands, func(context.Context, string) (*loki.QueryResponse, error) {
			return respWithLines(0), nil
		})
		require.NoError(t, err)
		assert.False(t, m.Found)
		assert.Equal(t, "c", m.Expr)
		assert.Equal(t, 3, m.Tried)
	})

	t.Run("skips failing candidates", func(t *testing.T) {
		m, err := probeTraceCandidates(context.Background(), cands, func(_ context.Context, e string) (*loki.QueryResponse, error) {
			if e == "a" {
				return nil, errors.New("boom")
			}
			return respWithLines(1), nil
		})
		require.NoError(t, err)
		assert.Equal(t, "b", m.Expr)
		assert.Equal(t, 1, m.Failed)
	})

	t.Run("no match counts failures", func(t *testing.T) {
		m, err := probeTraceCandidates(context.Background(), cands, func(_ context.Context, e string) (*loki.QueryResponse, error) {
			if e == "a" {
				return nil, errors.New("boom")
			}
			return respWithLines(0), nil
		})
		require.NoError(t, err)
		assert.False(t, m.Found)
		assert.Equal(t, 1, m.Failed)
		assert.Equal(t, 3, m.Tried)
	})

	t.Run("all failing returns first error", func(t *testing.T) {
		_, err := probeTraceCandidates(context.Background(), cands, func(_ context.Context, e string) (*loki.QueryResponse, error) {
			return nil, errors.New("boom " + e)
		})
		require.EqualError(t, err, "boom a")
	})

	t.Run("canceled context stops probing", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := probeTraceCandidates(ctx, cands, func(context.Context, string) (*loki.QueryResponse, error) {
			t.Error("must not run")
			return nil, errors.New("unexpected call")
		})
		require.ErrorIs(t, err, context.Canceled)
	})
}
