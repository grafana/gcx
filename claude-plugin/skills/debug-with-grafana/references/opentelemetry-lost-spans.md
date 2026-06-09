# OpenTelemetry lost-span checks

OpenTelemetry-specific checks for cases where trace data contains gaps,
`<root span not yet received>`, or child spans whose parent span is absent from
`gcx traces get --llm -o json` output.

Use this after fetching representative traces and identifying missing
`parentSpanId`s. The goal is to determine whether spans were lost in the SDK,
exporter, collector, or backend path, or whether they were never created/exported
locally.

## 1. Discover available span-loss metrics

Metric names vary by SDK, distro, and collector version. Discover before
querying exact names:

```bash
# SDK / application-side span metrics for an affected instance.
gcx metrics series -d <prom-uid> \
  '{__name__=~".*(span|spans).*",service_instance_id="<instance>"}' \
  --from <from> --to <to> -o json

# Collector / Alloy self-metrics when traffic passes through a collector.
gcx metrics series -d <prom-uid> \
  '{__name__=~"otelcol_.*spans.*"}' \
  --from <from> --to <to> -o json
```

## 2. Check SDK/exporter seen vs exported

For the Grafana OpenTelemetry Java distribution, these counters show whether
the local exporter saw spans and whether export succeeded:

```bash
gcx metrics query -d <prom-uid> \
  'increase(otlp_exporter_seen_total{service_instance_id="<instance>",type="span"}[10m])' \
  --from <from> --to <to> --step 1m -o json

gcx metrics query -d <prom-uid> \
  'increase(otlp_exporter_exported_total{service_instance_id="<instance>",type="span",success="true"}[10m])' \
  --from <from> --to <to> --step 1m -o json

gcx metrics query -d <prom-uid> \
  'increase(otlp_exporter_exported_total{service_instance_id="<instance>",type="span",success="false"}[10m])' \
  --from <from> --to <to> --step 1m -o json
```

Interpretation:

- `seen` materially greater than `exported{success="true"}` suggests local
  exporter loss, retry backlog, or backpressure.
- non-zero `exported{success="false"}` indicates failed exports in that window.
- small differences can be Prometheus scrape / `increase()` extrapolation
  artifacts; look for sustained gaps or failures.

## 3. For Java apps, verify instrumentation output locally

For the Grafana OpenTelemetry Java distribution, use the documented Java
troubleshooting path when traces are absent, incomplete, or hard to reconcile:

```bash
# Temporarily mirror spans to local logs while still exporting to OTLP.
OTEL_TRACES_EXPORTER=otlp,console

# Enable verbose Java agent internals only for a short debugging window.
OTEL_JAVAAGENT_DEBUG=true
```

Then:

- generate fresh traffic and allow a few minutes for data to appear before
  declaring it missing;
- inspect application, Docker, or Kubernetes logs for telemetry export errors,
  especially endpoint/auth/configuration errors or `5xx` responses from the OTLP
  endpoint;
- confirm the JVM command line actually includes `-javaagent:<path>/grafana-opentelemetry-java.jar`;
- search logs for `LoggingSpanExporter` and compare missing span IDs against a
  known present span ID from the same trace;
- if using a collector between the app and Grafana Cloud, inspect collector logs
  and metrics before assuming the Java agent dropped spans.

Console export and agent debug logging are verbose and can affect performance;
enable them only for a bounded reproduction window. If instrumentation itself is
suspected of changing application behavior, a final isolation test is to disable
the agent temporarily:

```bash
OTEL_JAVAAGENT_ENABLED=false
```

Reference:
[Grafana Java agent troubleshooting](https://grafana.com/docs/opentelemetry/instrument/grafana-java/#troubleshoot-your-instrumentation).

## 4. Check collector / Alloy refused and failed spans

If traffic goes through OpenTelemetry Collector or Alloy, compare common
collector self-metrics when present:

```bash
# Receiver refused spans: loss before processing.
gcx metrics query -d <prom-uid> \
  'sum(increase(otelcol_receiver_refused_spans[10m]))' \
  --from <from> --to <to> --step 1m -o json

# Exporter send failures: loss or retries after processing.
gcx metrics query -d <prom-uid> \
  'sum(increase(otelcol_exporter_send_failed_spans[10m]))' \
  --from <from> --to <to> --step 1m -o json
```

If these metric names are absent, use the discovery query above and inspect the
available `otelcol_*spans*` series for receiver, processor, and exporter labels.

## 5. Search exporter or agent logs for missing span IDs

Search for missing parent span IDs directly. Use a known present span ID from
the same trace as a control to prove span IDs are searchable in logs.

```bash
# Missing parent ID under investigation.
gcx logs query -d <loki-uid> \
  '{job="<service>"} |= "<missing-parent-span-id>"' \
  --from <from> --to <to> --limit 20 -o raw

# Control: a known present span ID from the same trace.
gcx logs query -d <loki-uid> \
  '{job="<service>"} |= "<known-present-span-id>"' \
  --from <from> --to <to> --limit 5 -o raw
```

For OpenTelemetry Java, `LoggingSpanExporter` lines commonly include trace ID
and span ID:

```text
'<span name>' : <trace-id> <span-id> <kind> [tracer: <instrumentation-scope>]
```

These log lines may not include `parentSpanId`. Absence of the missing parent
span ID means no span with that ID was observed in the available exporter logs;
it does not prove that no remote context with that ID existed.

## Decision points

- Exporter or collector failures present: investigate endpoint, auth, network,
  batching, retries, memory limiter, and backpressure.
- No export failures, missing parent ID appears in exporter logs: suspect loss
  after local export or backend ingestion/path behavior.
- No export failures, missing parent ID does not appear in exporter logs: suspect
  upstream remote context, missing instrumentation boundary, or a span that was
  never created/exported locally.
