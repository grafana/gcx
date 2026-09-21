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
			t.Fatal("expected an error for an invalid --stats-warn-bytes value")
		}
	})

	t.Run("rejects an unparseable threshold even when --skip-stats is set", func(t *testing.T) {
		opts := &statsPreflightOpts{SkipStats: true, StatsWarnBytes: "not-a-size"}
		if err := opts.Validate(); err == nil {
			t.Fatal("expected an error for an invalid --stats-warn-bytes value regardless of --skip-stats")
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

// runPreflight is a small helper matching the real call sites: it resolves a
// 1GB warnBytes threshold via Validate, then calls runStatsPreflight with
// isRange=true (so start/end are used as given, no instant-query fallback
// window applies). SkipStats is checked by callers (query.go/metrics.go)
// before ever invoking runStatsPreflight, so it's not exercised here.
func runPreflight(t *testing.T, client *loki.Client, stderr *bytes.Buffer, expr string) {
	t.Helper()
	opts := &statsPreflightOpts{StatsWarnBytes: "1GB"}
	if err := opts.Validate(); err != nil {
		t.Fatalf("unexpected Validate error: %v", err)
	}
	now := time.Now()
	runStatsPreflight(context.Background(), client, stderr, "uid", expr, true, now, now, now, opts.warnBytes)
}

func TestRunStatsPreflight_OverThresholdWarns(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"streams":1,"chunks":1,"bytes":2000000000,"entries":1}`))
	})

	var stderr bytes.Buffer
	runPreflight(t, client, &stderr, `{job="x"}`)

	if stderr.Len() == 0 {
		t.Fatal("expected a warning to be printed when bytes exceed the threshold")
	}
}

// TestRunStatsPreflight_TimesOut guards the review finding that a slow
// index-stats call could hold back an already-finished query: the whole
// check must be bounded by statsPreflightTimeout, not by the caller's ctx
// alone.
func TestRunStatsPreflight_TimesOut(t *testing.T) {
	old := statsPreflightTimeout
	statsPreflightTimeout = 20 * time.Millisecond
	t.Cleanup(func() { statsPreflightTimeout = old })

	unblock := make(chan struct{})
	t.Cleanup(func() { close(unblock) })
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-unblock:
		}
	})

	done := make(chan struct{})
	var stderr bytes.Buffer
	start := time.Now()
	go func() {
		runPreflight(t, client, &stderr, `{job="x"}`)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runStatsPreflight did not return within its timeout budget")
	}

	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("runStatsPreflight took %v, expected it to be bounded by statsPreflightTimeout", elapsed)
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected no output on timeout (soft-fail), got %q", stderr.String())
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
	runPreflight(t, client, &stderr, `rate({app="backstage"}[5m])`)

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
	runPreflight(t, client, &stderr, `count_over_time({app="a"}[5m]) + count_over_time({app="b"}[5m])`)

	if stderr.Len() == 0 {
		t.Fatal("expected a warning to be printed when the summed bytes across selectors exceed the threshold")
	}
}

// TestComputeStatsBytes_SelectorsRunConcurrentlyNotSerially pins the fix for
// the "one slow selector starves the shared deadline" review finding: two
// selectors, each slower than half the overall timeout, must still both
// complete and count toward the total — which is only possible if they run
// in parallel. Serially, the first selector alone would consume enough of
// the shared budget that the second's request starts too late to finish
// before the deadline, silently dropping its bytes from the total.
func TestComputeStatsBytes_SelectorsRunConcurrentlyNotSerially(t *testing.T) {
	old := statsPreflightTimeout
	statsPreflightTimeout = 200 * time.Millisecond
	t.Cleanup(func() { statsPreflightTimeout = old })

	const perSelectorDelay = 120 * time.Millisecond
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(perSelectorDelay):
		case <-r.Context().Done():
			return
		}
		switch r.URL.Query().Get("query") {
		case `{app="a"}`:
			_, _ = w.Write([]byte(`{"streams":1,"chunks":1,"bytes":500,"entries":1}`))
		case `{app="b"}`:
			_, _ = w.Write([]byte(`{"streams":1,"chunks":1,"bytes":500,"entries":1}`))
		default:
			t.Errorf("unexpected query param %q", r.URL.Query().Get("query"))
		}
	})

	now := time.Now()
	totalBytes, ok := computeStatsBytes(context.Background(), client, "uid", `count_over_time({app="a"}[5m]) + count_over_time({app="b"}[5m])`, true, now, now, now)

	if !ok {
		t.Fatal("expected at least one selector to succeed")
	}
	if totalBytes != 1000 {
		t.Errorf("totalBytes = %d, want 1000 (both selectors' 500 bytes) — a selector's bytes were dropped, meaning they ran serially and one starved the other's share of the timeout", totalBytes)
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
	runPreflight(t, client, &stderr, `count_over_time({app="a"}[5m]) + count_over_time({app="b"}[5m])`)

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
	runStatsPreflight(context.Background(), client, &stderr, "uid", `count_over_time({job="x"}[24h])`, false, now, now, now, opts.warnBytes)

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
	runStatsPreflight(context.Background(), client, &stderr, "uid", `count_over_time({job="x"}[5m] offset 1h)`, true, start, now, now, opts.warnBytes)

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
	runPreflight(t, client, &stderr, `vector(1)`)

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
	runPreflight(t, client, &stderr, `{job="x"}`)

	if stderr.Len() != 0 {
		t.Fatalf("expected no output, got %q", stderr.String())
	}
}

func TestStatsPreflightOpts_Validate_StatsMaxBytes(t *testing.T) {
	t.Run("unset leaves hasMaxBytes false", func(t *testing.T) {
		opts := &statsPreflightOpts{StatsWarnBytes: "1GiB"}
		if err := opts.Validate(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if opts.hasMaxBytes {
			t.Error("expected hasMaxBytes to be false when --stats-max-bytes is unset")
		}
	})

	t.Run("resolves a valid threshold", func(t *testing.T) {
		opts := &statsPreflightOpts{StatsWarnBytes: "1GiB", StatsMaxBytes: "5GiB"}
		if err := opts.Validate(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !opts.hasMaxBytes {
			t.Error("expected hasMaxBytes to be true when --stats-max-bytes is set")
		}
		if opts.maxBytes != 5<<30 {
			t.Errorf("maxBytes = %d, want %d", opts.maxBytes, uint64(5<<30))
		}
	})

	t.Run("rejects an unparseable threshold as a usage error", func(t *testing.T) {
		opts := &statsPreflightOpts{StatsWarnBytes: "1GiB", StatsMaxBytes: "not-a-size"}
		if err := opts.Validate(); err == nil {
			t.Fatal("expected an error for an invalid --stats-max-bytes value")
		}
	})
}

// runPreflightSync mirrors runPreflight but for checkStatsPreflightSync,
// resolving both a warnBytes threshold and a fixed 5GB maxBytes threshold
// via Validate.
func runPreflightSync(t *testing.T, client *loki.Client, stderr *bytes.Buffer, expr, warnBytes string) error {
	t.Helper()
	opts := &statsPreflightOpts{StatsWarnBytes: warnBytes, StatsMaxBytes: "5GB"}
	if err := opts.Validate(); err != nil {
		t.Fatalf("unexpected Validate error: %v", err)
	}
	now := time.Now()
	return checkStatsPreflightSync(context.Background(), client, stderr, "uid", expr, true, now, now, now, opts.warnBytes, opts.maxBytes)
}

func TestCheckStatsPreflightSync_OverMaxBytesBlocks(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"streams":1,"chunks":1,"bytes":6000000000,"entries":1}`))
	})

	var stderr bytes.Buffer
	err := runPreflightSync(t, client, &stderr, `{job="x"}`, "1GiB")
	if err == nil {
		t.Fatal("expected an error when the estimate exceeds --stats-max-bytes")
	}
	if stderr.Len() != 0 {
		t.Errorf("expected no stderr warning when blocking (the error carries the message), got %q", stderr.String())
	}
}

