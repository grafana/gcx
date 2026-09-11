package loki //nolint:testpackage // white-box: exercises unexported runStatsPreflight and statsPreflightOpts directly

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/query/loki"
	"k8s.io/client-go/rest"
)

func TestStatsPreflightOpts_Validate(t *testing.T) {
	t.Run("rejects an unparseable threshold as a usage error", func(t *testing.T) {
		opts := &statsPreflightOpts{StatsWarnBytes: "not-a-size"}
		if err := opts.Validate(); err == nil {
			t.Fatal("expected an error for an invalid --stats value")
		}
	})

	t.Run("rejects an unparseable threshold even when --skip-stats is set", func(t *testing.T) {
		opts := &statsPreflightOpts{SkipStats: true, StatsWarnBytes: "not-a-size"}
		if err := opts.Validate(); err == nil {
			t.Fatal("expected an error for an invalid --stats value regardless of --skip-stats")
		}
	})

	t.Run("resolves a valid threshold", func(t *testing.T) {
		opts := &statsPreflightOpts{StatsWarnBytes: "1GiB"}
		if err := opts.Validate(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if opts.warnBytes != 1<<30 {
			t.Errorf("warnBytes = %d, want %d", opts.warnBytes, uint64(1<<30))
		}
	})
}

func newTestClient(t *testing.T, handler http.HandlerFunc) *loki.Client {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: server.URL},
		Namespace: "default",
	}
	client, err := loki.NewClient(cfg)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	return client
}

// runPreflight is a small helper matching the real call sites: it resolves
// warnBytes from a raw threshold string via Validate, then calls
// runStatsPreflight with isRange=true (so start/end are used as given, no
// instant-query fallback window applies).
func runPreflight(t *testing.T, client *loki.Client, stderr *bytes.Buffer, expr, warnBytesStr string, skipStats bool) {
	t.Helper()
	opts := &statsPreflightOpts{SkipStats: skipStats, StatsWarnBytes: warnBytesStr}
	if err := opts.Validate(); err != nil {
		t.Fatalf("unexpected Validate error: %v", err)
	}
	now := time.Now()
	runStatsPreflight(context.Background(), client, stderr, "uid", expr, true, now, now, now, opts.SkipStats, opts.warnBytes)
}

func TestRunStatsPreflight_SkipStatsBypassesCall(t *testing.T) {
	called := false
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		called = true
		_, _ = w.Write([]byte(`{"bytes":0}`))
	})

	var stderr bytes.Buffer
	runPreflight(t, client, &stderr, `{job="x"}`, "1GiB", true)

	if called {
		t.Fatal("expected index-stats endpoint not to be called when SkipStats is set")
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected no output, got %q", stderr.String())
	}
}

func TestRunStatsPreflight_OverThresholdWarns(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"streams":1,"chunks":1,"bytes":2000000000,"entries":1}`))
	})

	var stderr bytes.Buffer
	runPreflight(t, client, &stderr, `{job="x"}`, "1GB", false)

	if stderr.Len() == 0 {
		t.Fatal("expected a warning to be printed when bytes exceed the threshold")
	}
}

// TestRunStatsPreflight_ExtractsSelectorFromMetricExpression guards against a
// real bug found via manual testing: Loki's index/stats endpoint rejects a
// full expression ("only label matchers are supported"), so passing a
// rate(...)-style metric expression straight through silently soft-failed
// and no warning ever printed. runStatsPreflight must reduce the expression
// to its stream selector before calling IndexStats.
func TestRunStatsPreflight_ExtractsSelectorFromMetricExpression(t *testing.T) {
	var gotQuery string
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("query")
		_, _ = w.Write([]byte(`{"streams":1,"chunks":1,"bytes":2000000000,"entries":1}`))
	})

	var stderr bytes.Buffer
	runPreflight(t, client, &stderr, `rate({app="backstage"}[5m])`, "1GB", false)

	if gotQuery != `{app="backstage"}` {
		t.Errorf("query param sent to index-stats = %q, want %q", gotQuery, `{app="backstage"}`)
	}
	if stderr.Len() == 0 {
		t.Fatal("expected a warning to be printed for a metric expression over threshold")
	}
}

// TestRunStatsPreflight_SumsMultipleSelectors guards against a second real
// bug: an expression combining two selectors via a binary operator (e.g.
// count_over_time({a}[5m]) + count_over_time({b}[5m])) must have both
// selectors' bytes summed toward the threshold, not just the first one.
func TestRunStatsPreflight_SumsMultipleSelectors(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("query") {
		case `{app="a"}`:
			_, _ = w.Write([]byte(`{"streams":1,"chunks":1,"bytes":600000000,"entries":1}`))
		case `{app="b"}`:
			_, _ = w.Write([]byte(`{"streams":1,"chunks":1,"bytes":600000000,"entries":1}`))
		default:
			t.Errorf("unexpected query param %q", r.URL.Query().Get("query"))
		}
	})

	var stderr bytes.Buffer
	// Neither selector alone (600MB) exceeds 1GB, but their sum (1.2GB) does.
	runPreflight(t, client, &stderr, `count_over_time({app="a"}[5m]) + count_over_time({app="b"}[5m])`, "1GB", false)

	if stderr.Len() == 0 {
		t.Fatal("expected a warning to be printed when the summed bytes across selectors exceed the threshold")
	}
}

// TestRunStatsPreflight_SoftFailsPerSelector verifies that one selector's
// IndexStats call failing doesn't prevent a warning derived from the
// selectors that did succeed.
func TestRunStatsPreflight_SoftFailsPerSelector(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("query") == `{app="a"}` {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"unsupported"}`))
			return
		}
		_, _ = w.Write([]byte(`{"streams":1,"chunks":1,"bytes":2000000000,"entries":1}`))
	})

	var stderr bytes.Buffer
	runPreflight(t, client, &stderr, `count_over_time({app="a"}[5m]) + count_over_time({app="b"}[5m])`, "1GB", false)

	if stderr.Len() == 0 {
		t.Fatal("expected a warning derived from the selector that succeeded")
	}
}

