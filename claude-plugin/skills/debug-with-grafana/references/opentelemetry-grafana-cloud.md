# Grafana Cloud OTLP / Tempo stage

Use this stage to verify the final hop from the last exporter to Grafana Cloud
and whether accepted trace data is queryable in Tempo.

```text
application exporter or collector exporter
  -> Grafana Cloud OTLP endpoint
  -> Tempo ingestion and query
```

## What this stage can prove

- The client exporter received successful responses from Grafana Cloud.
- The trace is queryable in Tempo after ingestion delay.
- The problem is more likely query/time-range/resource scoping than export loss.
- Additional Grafana Cloud backend investigation is needed because the client
  reports success but the trace is not queryable.

## Verify the client is sending to the right endpoint

Before assuming backend loss, verify configuration at the last exporter:

- OTLP endpoint URL and region are correct.
- Protocol matches the endpoint (`http/protobuf` vs gRPC as configured).
- Auth headers/token are present and scoped for the target stack/tenant.
- The exporter is sending traces, not only metrics or logs.
- The service name/resource attributes match the values used in Tempo queries.
- Sampling policy is understood. Tail sampling and adaptive sampling can keep or
  drop whole traces; head sampling can prevent spans from being recorded before
  export.

## Verify export success at the last client-side hop

If the app exports directly to Grafana Cloud, use the in-process exporter metrics
from [`opentelemetry-in-process.md`](opentelemetry-in-process.md). If traffic
uses a collector/Alloy, use the exporter metrics from
[`opentelemetry-collector.md`](opentelemetry-collector.md).

Client-side success signals are the strongest tenant-visible proof that payloads
were accepted by the Grafana Cloud endpoint:

```bash
# Direct application exporter, when these metrics are available.
gcx metrics query -d <prom-uid> \
  'increase(otlp_exporter_exported_total{service_instance_id="<instance>",type="span",success="true"}[10m])' \
  --from <from> --to <to> --step 1m -o json

# Collector exporter, when collector metrics are available.
gcx metrics query -d <prom-uid> \
  'sum by (exporter) (increase(otelcol_exporter_sent_spans[10m]))' \
  --from <from> --to <to> --step 1m -o json
```

Also check failed-export counters and logs for HTTP status codes, authentication
errors, throttling, endpoint DNS/TLS failures, or retries.

## Verify the trace is queryable in Tempo

If you have a trace ID, fetch it directly and use a time window that covers the
trace plus ingestion/query delay:

```bash
gcx traces get -d <tempo-uid> <trace-id> \
  --from <from> --to <to> --llm -o json
```

If you do not have a trace ID, query by service and symptom:

```bash
gcx traces query -d <tempo-uid> \
  '{ resource.service.name = "<service>" && duration > 1s }' \
  --from <from> --to <to> --limit 20 -o json
```

For missing-root investigations, search for traces where Tempo has no root
service:

```bash
gcx traces query -d <tempo-uid> \
  '{ trace:rootService = "" && resource.service.name = "<service>" }' \
  --from <from> --to <to> --limit 20 -o json
```

## Debug options

- Compare the final exporter success window with the trace start/end timestamp.
  Use UTC and include enough buffer for batching and ingestion delay.
- Query by trace ID first when available; TraceQL service/name filters can miss
  traces if resource attributes differ from expectations.
- Search logs for OTLP HTTP/gRPC status codes at the final exporter.
- Check whether the trace might have been intentionally dropped by sampling or
  policy. Adaptive/tail sampling usually drops whole traces, not individual spans.
- If client-side export reports success but Tempo cannot find the trace, collect
  the trace ID, endpoint, timestamp, service labels, exporter success metrics,
  and relevant logs for Grafana Cloud/Tempo support investigation.

## Common conclusions

- **Exporter reports success and trace is queryable**: the pipeline delivered
  data; remaining waterfall gaps are likely missing instrumentation,
  propagated-but-unexported remote parents, or uninstrumented work inside spans.
- **Exporter reports failures**: investigate endpoint, auth, network, retry,
  throttling, payload size, or tenant limits.
- **Exporter reports success but trace is not queryable**: verify time range,
  datasource, service attributes, and sampling first; then escalate with trace
  ID, UTC window, and final-hop exporter evidence.
- **Trace is queryable but has missing parents**: use [`trace-gaps.md`](trace-gaps.md)
  to distinguish remote upstream context from spans lost earlier in the path.
