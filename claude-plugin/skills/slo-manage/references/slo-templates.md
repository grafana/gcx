# SLO YAML Templates

Use `apiVersion: slo.ext.grafana.app/v1alpha1` and `kind: SLO` for all SLO definitions.

---

## Ratio Query Template

Use when measuring a success/total ratio (e.g., HTTP success rate, request availability).

```yaml
apiVersion: slo.ext.grafana.app/v1alpha1
kind: SLO
metadata:
  name: ""                          # empty = create new; UUID = upsert existing
spec:
  name: "api-availability"          # human-readable name shown in UI
  description: "HTTP API availability measured by 2xx/total request ratio"
  query:
    type: ratio
    ratio:
      successMetric:
        prometheusMetric: http_requests_total{status!~"5.."}  # requests that succeeded
      totalMetric:
        prometheusMetric: http_requests_total                 # all requests
      groupByLabels:                # optional: enables dimensional breakdown
        - cluster
        - service
  objectives:
    - value: 0.999                  # 99.9% target (range: 0.9–0.9999 typical)
      window: 28d                   # rolling window: 7d | 14d | 28d | 30d
  labels:                           # optional: for filtering and grouping
    - key: team
      value: platform
    - key: tier
      value: critical
  alerting:
    fastBurn:                       # pages on-call: high burn rate
      annotations:
        - key: runbook_url
          value: https://github.com/myorg/runbooks/blob/main/api-slo.md
        - key: summary
          value: "SLO fast burn: API availability burning error budget rapidly"
    slowBurn:                       # creates ticket: gradual degradation
      annotations:
        - key: runbook_url
          value: https://github.com/myorg/runbooks/blob/main/api-slo.md
        - key: summary
          value: "SLO slow burn: API availability degrading over time"
  destinationDatasource:
    uid: <prometheus-datasource-uid> # resolve with: gcx datasources list --type prometheus
  folder:
    uid: <folder-uid>               # optional: omit to use root folder
```

---

## Freeform Query Template

Use when you have a raw PromQL expression that directly expresses the SLI (must return 0–1).

```yaml
apiVersion: slo.ext.grafana.app/v1alpha1
kind: SLO
metadata:
  name: ""
spec:
  name: "checkout-latency-slo"
  description: "Checkout requests completing under 500ms (freeform PromQL)"
  query:
    type: freeform
    freeform:
      # PromQL expression that returns 0.0–1.0 representing the SLI
      # Must be a ratio of good events / total events
      # REQUIRED: use $__rate_interval in all rate()/increase() calls — literal ranges (e.g. [5m]) are rejected by the SLO API
      query: >
        sum(rate(http_request_duration_seconds_bucket{job="checkout",le="0.5"}[$__rate_interval]))
        /
        sum(rate(http_request_duration_seconds_count{job="checkout"}[$__rate_interval]))
  objectives:
    - value: 0.95                   # 95% of requests complete under 500ms
      window: 28d
  labels:
    - key: team
      value: checkout
  alerting:
    fastBurn:
      annotations:
        - key: runbook_url
          value: https://github.com/myorg/runbooks/blob/main/checkout-latency.md
        - key: summary
          value: "SLO fast burn: checkout latency SLO burning error budget"
    slowBurn:
      annotations:
        - key: runbook_url
          value: https://github.com/myorg/runbooks/blob/main/checkout-latency.md
        - key: summary
          value: "SLO slow burn: checkout latency degrading"
  destinationDatasource:
    uid: <prometheus-datasource-uid>
```

---

## Threshold Query Template

Use when measuring whether a metric stays above or below a fixed threshold.

```yaml
apiVersion: slo.ext.grafana.app/v1alpha1
kind: SLO
metadata:
  name: ""
spec:
  name: "database-availability"
  description: "Database instances reporting up (threshold: up >= 1)"
  query:
    type: threshold
    threshold:
      thresholdExpression: "up{job='postgres'}"  # metric to evaluate
      threshold:
        value: 1.0                  # threshold value
        operator: gte               # operator: gte | lte | gt | lt
      groupByLabels:
        - instance
        - cluster
  objectives:
    - value: 0.9999                 # 99.99% of time threshold is met
      window: 28d
  labels:
    - key: team
      value: data
    - key: tier
      value: critical
  alerting:
    fastBurn:
      annotations:
        - key: runbook_url
          value: https://github.com/myorg/runbooks/blob/main/db-availability.md
        - key: summary
          value: "SLO fast burn: database availability threshold breached"
    slowBurn:
      annotations:
        - key: runbook_url
          value: https://github.com/myorg/runbooks/blob/main/db-availability.md
        - key: summary
          value: "SLO slow burn: database availability degrading"
  destinationDatasource:
    uid: <prometheus-datasource-uid>
  folder:
    uid: <folder-uid>
```
