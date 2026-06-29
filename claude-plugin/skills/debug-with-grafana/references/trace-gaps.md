# Trace gaps and missing parents

Lightweight workflow for distributed traces where the UI shows empty waterfall gaps,
`<root span not yet received>`, or child spans whose parent is absent from the
fetched trace.

## What to distinguish

- **Missing parent evidence**: spans reference a `parentSpanId` that is not
  present in `gcx traces get --llm -o json` output.
- **Uninstrumented time**: a present parent span has elapsed time not covered by
  child spans. This may be CPU work, locks, queueing, DB connection waits,
  serialization, framework code, or any code path without child spans.
- **Remote upstream parent**: a server span can have a parent from incoming
  trace context even if the upstream caller does not export to the same backend.
  This can produce `<root span not yet received>` without implying that the
  backend dropped a span.

## First-pass checks

Fetch representative traces:

```bash
gcx traces query -d <tempo-uid> \
  '{ trace:rootService = "" && resource.service.name = "<service>" }' \
  --from <from> --to <to> --limit 20 -o json

gcx traces get -d <tempo-uid> <trace-id> --llm -o json
```

## Next checks

If missing-parent evidence suggests spans may have been created but lost, follow
[`opentelemetry.md`](opentelemetry.md) to walk the OpenTelemetry pipeline from
in-process generation through collector/Alloy to Grafana Cloud / Tempo.

For remaining uncovered time inside present spans, check runtime and wait
signals before assuming spans were dropped: CPU, GC, thread pools, queues, DB
connection pools, and downstream latency. If metrics are inconclusive, ask for
language-appropriate wall-clock profiling or add targeted spans around service
handlers, DB connection acquisition, outbound calls, and executor handoffs.
