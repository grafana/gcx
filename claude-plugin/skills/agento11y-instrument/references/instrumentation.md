# Instrumentation fallback (minimal)

This is a deliberately minimal fallback for when the live fetch of sigil-sdk's `llms.txt` "Path B"
is unavailable. It carries only the highest-value pieces — the OTel provider setup (gap checklist
#1, the silent failure) and the instrumentation preference order. For everything else (per-provider
wrappers, framework adapters, field lists, workflow steps, content-capture modes), fetch
`https://raw.githubusercontent.com/grafana/sigil-sdk/main/llms.txt`.

## The #1 gap: OTel providers (or metrics are silently lost)

The Agent Observability SDK emits OTel spans and metrics (`gen_ai.client.operation.duration`,
`gen_ai.client.token.usage`, `gen_ai.client.time_to_first_token`,
`gen_ai.client.tool_calls_per_operation`). It does **not** create the OTel providers — that is the
application's job. Without a configured `TracerProvider` **and** `MeterProvider`, these go to the
default no-op and are lost with no error. Configure both **before** creating the SDK client, and
shut them down after the SDK client shuts down.

The exporters read `OTEL_EXPORTER_OTLP_ENDPOINT` and `OTEL_EXPORTER_OTLP_HEADERS` from the env.

### Python

```python
from opentelemetry import trace, metrics
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor
from opentelemetry.sdk.metrics import MeterProvider
from opentelemetry.sdk.metrics.export import PeriodicExportingMetricReader
from opentelemetry.sdk.resources import Resource
from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter
from opentelemetry.exporter.otlp.proto.http.metric_exporter import OTLPMetricExporter

resource = Resource.create({"service.name": "my-app"})

tp = TracerProvider(resource=resource)
tp.add_span_processor(BatchSpanProcessor(OTLPSpanExporter()))
trace.set_tracer_provider(tp)

mp = MeterProvider(resource=resource, metric_readers=[
    PeriodicExportingMetricReader(OTLPMetricExporter())
])
metrics.set_meter_provider(mp)
# Deps: opentelemetry-sdk, opentelemetry-exporter-otlp-proto-http
```

### Go

```go
traceExp, _ := otlptracehttp.New(ctx)
tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(traceExp), sdktrace.WithResource(res))
otel.SetTracerProvider(tp)
defer tp.Shutdown(ctx)

metricExp, _ := otlpmetrichttp.New(ctx)
mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp)), sdkmetric.WithResource(res))
otel.SetMeterProvider(mp)
defer mp.Shutdown(ctx)
```

### JS / TS

```typescript
import { metrics } from '@opentelemetry/api';
import { NodeTracerProvider } from '@opentelemetry/sdk-trace-node';
import { BatchSpanProcessor } from '@opentelemetry/sdk-trace-base';
import { OTLPTraceExporter } from '@opentelemetry/exporter-trace-otlp-http';
import { MeterProvider, PeriodicExportingMetricReader } from '@opentelemetry/sdk-metrics';
import { OTLPMetricExporter } from '@opentelemetry/exporter-metrics-otlp-http';

const tp = new NodeTracerProvider({ resource });
tp.addSpanProcessor(new BatchSpanProcessor(new OTLPTraceExporter()));
tp.register();

const mp = new MeterProvider({
  resource,
  readers: [new PeriodicExportingMetricReader({ exporter: new OTLPMetricExporter() })],
});
metrics.setGlobalMeterProvider(mp);
```

## Instrumentation preference order

Use the highest-level option that exists for the app's language; do not assume symmetry.

1. **Provider wrapper** — wrap the LLM client (OpenAI / Anthropic / Gemini). Least code, captures
   model/tokens/cost automatically. (`go-providers/*`, `python-providers/*`, JS subpath providers.)
2. **Framework adapter** — if the app uses a supported framework (LangGraph, LangChain,
   OpenAI Agents, LlamaIndex, Google ADK, Strands, Vercel AI SDK, and more — Python has the most,
   JS fewer, Go only google-adk). The adapter captures generations and workflow steps from callbacks.
3. **Hand-instrumentation** — the core SDK (`start_generation` / `StartGeneration` /
   `startGeneration`) around each model call, when no wrapper/adapter fits.

## Environment variables (canonical)

Use `AGENTO11Y_*` (never legacy `SIGIL_*`). The SDK reads these automatically; construct the client
with no config when they're present.

```
AGENTO11Y_ENDPOINT=<your-agent-observability-api-url>
AGENTO11Y_PROTOCOL=http
AGENTO11Y_AUTH_MODE=basic
AGENTO11Y_AUTH_TENANT_ID=<your-instance-id>
AGENTO11Y_AUTH_TOKEN=<glc_... token>

OTEL_EXPORTER_OTLP_ENDPOINT=<from stack OpenTelemetry card>
OTEL_EXPORTER_OTLP_HEADERS=Authorization=Basic <base64 of "<otlp-instance-id>:<glc_... token>">
```

For the full reference — per-provider wrapper usage, every framework adapter, the complete telemetry
field list, workflow-step schema, `set_result` field completeness, and content-capture modes — fetch
`https://raw.githubusercontent.com/grafana/sigil-sdk/main/llms.txt`.