func TestCheckStatsPreflightSync_BetweenWarnAndMaxWarnsWithoutBlocking(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"streams":1,"chunks":1,"bytes":2000000000,"entries":1}`))
	})

	var stderr bytes.Buffer
	err := runPreflightSync(t, client, &stderr, `{job="x"}`, "1GB")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stderr.Len() == 0 {
		t.Fatal("expected a non-blocking warning when the estimate exceeds warnBytes but not maxBytes")
	}
}

func TestCheckStatsPreflightSync_UnderWarnBytesIsSilent(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"streams":1,"chunks":1,"bytes":100,"entries":1}`))
	})

	var stderr bytes.Buffer
	err := runPreflightSync(t, client, &stderr, `{job="x"}`, "1GB")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stderr.Len() != 0 {
		t.Errorf("expected no output, got %q", stderr.String())
	}
}

func TestCheckStatsPreflightSync_NoSelectorFoundSkipsSilently(t *testing.T) {
	called := false
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		called = true
		_, _ = w.Write([]byte(`{"bytes":0}`))
	})

	var stderr bytes.Buffer
	err := runPreflightSync(t, client, &stderr, `vector(1)`, "1GB")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called {
		t.Fatal("expected index-stats endpoint not to be called when no selector can be extracted")
	}
}

// TestStartStatsPreflight_ChoosesSyncOverAsyncWhenMaxBytesSet is a direct,
// fast unit test of startStatsPreflight's branching — the switch that
// decides between the blocking and async paths is the single most
// safety-critical line in this feature (get it backwards and the block
// silently stops blocking), so it gets its own test independent of the
// slower full-command integration tests in query_test.go.
func TestStartStatsPreflight_ChoosesSyncOverAsyncWhenMaxBytesSet(t *testing.T) {
	requests := 0
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		requests++
		_, _ = w.Write([]byte(`{"streams":1,"chunks":1,"bytes":6000000000,"entries":1}`))
	})

	preflight := &statsPreflightOpts{StatsWarnBytes: "1GB", StatsMaxBytes: "5GB"}
	if err := preflight.Validate(); err != nil {
		t.Fatalf("unexpected Validate error: %v", err)
	}

	now := time.Now()
	var stderr bytes.Buffer
	wait, cancel, err := startStatsPreflight(context.Background(), client, &stderr, "uid", `{job="x"}`, true, now, now, now, preflight)

	if err == nil {
		t.Fatal("expected the sync path to block when the estimate exceeds --stats-max-bytes")
	}
	if requests != 1 {
		t.Errorf("expected exactly one index-stats request (the sync check), got %d", requests)
	}
	if wait != nil {
		t.Error("expected a nil wait function alongside a blocking error")
	}
	if cancel != nil {
		t.Error("expected a nil cancel function alongside a blocking error")
	}
}

