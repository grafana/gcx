package telemetry

import (
	"bytes"
	"context"
	"crypto/rand"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/httputils"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

// defaultEndpoint is the usage-stats OTLP receiver. GCX_TELEMETRY_ENDPOINT
// overrides it, for pointing test builds at a dev deployment.
const defaultEndpoint = "https://stats.grafana.org/v1/traces"

// exportTimeout caps the whole export request. Telemetry must never
// noticeably delay CLI exit, so this is deliberately tight: the payload is a
// single tiny span and the receiver replies before doing any work.
const exportTimeout = time.Second

// Export sends the event to the usage-stats receiver as one OTLP root span.
// It never reports failure: telemetry is fire-and-forget and must not affect
// the command's outcome. A lost event is fine — the shared client may retry
// transient failures, but exportTimeout caps the whole exchange.
func Export(event Event, start time.Time) {
	endpoint := defaultEndpoint
	override := os.Getenv(envEndpoint)
	if override != "" {
		endpoint = override
	}
	export(event, start, endpoint)
}

func export(event Event, start time.Time, endpoint string) {
	body, err := proto.Marshal(encodeTracesData(event, start))
	if err != nil {
		return
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/x-protobuf")
	client := httputils.NewClient(httputils.ClientOpts{Timeout: exportTimeout})
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	_ = resp.Body.Close()
}

// encodeTracesData writes the event into a trace structure. The usage-stats
// receiver expects one root span. Use TracesData instead of
// ExportTraceServiceRequest in otlp/collector/trace/v1 to save import package
// weight (it saves about 6MB).
func encodeTracesData(event Event, start time.Time) *tracepb.TracesData {
	end := start.Add(time.Duration(event.DurationMS) * time.Millisecond)
	span := &tracepb.Span{
		TraceId:           randomID(16),
		SpanId:            randomID(8),
		Name:              strings.TrimSpace(ServiceName + " " + event.Command),
		StartTimeUnixNano: uint64(start.UnixNano()),
		EndTimeUnixNano:   uint64(end.UnixNano()),
		Attributes:        spanAttributes(event),
	}
	return &tracepb.TracesData{
		ResourceSpans: []*tracepb.ResourceSpans{{
			Resource: &resourcepb.Resource{
				Attributes: []*commonpb.KeyValue{
					stringAttr("service.name", event.Service),
					stringAttr("service.version", event.Version),
					stringAttr("os.type", event.OS),
					stringAttr("host.arch", event.Arch),
				},
			},
			ScopeSpans: []*tracepb.ScopeSpans{{Spans: []*tracepb.Span{span}}},
		}},
	}
}

// spanAttributes flattens the event fields into span attributes, mirroring
// the JSON encoding: parse_error_* fields travel only when set, matching
// their omitempty tags.
func spanAttributes(event Event) []*commonpb.KeyValue {
	attrs := []*commonpb.KeyValue{
		stringAttr("device_id", event.DeviceID),
		boolAttr("device_id_persisted", event.DeviceIDPersisted),
		stringAttr("command", event.Command),
		stringAttr("flags", event.Flags),
		stringAttr("provider", event.Provider),
		stringAttr("outcome", event.Outcome),
		intAttr("exit_code", int64(event.ExitCode)),
		stringAttr("error_kind", event.ErrorKind),
		boolAttr("is_tty", event.IsTTY),
		boolAttr("is_ci", event.IsCI),
		stringAttr("ci_provider", event.CIProvider),
		boolAttr("is_agent", event.IsAgent),
		stringAttr("agent", event.Agent),
		stringAttr("target_kind", event.TargetKind),
		stringAttr("output_format", event.OutputFormat),
	}
	for _, kv := range []struct{ key, value string }{
		{"parse_error_kind", event.ParseErrorKind},
		{"parse_error_parent", event.ParseErrorParent},
		{"parse_error_token", event.ParseErrorToken},
		{"attempted_command", event.AttemptedCommand},
		{"parse_error_flags", event.ParseErrorFlags},
		{"parse_error_nearest", event.ParseErrorNearest},
	} {
		if kv.value != "" {
			attrs = append(attrs, stringAttr(kv.key, kv.value))
		}
	}
	if event.ParseErrorDistance != 0 {
		attrs = append(attrs, intAttr("parse_error_distance", int64(event.ParseErrorDistance)))
	}
	return attrs
}

// randomID returns n random bytes for a trace or span ID. The IDs only need
// to be unique-ish; they correlate nothing, by design.
func randomID(n int) []byte {
	id := make([]byte, n)
	_, _ = rand.Read(id)
	return id
}

func stringAttr(key, value string) *commonpb.KeyValue {
	return &commonpb.KeyValue{Key: key, Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: value}}}
}

func boolAttr(key string, value bool) *commonpb.KeyValue {
	return &commonpb.KeyValue{Key: key, Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_BoolValue{BoolValue: value}}}
}

func intAttr(key string, value int64) *commonpb.KeyValue {
	return &commonpb.KeyValue{Key: key, Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: value}}}
}
