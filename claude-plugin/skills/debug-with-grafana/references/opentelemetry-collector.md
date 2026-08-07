# OpenTelemetry Collector / Alloy stage

Use this stage when telemetry flows through an OpenTelemetry Collector or Grafana
Alloy before reaching Grafana Cloud.

```text
application exporter
  -> collector / Alloy receiver
  -> processors, queues, batching, retry
  -> collector / Alloy exporter
```

## What this stage can prove

- The collector received spans from the application.
- The collector refused, dropped, queued, retried, or successfully exported spans.
- Loss or backpressure is happening inside the collector rather than in the
  application process or Grafana Cloud.

## Verify data reaches the collector

Discover collector self-metrics first because labels and exact metric names can
vary by version and configuration:

```bash
gcx metrics series -d <prom-uid> \
  '{__name__=~"otelcol_.*(spans|queue|retry|refused|failed).*"}' \
  --from <from> --to <to> -o json
```

When present, receiver accepted/refused span counters are the first boundary
check:

```bash
# Spans accepted by collector receivers.
gcx metrics query -d <prom-uid> \
  'sum by (receiver, transport) (increase(otelcol_receiver_accepted_spans[10m]))' \
  --from <from> --to <to> --step 1m -o json

# Spans refused by collector receivers.
gcx metrics query -d <prom-uid> \
  'sum by (receiver, transport) (increase(otelcol_receiver_refused_spans[10m]))' \
  --from <from> --to <to> --step 1m -o json
```

If accepted spans do not increase during the reproduction window, check the
application exporter endpoint, network path, receiver configuration, protocol,
TLS, and authentication between app and collector.

## Debug options

- Inspect collector/Alloy logs around the reproduction timestamp for receiver,
  processor, memory limiter, batch, queue, retry, and exporter errors.
- Temporarily add a debug/logging exporter to the traces pipeline to print spans
  received by the collector before exporting onward.
- Route a single test service or short reproduction through the debug exporter;
  avoid enabling verbose span logging globally in production.
- Confirm the traces pipeline connects the expected receiver, processors, and
  exporter. A receiver can be configured but unused if it is not wired into the
  traces pipeline.
- If multiple collectors are load-balanced, query the specific collector
  instance that handled the affected application instance when possible.

### Temporary debug exporter settings

The Collector `debug` exporter is the standard local exporter for confirming
that telemetry reached a Collector pipeline. First confirm the Collector
distribution includes the components you plan to use:

```bash
otelcol components | grep -E 'otlp|debug'
```

To verify that traces are reaching the collector while preserving the normal
export path, add `debug` alongside the existing exporter in the traces pipeline:

```yaml
receivers:
  otlp:
    protocols:
      grpc:
      http:

processors:
  batch:

exporters:
  # Existing exporter to the next hop / Grafana Cloud.
  otlphttp/grafanacloud:
    endpoint: https://<otlp-gateway>/otlp
    headers:
      Authorization: Basic <redacted>

  # Local debug exporter. With no options, it logs a summary for each export.
  debug:

service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [batch]
      exporters: [otlphttp/grafanacloud, debug]
```

For a short, low-volume reproduction where span IDs and parent IDs must be
visible in the collector logs, increase verbosity:

```yaml
exporters:
  debug:
    verbosity: detailed
```

Use `verbosity: detailed` sparingly: it can log full payload contents, including
attributes that may be sensitive, and can produce high log volume. Prefer a
single test service, narrow time window, or temporary canary collector when
possible.

Expected evidence:

- default debug exporter output proves batches are passing through the pipeline;
- `verbosity: detailed` output can show trace ID, span ID, parent ID, span name,
  resource attributes, and span attributes;
- if the debug exporter prints the span but the downstream exporter fails, the
  issue is after collector processing;
- if the debug exporter never prints the test span, focus on receiver wiring,
  client endpoint/protocol, pipeline configuration, or earlier stages.

Reference:
[OpenTelemetry Collector local exporters troubleshooting](https://opentelemetry.io/docs/collector/troubleshooting/#local-exporters).

## Metrics that suggest drops or export failure

Check exporter success/failure and queue pressure:

```bash
# Spans successfully sent by collector exporters.
gcx metrics query -d <prom-uid> \
  'sum by (exporter) (increase(otelcol_exporter_sent_spans[10m]))' \
  --from <from> --to <to> --step 1m -o json

# Exporter send failures.
gcx metrics query -d <prom-uid> \
  'sum by (exporter) (increase(otelcol_exporter_send_failed_spans[10m]))' \
  --from <from> --to <to> --step 1m -o json

# Queue pressure, when queue metrics are exposed.
gcx metrics query -d <prom-uid> \
  'max by (exporter) (otelcol_exporter_queue_size)' \
  --from <from> --to <to> --step 1m -o json
```

Also inspect discovered metrics for processor refused/dropped spans,
memory-limiter drops, enqueue failures, retry counts, queue capacity, and batch
processor behavior. Exact names vary; do not assume a metric is absent until the
series discovery query has been checked.

## Common conclusions

- **Receiver accepted spans, exporter sent spans, no failures**: move to Grafana
  Cloud / Tempo checks.
- **Receiver refused spans**: investigate receiver limits, bad payloads,
  protocol mismatch, memory pressure, or collector overload.
- **Exporter send failures or full queues**: investigate Grafana Cloud endpoint,
  auth, network, retry/backoff, queue sizing, memory limiter, or throttling.
- **Debug exporter shows the span but Tempo does not**: the issue is downstream
  of collector receipt; continue with Grafana Cloud checks.