// TestStartStatsPreflight_UsesAsyncPathWhenNoMaxBytes confirms the default
// (no --stats-max-bytes) case still uses the concurrent, zero-added-latency
// path: startStatsPreflight must return immediately (before the index-stats
// call completes), with a wait function the caller blocks on afterward.
func TestStartStatsPreflight_UsesAsyncPathWhenNoMaxBytes(t *testing.T) {
	unblock := make(chan struct{})
	t.Cleanup(func() { close(unblock) })
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		<-unblock
		_, _ = w.Write([]byte(`{"streams":1,"chunks":1,"bytes":100,"entries":1}`))
	})

	preflight := &statsPreflightOpts{StatsWarnBytes: "1GB"}
	if err := preflight.Validate(); err != nil {
		t.Fatalf("unexpected Validate error: %v", err)
	}

	now := time.Now()
	var stderr bytes.Buffer
	returned := make(chan struct{})
	var wait, cancel func()
	var err error
	go func() {
		wait, cancel, err = startStatsPreflight(context.Background(), client, &stderr, "uid", `{job="x"}`, true, now, now, now, preflight)
		close(returned)
	}()

	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("startStatsPreflight did not return promptly; expected it not to wait for the async check")
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	unblock <- struct{}{}
	wait()
	cancel() // no-op once the check has already finished; just exercised for nil-safety
}

// TestStartStatsPreflight_CancelCutsTheAsyncCheckShort pins the fix for the
// "a fast query still waits out a slow index-stats call" review finding:
// calling cancel() must make wait() return promptly, even while the
// index-stats request is still genuinely in flight — not leave it to run
// out the full statsPreflightTimeout. Without propagating the cancellation
// into the HTTP request's context, this would block for the whole timeout
// instead.
func TestStartStatsPreflight_CancelCutsTheAsyncCheckShort(t *testing.T) {
	old := statsPreflightTimeout
	statsPreflightTimeout = 5 * time.Second
	t.Cleanup(func() { statsPreflightTimeout = old })

	unblock := make(chan struct{})
	t.Cleanup(func() { close(unblock) })
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-unblock:
			_, _ = w.Write([]byte(`{"streams":1,"chunks":1,"bytes":100,"entries":1}`))
		}
	})

	preflight := &statsPreflightOpts{StatsWarnBytes: "1GB"}
	if err := preflight.Validate(); err != nil {
		t.Fatalf("unexpected Validate error: %v", err)
	}

	now := time.Now()
	var stderr bytes.Buffer
	wait, cancel, err := startStatsPreflight(context.Background(), client, &stderr, "uid", `{job="x"}`, true, now, now, now, preflight)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Simulate the real query returning quickly, well before the 5s
	// statsPreflightTimeout: cancel immediately, and wait() must not block
	// anywhere near that long.
	start := time.Now()
	cancel()
	done := make(chan struct{})
	go func() {
		wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("wait() did not return promptly after cancel(); the async check ran out its own timeout instead")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("wait() took %v after cancel(), expected it to return almost immediately", elapsed)
	}
}

// TestStartStatsPreflight_SkipStatsDisablesBothPaths confirms --skip-stats
// takes priority over --stats-max-bytes: no index-stats request at all.
func TestStartStatsPreflight_SkipStatsDisablesBothPaths(t *testing.T) {
	called := false
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		called = true
		_, _ = w.Write([]byte(`{"streams":1,"chunks":1,"bytes":6000000000,"entries":1}`))
	})

	preflight := &statsPreflightOpts{SkipStats: true, StatsWarnBytes: "1GB", StatsMaxBytes: "5GB"}
	if err := preflight.Validate(); err != nil {
		t.Fatalf("unexpected Validate error: %v", err)
	}

	now := time.Now()
	var stderr bytes.Buffer
	wait, cancel, err := startStatsPreflight(context.Background(), client, &stderr, "uid", `{job="x"}`, true, now, now, now, preflight)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cancel()
	wait()

	if called {
		t.Fatal("expected --skip-stats to prevent any index-stats request")
	}
}

func TestRunStatsPreflight_IndexStatsErrorSoftFails(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"unsupported"}`))
	})

	var stderr bytes.Buffer
	runPreflight(t, client, &stderr, `{job="x"}`)

	if stderr.Len() != 0 {
		t.Fatalf("expected no output on soft-fail, got %q", stderr.String())
	}
}
