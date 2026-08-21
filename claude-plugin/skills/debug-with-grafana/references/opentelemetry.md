# OpenTelemetry investigation checks

Use this as the OpenTelemetry entry point for telemetry path investigations:
missing or incomplete traces, suspected export failures, collector/Alloy drops,
or span gaps that need to be separated from uninstrumented application time.

The useful mental model is the telemetry pipeline. Debug left-to-right and find
the last stage where the data is proven present and the first stage where it is
missing or failing:

```text
application SDK / auto-instrumentation
  -> SDK processor / exporter
  -> optional OpenTelemetry Collector or Alloy
  -> Grafana Cloud OTLP endpoint
  -> Tempo
```

## Stage references

| Stage | Question to answer | Reference |
|-------|--------------------|-----------|
| In process | Did the application create the span and hand it to an exporter? | [`opentelemetry-in-process.md`](opentelemetry-in-process.md) |
| Collector / Alloy | Did the collector receive the spans and export them onward? | [`opentelemetry-collector.md`](opentelemetry-collector.md) |
| Grafana Cloud | Did Grafana Cloud accept the payload, and is it queryable in Tempo? | [`opentelemetry-grafana-cloud.md`](opentelemetry-grafana-cloud.md) |

For Java agent specifics, use
[`opentelemetry-java.md`](opentelemetry-java.md) together with the in-process
stage checks.

## Baseline data to collect first

For trace investigations, fetch representative traces and identify exact service
instances and time windows before querying metrics:

```bash
gcx traces get -d <tempo-uid> <trace-id> --llm -o json
```

Record:

- trace ID, span IDs, and any missing `parentSpanId`s;
- `service.name`, `service.namespace`, and `service_instance_id` labels;
- the trace start/end timestamps in UTC;
- whether the application exports directly to Grafana Cloud or through a
  collector/Alloy pipeline;
- whether missing data is a complete trace, a missing parent span, or uncovered
  time inside an otherwise-present span.

## Decision workflow

1. **Start in process**: prove the application generated the span and attempted
   export. If it never existed locally, look at instrumentation, sampling,
   async context propagation, or language-specific agent behavior.
2. **Check collector / Alloy** when present: prove receiver acceptance, processor
   health, queue health, and successful export toward Grafana Cloud.
3. **Check Grafana Cloud / Tempo**: prove the client saw successful OTLP export
   and that the trace is queryable by trace ID or TraceQL after ingestion delay.
4. **For waterfall gaps without missing-parent evidence**: stop chasing exporter
   drops and investigate uninstrumented application/runtime work with targeted
   spans, runtime metrics, and wall-clock profiling.

Keep the stage boundaries explicit in notes: "present in app logs but not
collector", "collector accepted but exporter failed", or "export successful but
not queryable in Tempo" are much more actionable than "missing span".
