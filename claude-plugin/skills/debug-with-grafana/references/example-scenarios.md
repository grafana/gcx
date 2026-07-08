# Example Scenarios

End-to-end command sequences for the three most common debugging entry points.
Each scenario maps to the numbered steps in SKILL.md.

## Contents

- [Scenario 1: HTTP 500 Error Spike](#scenario-1-http-500-error-spike)
- [Scenario 2: Latency Degradation](#scenario-2-latency-degradation)
- [Scenario 3: Service Down / No Data](#scenario-3-service-down--no-data)

---

## Scenario 1: HTTP 500 Error Spike

**Trigger**: User reports "my API started returning 500 errors 30 minutes ago".

**Command sequence:**

```bash
# Step 1: Find datasource UIDs
gcx datasources list -t prometheus -o json
gcx datasources list -t loki -o json

# Step 2: Confirm service is being scraped
gcx metrics query -d <prom-uid> 'up{job="api"}' -o json

# Step 3: Observe error rate over last 2 hours (wider window to see the spike start)
gcx metrics query -d <prom-uid> \
  'rate(http_requests_total{job="api",status=~"5.."}[5m])' \
  --from now-2h --to now --step 1m -o graph

# Identify which status codes are elevated
gcx metrics query -d <prom-uid> \
  'sum by(status) (rate(http_requests_total{job="api"}[5m]))' \
  --from now-2h --to now --step 1m -o json

# Step 4: Check if latency rose at the same time
gcx metrics query -d <prom-uid> \
  'histogram_quantile(0.95, rate(http_request_duration_seconds_bucket{job="api"}[5m]))' \
  --from now-2h --to now --step 1m -o graph

# Step 5: Get error logs in the spike window
gcx logs query -d <loki-uid> \
  '{job="api"} |= "error"' \
  --from now-2h --to now -o json

# Step 6: Check alert rules
gcx alert rules list -o json | jq '.[] | .rules[]? | select(.state == "firing")'
```

**Expected output shape at Step 3 (matrix):**
```json
{
  "status": "success",
  "data": {
    "resultType": "matrix",
    "result": [
      {
        "metric": {"job": "api", "status": "500"},
        "values": [[<timestamp>, "<rate>"], ...]
      }
    ]
  }
}
```

**Interpretation**: Look for the timestamp where `values` shows the rate
increasing from baseline. Match this to log timestamps in Step 5.

---

## Scenario 2: Latency Degradation

**Trigger**: User reports "requests are taking much longer than usual, no errors yet".

**Command sequence:**

```bash
# Step 1: Find datasource UIDs
gcx datasources list -t prometheus -o json

# Step 2: Confirm service health (latency without errors suggests slow dependency)
gcx metrics query -d <prom-uid> 'up{job="api"}' -o json

# Step 3: Error rate (confirm it's not elevated yet)
gcx metrics query -d <prom-uid> \
  'rate(http_requests_total{job="api",status=~"5.."}[5m])' \
  --from now-1h --to now --step 1m -o json

# Step 4: P95 latency is the primary signal - visualize trend
gcx metrics query -d <prom-uid> \
  'histogram_quantile(0.95, rate(http_request_duration_seconds_bucket{job="api"}[5m]))' \
  --from now-2h --to now --step 1m -o graph

# Break down by endpoint to isolate which routes are slow
gcx metrics query -d <prom-uid> \
  'histogram_quantile(0.95, sum by(le, handler) (rate(http_request_duration_seconds_bucket{job="api"}[5m])))' \
  --from now-1h --to now --step 1m -o json

# Step 5: Check for timeout log patterns suggesting upstream dependency issue
gcx logs query -d <loki-uid> \
  '{job="api"} |~ "timeout|slow|waiting"' \
  --from now-2h --to now -o json

# Check database or downstream service latency if metrics available
gcx metrics query -d <prom-uid> \
  'rate(db_query_duration_seconds_sum{job="api"}[5m]) / rate(db_query_duration_seconds_count{job="api"}[5m])' \
  --from now-2h --to now --step 1m -o json
```

**Expected output shape at Step 4 (histogram):**
```json
{
  "status": "success",
  "data": {
    "resultType": "matrix",
    "result": [
      {
        "metric": {"job": "api"},
        "values": [[<timestamp>, "<seconds>"], ...]
      }
    ]
  }
}
```

**Interpretation**: Rising `values` across all endpoints suggests a shared
resource or dependency. Rising values for one endpoint only suggests a
handler-specific issue. Compare latency onset time with log timestamps.

---

## Scenario 3: Service Down / No Data

**Trigger**: User reports "service seems completely down" or dashboard shows no data.

**Command sequence:**

```bash
# Step 1: Verify datasource connectivity first (simplest possible query)
gcx datasources list -o json

# Step 2: Check whether the service is being scraped at all
gcx metrics query -d <prom-uid> 'up{job="api"}' -o json

# Check if the job label exists at all (absence = service was never registered)
gcx metrics labels -d <prom-uid> -l job -o json

# Step 3: Without error rate data, check for recent data gaps
gcx metrics query -d <prom-uid> \
  'absent(up{job="api"})' \
  --from now-1h --to now --step 1m -o json

# Step 4: Query latency from any recent data before the outage
gcx metrics query -d <prom-uid> \
  'histogram_quantile(0.95, rate(http_request_duration_seconds_bucket{job="api"}[5m]))' \
  --from now-3h --to now --step 5m -o graph

# Step 5: Check Loki for last known logs before data disappeared
gcx logs query -d <loki-uid> \
  '{job="api"}' \
  --from now-3h --to now -o json

# Crash or OOM signals in logs
gcx logs query -d <loki-uid> \
  '{job="api"} |~ "panic|OOM|killed|crashed|SIGTERM"' \
  --from now-3h --to now -o json

# Step 6: Check alert rules for any firing service-down alerts
gcx alert rules list -o json | jq '.[] | .rules[]? | select(.state == "firing")'
```

**Expected output shape when service is down (up=0):**
```json
{
  "status": "success",
  "data": {
    "resultType": "vector",
    "result": [
      {
        "metric": {"__name__": "up", "job": "api", "instance": "<host:port>"},
        "value": [<timestamp>, "0"]
      }
    ]
  }
}
```

**Expected output shape when service was never scraped (absent):**
```json
{
  "status": "success",
  "data": {
    "resultType": "vector",
    "result": []
  }
}
```

**Interpretation**:
- `up=0`: Service is registered but failing health checks - check pod/process status
- Empty result for `up{job="api"}`: Job never existed or was removed from scrape config
- Data present up to a specific timestamp then absent: Service crashed at that time - correlate with crash logs
