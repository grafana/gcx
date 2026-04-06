# SM Check Failure Modes

Reference for classifying Synthetic Monitoring check failures. Cross-reference signals from probe timeline and PromQL metrics against the table below.

## Failure Mode Reference

| Failure Mode | Signals | Likely Cause | Next Action |
|---|---|---|---|
| **Target down** | All probes failing; HTTP connection refused or 5xx; `probe_success=0` across all probes | Service crash, deployment rollback needed, upstream dependency failure, firewall rule blocking SM probe IPs | Check service health endpoints; review recent deployments; verify SM probe IP allowlist |
| **Regional / CDN** | Subset of geographically clustered probes failing; other regions healthy; `probe_success=0` for affected regions only | CDN edge node outage; BGP routing issue; regional network partition; geo-based firewall rules | Check CDN status page for affected regions; review BGP routing tables; check geo-based ACLs |
| **SSL / TLS** | TLS handshake errors; `probe_ssl_earliest_cert_expiry` shows < 14 days; `probe_http_duration_seconds{phase="tls"}` elevated or absent | Expired or expiring certificate; missing intermediate CA in chain; TLS version/cipher mismatch; wrong SNI | Renew certificate; verify full chain including intermediates; check TLS min version config |
| **DNS resolution** | DNS error in probe response; `probe_success=0` with DNS timeout signal; all probes affected equally | DNS provider outage; record deleted or misconfigured; NXDOMAIN for target hostname; TTL expired during change | Check DNS provider status; verify A/CNAME records exist; check NS delegation; reduce TTL before changes |
| **Timeout** | Probes timing out before completing; `probe_http_duration_seconds{phase="connect"}` or `{phase="tls"}` near or exceeding timeout value; intermittent across probes | Check timeout too tight for endpoint latency; target responding slowly; network congestion; resource exhaustion on target | Increase check timeout (must be < frequency); investigate target latency; check resource metrics on target |
| **Content / assertion** | Probes reach target and get response; HTTP status code unexpected (e.g., 302, 403, 404); body assertion mismatch; `probe_success=0` despite connection succeeding | Application logic change; A/B test or feature flag changed response; authentication required; redirect loop | Compare current response to expected; check recent deployments; update assertion if intentional change |
| **Private probe infra** | Only private probes failing; public probes healthy; no pattern across regions | Private probe agent down or network-isolated; agent version mismatch; proxy/firewall between agent and target | Check private probe agent status (`gcx synth probes list`); verify agent connectivity; check agent logs |
| **Rate limiting** | HTTP 429 responses; intermittent failures with apparent recovery; failures correlate with check frequency | Target rate-limiting SM probe IPs; check frequency too high for allowed request rate | Allowlist SM probe IPs on target; reduce check frequency; implement backoff or use `basicMetricsOnly: true` |

## Classification Decision Tree

```
All probes failing?
├─ YES → Check HTTP response code / connection error
│        ├─ Connection refused / timeout → Target down
│        ├─ TLS error / cert expiry <14d → SSL/TLS
│        ├─ DNS NXDOMAIN / timeout → DNS resolution
│        └─ HTTP 429 → Rate limiting
└─ NO → Subset failing?
         ├─ Geographic cluster → Regional/CDN
         ├─ Only private probes → Private probe infra
         └─ Intermittent all probes → Timeout or Flapping
              └─ Check phase latency → Timeout if connect/tls high

Probes reach target but probe_success=0?
└─ Content/assertion failure (status code or body mismatch)
```

## Key Metrics for Classification

- `probe_success` — binary pass/fail per probe
- `probe_http_status_code` — HTTP response code (4xx/5xx indicate assertion or target error)
- `probe_http_duration_seconds{phase}` — time per HTTP phase (dns, connect, tls, processing, transfer)
- `probe_ssl_earliest_cert_expiry` — Unix timestamp of earliest cert expiry; subtract `time()` and divide by 86400 for days remaining
- `probe_dns_lookup_time_seconds` — DNS resolution latency; high values suggest DNS issues