// TestRunStatsPreflight_WidensWindowForRangeVectorDuration guards against a
// real review finding: an instant metric query's naive window (now-1m..now)
// undercounts what Loki actually evaluates when the expression has a range
// vector like "[24h]" — the pre-flight must widen the checked window by that
// duration, not just the CLI's own instant-query default.
func TestRunStatsPreflight_WidensWindowForRangeVectorDuration(t *testing.T) {
	var gotStart string
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotStart = r.URL.Query().Get("start")
		_, _ = w.Write([]byte(`{"streams":1,"chunks":1,"bytes":1,"entries":1}`))
	})

	opts := &statsPreflightOpts{StatsWarnBytes: "1GiB"}
	if err := opts.Validate(); err != nil {
		t.Fatalf("unexpected Validate error: %v", err)
	}

	now := time.Now()
	var stderr bytes.Buffer
	runStatsPreflight(context.Background(), client, &stderr, "uid", `count_over_time({job="x"}[24h])`, false, now, now, now, opts.SkipStats, opts.warnBytes)

	wantStart := strconv.FormatInt(now.Add(-24*time.Hour).Add(-time.Minute).UnixNano(), 10)
	if gotStart != wantStart {
		t.Errorf("start param sent to index-stats = %q, want %q (24h range vector should widen the instant-query window)", gotStart, wantStart)
	}
}

// TestRunStatsPreflight_WidensWindowForOffset guards against the second
// review example: an "offset" modifier shifts the real evaluation window
// back, so the naive --from/--to window alone checks the wrong period.
func TestRunStatsPreflight_WidensWindowForOffset(t *testing.T) {
	var gotStart string
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotStart = r.URL.Query().Get("start")
		_, _ = w.Write([]byte(`{"streams":1,"chunks":1,"bytes":1,"entries":1}`))
	})

	opts := &statsPreflightOpts{StatsWarnBytes: "1GiB"}
	if err := opts.Validate(); err != nil {
		t.Fatalf("unexpected Validate error: %v", err)
	}

	now := time.Now()
	start := now.Add(-time.Hour)
	var stderr bytes.Buffer
	runStatsPreflight(context.Background(), client, &stderr, "uid", `count_over_time({job="x"}[5m] offset 1h)`, true, start, now, now, opts.SkipStats, opts.warnBytes)

	wantStart := strconv.FormatInt(start.Add(-(5*time.Minute + time.Hour)).UnixNano(), 10)
	if gotStart != wantStart {
		t.Errorf("start param sent to index-stats = %q, want %q ([5m] range vector + 1h offset should widen the range-query window)", gotStart, wantStart)
	}
}

func TestRunStatsPreflight_NoSelectorFoundSkipsSilently(t *testing.T) {
	called := false
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		called = true
		_, _ = w.Write([]byte(`{"bytes":0}`))
	})

	var stderr bytes.Buffer
	runPreflight(t, client, &stderr, `vector(1)`, "1GB", false)

	if called {
		t.Fatal("expected index-stats endpoint not to be called when no selector can be extracted")
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected no output, got %q", stderr.String())
	}
}

func TestRunStatsPreflight_UnderThresholdIsSilent(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"streams":1,"chunks":1,"bytes":100,"entries":1}`))
	})

	var stderr bytes.Buffer
	runPreflight(t, client, &stderr, `{job="x"}`, "1GB", false)

	if stderr.Len() != 0 {
		t.Fatalf("expected no output, got %q", stderr.String())
	}
}

func TestRunStatsPreflight_IndexStatsErrorSoftFails(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"unsupported"}`))
	})

	var stderr bytes.Buffer
	runPreflight(t, client, &stderr, `{job="x"}`, "1GB", false)

	if stderr.Len() != 0 {
		t.Fatalf("expected no output on soft-fail, got %q", stderr.String())
	}
}
