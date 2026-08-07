# OpenTelemetry Java checks

Java-specific checks for applications instrumented with the Grafana
OpenTelemetry Java distribution or the OpenTelemetry Java agent. Use this after
first identifying the trace, service instance, and export path with the
OpenTelemetry overview and in-process stage:
[`opentelemetry.md`](opentelemetry.md) and
[`opentelemetry-in-process.md`](opentelemetry-in-process.md).

## 1. Confirm the Java agent is active

Check the JVM command line or deployment configuration and confirm the agent is
actually attached:

```bash
-javaagent:/path/to/grafana-opentelemetry-java.jar
```

Also verify the service name, resource attributes, OTLP endpoint, protocol, and
auth headers used by the process. A mismatch here can look like missing telemetry
when the app is exporting to a different service identity or backend.

## 2. Mirror spans to local logs for a bounded reproduction

For the Grafana OpenTelemetry Java distribution, the documented troubleshooting
path is to temporarily export spans to local logs while still exporting to OTLP:

```bash
OTEL_TRACES_EXPORTER=otlp,console
```

Then generate fresh traffic and allow a few minutes for data to appear before
declaring it missing. Search application, Docker, or Kubernetes logs for the
trace ID and relevant span IDs.

Console export is verbose and can affect performance. Enable it only for a short
reproduction window.

## 3. Enable Java agent debug logging only briefly

If spans are absent or incomplete and normal logs are not enough, enable verbose
agent internals for a short debugging window:

```bash
OTEL_JAVAAGENT_DEBUG=true
```

Inspect the logs for instrumentation, endpoint, auth, configuration, or OTLP
`5xx` errors. Disable this after the reproduction because it is noisy.

## 4. Search LoggingSpanExporter output with controls

Java `LoggingSpanExporter` lines commonly include trace ID and span ID:

```text
'<span name>' : <trace-id> <span-id> <kind> [tracer: <instrumentation-scope>]
```

Search for the missing parent span ID and for a known present span ID from the
same trace. The known present span is the control that proves the log stream and
query are correct.

These log lines may not include `parentSpanId`. Absence of the missing parent
span ID means no span with that ID was observed in the available exporter logs;
it does not prove that no remote context with that ID existed.

## 5. Enable JDBC datasource spans for connection acquisition visibility

If the investigation needs visibility into DB pool borrow / `getConnection`
time, enable JDBC datasource instrumentation:

```bash
OTEL_INSTRUMENTATION_JDBC_DATASOURCE_ENABLED=true
```

or:

```bash
-Dotel.instrumentation.jdbc-datasource.enabled=true
```

This can help distinguish SQL execution time from connection acquisition or pool
wait time.

## 6. Isolate instrumentation effects if needed

If the application team suspects the Java agent itself changes behavior, a final
isolation test is to disable the agent temporarily and compare the application
symptom without instrumentation:

```bash
OTEL_JAVAAGENT_ENABLED=false
```

Use this only as an isolation test; it removes telemetry needed for the
investigation.

Reference:
[Grafana Java agent troubleshooting](https://grafana.com/docs/opentelemetry/instrument/grafana-java/#troubleshoot-your-instrumentation).
