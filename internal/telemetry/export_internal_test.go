package telemetry

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/version"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testEvent() Event {
	return Event{
		Service:           ServiceName,
		Version:           "v1.2.3",
		OS:                "darwin",
		Arch:              "arm64",
		DeviceID:          "36d988e3-971e-4566-b0ca-7416f85b0da2",
		DeviceIDPersisted: true,
		Command:           "dashboards list",
		Flags:             "output",
		Provider:          "dashboards",
		Outcome:           OutcomeOK,
		ExitCode:          0,
		DurationMS:        1059,
		IsTTY:             true,
		OutputFormat:      "table",
	}
}

// The event must round-trip the wire: what the receiver decodes from our POST
// body is field-for-field what the log mode would have printed.
func TestExportPostsJSONToEndpointOverride(t *testing.T) {
	requests := make(chan *http.Request, 1)
	bodies := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requests <- r
		bodies <- body
	}))
	defer server.Close()
	t.Setenv(envEndpoint, server.URL+"/gcx-usage-report")

	event := testEvent()
	Export(event)

	select {
	case r := <-requests:
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/gcx-usage-report", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, version.UserAgent(), r.Header.Get("User-Agent"))
	case <-time.After(5 * time.Second):
		t.Fatal("no request received")
	}

	body := <-bodies
	var decoded Event
	require.NoError(t, json.Unmarshal(body, &decoded))
	assert.Equal(t, event, decoded)

	// parse_error_* fields mirror their omitempty tags: absent from the body
	// unless set.
	var raw map[string]any
	require.NoError(t, json.Unmarshal(body, &raw))
	for _, key := range []string{
		"parse_error_kind", "parse_error_parent", "parse_error_token",
		"attempted_command", "parse_error_flags", "parse_error_nearest",
		"parse_error_distance",
	} {
		assert.NotContains(t, raw, key)
	}
}

// parse_error_* fields travel when set, including distance -1 ("no near
// match"), which omitempty must not drop.
func TestExportCarriesParseErrorFields(t *testing.T) {
	bodies := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodies <- body
	}))
	defer server.Close()
	t.Setenv(envEndpoint, server.URL)

	event := testEvent()
	event.Outcome = OutcomeParseError
	event.ParseErrorKind = "unknown_command"
	event.ParseErrorToken = "<redacted>"
	event.ParseErrorDistance = -1
	Export(event)

	select {
	case body := <-bodies:
		var raw map[string]any
		require.NoError(t, json.Unmarshal(body, &raw))
		assert.Equal(t, "unknown_command", raw["parse_error_kind"])
		assert.Equal(t, "<redacted>", raw["parse_error_token"])
		assert.InDelta(t, float64(-1), raw["parse_error_distance"], 0)
	case <-time.After(5 * time.Second):
		t.Fatal("no request received")
	}
}

// Export must never fail loudly, however broken the endpoint.
func TestExportSwallowsFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Setenv(envEndpoint, server.URL)
	Export(testEvent())

	server.Close()
	Export(testEvent())

	t.Setenv(envEndpoint, "://not a url")
	Export(testEvent())
}

// A retryable failure (503) must produce exactly one request: the export runs
// synchronously before CLI exit, so retries would delay every invocation
// whenever the receiver is unavailable.
func TestExportMakesSingleAttempt(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	t.Setenv(envEndpoint, server.URL)

	Export(testEvent())

	assert.EqualValues(t, 1, attempts.Load())
}

// exportTimeout is the ceiling on how long telemetry can hold up CLI exit: a
// receiver that accepts the request and never replies must not block Export
// past it.
func TestExportReturnsPromptlyWhenReceiverHangs(t *testing.T) {
	// Hold every response until the test ends. Released before server.Close()
	// (defers run LIFO) so Close's wait for in-flight handlers can finish.
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		<-release
	}))
	defer server.Close()
	defer close(release)
	t.Setenv(envEndpoint, server.URL)

	start := time.Now()
	Export(testEvent())
	// Generous slack over exportTimeout to avoid flakes on loaded machines.
	assert.Less(t, time.Since(start), 3*exportTimeout)
}
