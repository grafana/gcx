# SM PromQL Patterns

PromQL query patterns for Synthetic Monitoring metrics. Run via:
```bash
grafanactl query -d <datasource-uid> -e '<query>' --from <start> --to <end> --step <step>
```

Replace `<job>` with the check job name and `<instance>` with the check target (URL or hostname).

---

## probe_success Rate Over Time

Overall success rate across all probes (1 = pass, 0 = fail):
```promql
avg(probe_success{job="<job>",instance="<target>"})
```

Per-probe success rate (use to identify which probes are failing):
```promql
avg by (probe) (probe_success{job="<job>",instance="<target>"})
```

Success rate as a percentage over a 5-minute window:
```promql
100 * avg_over_time(probe_success{job="<job>",instance="<target>"}[5m])
```

Recommended step: `1m` for short windows (< 3h), `5m` for longer windows.

---

## HTTP Phase Latency

Time spent in each HTTP phase (dns, connect, tls, processing, transfer):
```promql
avg by (phase) (probe_http_duration_seconds{job="<job>",instance="<target>"})
```

Total HTTP request duration:
```promql
avg(probe_http_duration_seconds{job="<job>",instance="<target>",phase="transfer"})
+ avg(probe_http_duration_seconds{job="<job>",instance="<target>",phase="processing"})
+ avg(probe_http_duration_seconds{job="<job>",instance="<target>",phase="tls"})
+ avg(probe_http_duration_seconds{job="<job>",instance="<target>",phase="connect"})
+ avg(probe_http_duration_seconds{job="<job>",instance="<target>",phase="dns"})
```

Phase latency per probe (use to identify regional latency differences):
```promql
avg by (probe, phase) (probe_http_duration_seconds{job="<job>",instance="<target>"})
```

DNS resolution latency (use to identify DNS issues):
```promql
avg by (probe) (probe_http_duration_seconds{job="<job>",instance="<target>",phase="dns"})
```

TLS handshake latency (high values suggest cert chain or cipher negotiation issues):
```promql
avg by (probe) (probe_http_duration_seconds{job="<job>",instance="<target>",phase="tls"})
```

---

## SSL Certificate Expiry

Days until earliest certificate expiry (negative means already expired):
```promql
(probe_ssl_earliest_cert_expiry{job="<job>",instance="<target>"} - time()) / 86400
```

Alert threshold check — certs expiring within 14 days:
```promql
(probe_ssl_earliest_cert_expiry{job="<job>",instance="<target>"} - time()) / 86400 < 14
```

Per-probe cert expiry (use to detect inconsistent cert deployment across regions):
```promql
min by (probe) ((probe_ssl_earliest_cert_expiry{job="<job>",instance="<target>"} - time()) / 86400)
```

---

## Per-Probe Error Rates

Failure rate by probe (1 = always failing, 0 = always passing):
```promql
1 - avg by (probe) (probe_success{job="<job>",instance="<target>"})
```

Count of failed probe executions over 10-minute window:
```promql
count by (probe) (probe_success{job="<job>",instance="<target>"} == 0)
```

HTTP status codes by probe (identify 4xx/5xx patterns):
```promql
avg by (probe) (probe_http_status_code{job="<job>",instance="<target>"})
```

---

## Useful Filters

Filter by specific probe (use probe name from `grafanactl synth probes list`):
```promql
probe_success{job="<job>",instance="<target>",probe="<probe-name>"}
```

Filter by multiple probes (regex):
```promql
probe_success{job="<job>",instance="<target>",probe=~"<probe1>|<probe2>"}
```

---

## Recommended Grafanactl Commands

Quick per-probe check (JSON for agent processing):
```bash
grafanactl query -d <datasource-uid> \
  -e 'avg by (probe) (probe_success{job="<job>",instance="<target>"})' \
  --from now-1h --to now --step 1m -o json
```

Graph for user display:
```bash
grafanactl query -d <datasource-uid> \
  -e 'avg by (probe) (probe_success{job="<job>",instance="<target>"})' \
  --from now-1h --to now --step 1m -o graph
```

Cert expiry check:
```bash
grafanactl query -d <datasource-uid> \
  -e '(probe_ssl_earliest_cert_expiry{job="<job>",instance="<target>"} - time()) / 86400' \
  --from now-5m --to now --step 1m -o json
```
