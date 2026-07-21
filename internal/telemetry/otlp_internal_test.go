package telemetry

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
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

// The event must round-trip the wire: what the receiver decodes from our
// marshalled bytes is field-for-field what the log mode would have printed.
func TestEncodeTracesDataRoundTrip(t *testing.T) {
	event := testEvent()
	start := time.Now()

	body, err := proto.Marshal(encodeTracesData(event, start))
	require.NoError(t, err)

	decoded := &tracepb.TracesData{}
	require.NoError(t, proto.Unmarshal(body, decoded))

	require.Len(t, decoded.GetResourceSpans(), 1)
	rs := decoded.GetResourceSpans()[0]
	assert.Equal(t, map[string]any{
		"service.name":    ServiceName,
		"service.version": "v1.2.3",
		"os.type":         "darwin",
		"host.arch":       "arm64",
	}, attrValues(t, rs.GetResource().GetAttributes()))

	require.Len(t, rs.GetScopeSpans(), 1)
	require.Len(t, rs.GetScopeSpans()[0].GetSpans(), 1)
	span := rs.GetScopeSpans()[0].GetSpans()[0]

	assert.Len(t, span.GetTraceId(), 16)
	assert.Len(t, span.GetSpanId(), 8)
	assert.Equal(t, "gcx dashboards list", span.GetName())
	// The receiver derives duration_ms from the span timestamps; they must
	// reproduce the event's DurationMS exactly.
	assert.Equal(t, uint64(start.UnixNano()), span.GetStartTimeUnixNano())
	end := start.Add(time.Duration(event.DurationMS) * time.Millisecond)
	assert.Equal(t, uint64(end.UnixNano()), span.GetEndTimeUnixNano())

	assert.Equal(t, map[string]any{
		"device_id":           event.DeviceID,
		"device_id_persisted": true,
		"command":             "dashboards list",
		"flags":               "output",
		"provider":            "dashboards",
		"outcome":             OutcomeOK,
		"exit_code":           int64(0),
		"error_kind":          "",
		"is_tty":              true,
		"is_ci":               false,
		"ci_provider":         "",
		"is_agent":            false,
		"agent":               "",
		"target_kind":         "",
		"output_format":       "table",
	}, attrValues(t, span.GetAttributes()))
}

// parse_error_* attributes mirror the JSON omitempty tags: absent unless set,
// with distance -1 ("no near match") surviving.
func TestEncodeTracesDataParseErrorAttributes(t *testing.T) {
	event := testEvent()
	attrs := attrValues(t, spanAttributes(event))
	for _, key := range []string{
		"parse_error_kind", "parse_error_parent", "parse_error_token",
		"attempted_command", "parse_error_flags", "parse_error_nearest",
		"parse_error_distance",
	} {
		assert.NotContains(t, attrs, key)
	}

	event.ParseErrorKind = "unknown_command"
	event.ParseErrorToken = "<redacted>"
	event.ParseErrorDistance = -1
	attrs = attrValues(t, spanAttributes(event))
	assert.Equal(t, "unknown_command", attrs["parse_error_kind"])
	assert.Equal(t, "<redacted>", attrs["parse_error_token"])
	assert.Equal(t, int64(-1), attrs["parse_error_distance"])
}

func TestExportPostsProtobufToEndpointOverride(t *testing.T) {
	requests := make(chan *http.Request, 1)
	bodies := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requests <- r
		bodies <- body
	}))
	defer server.Close()
	t.Setenv(envEndpoint, server.URL)

	Export(testEvent(), time.Now())

	select {
	case r := <-requests:
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/x-protobuf", r.Header.Get("Content-Type"))
	case <-time.After(5 * time.Second):
		t.Fatal("no request received")
	}
	decoded := &tracepb.TracesData{}
	require.NoError(t, proto.Unmarshal(<-bodies, decoded))
	require.Len(t, decoded.GetResourceSpans(), 1)
}

// Export must never fail loudly, however broken the endpoint.
func TestExportSwallowsFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Setenv(envEndpoint, server.URL)
	Export(testEvent(), time.Now())

	server.Close()
	Export(testEvent(), time.Now())

	t.Setenv(envEndpoint, "://not a url")
	Export(testEvent(), time.Now())
}

// The exporter deliberately uses only the OTLP message packages; the
// collector packages would pull google.golang.org/grpc into a build that has
// none. TracesData is wire-identical to ExportTraceServiceRequest, so
// nothing is lost.
func TestGrpcStaysOutOfGoMod(t *testing.T) {
	gomod, err := os.ReadFile("../../go.mod")
	require.NoError(t, err)
	assert.NotContains(t, string(gomod), "google.golang.org/grpc")
}

func attrValues(t *testing.T, kvs []*commonpb.KeyValue) map[string]any {
	t.Helper()
	m := make(map[string]any, len(kvs))
	for _, kv := range kvs {
		require.NotContains(t, m, kv.GetKey(), "duplicate attribute key")
		switch v := kv.GetValue().GetValue().(type) {
		case *commonpb.AnyValue_StringValue:
			m[kv.GetKey()] = v.StringValue
		case *commonpb.AnyValue_BoolValue:
			m[kv.GetKey()] = v.BoolValue
		case *commonpb.AnyValue_IntValue:
			m[kv.GetKey()] = v.IntValue
		default:
			t.Fatalf("unexpected attribute type for %q", kv.GetKey())
		}
	}
	return m
}
