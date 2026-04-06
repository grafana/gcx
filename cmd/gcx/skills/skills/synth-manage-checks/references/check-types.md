# Check Type Reference

## Decision Tree

```
What is the target?
├── URL (https:// or http://)
│   ├── Need to trace routing/hops? → Traceroute
│   └── Standard availability/response? → HTTP
├── Hostname or IP address (no port)
│   └── → Ping
├── Domain name (DNS resolution check)
│   └── → DNS
└── host:port (TCP connectivity)
    └── → TCP
```

## HTTP Check

Tests URL availability, response codes, response time, and optional content matching.

```yaml
apiVersion: syntheticmonitoring.ext.grafana.app/v1alpha1
kind: Check
metadata:
  name: my-api-http         # non-numeric = create
spec:
  job: my-api-http
  target: https://api.example.com/health
  frequency: 60000          # 60s
  timeout: 10000            # 10s, must be < frequency
  enabled: true
  labels:
    - name: environment
      value: production
  probes:
    - Atlanta
    - Frankfurt
    - Singapore
  alertSensitivity: medium
  basicMetricsOnly: false
  settings:
    http:
      validStatusCodes: [200]    # empty = accept 2xx/3xx
      validHTTPVersions: ["HTTP/1.1", "HTTP/2.0"]
      method: GET
      noFollowRedirects: false
      tlsConfig:
        insecureSkipVerify: false
      headers: {}
      body: ""
      failIfNotSSL: false
      failIfSSL: false
```

Key HTTP fields:
- `validStatusCodes`: acceptable HTTP status codes; empty = default (2xx/3xx)
- `failIfNotSSL`: fail if response is not HTTPS
- `noFollowRedirects`: `true` to test redirect behavior
- `body`: POST body; set `method: POST` for non-GET

## Ping Check

Tests ICMP reachability and round-trip time to a hostname or IP.

```yaml
apiVersion: syntheticmonitoring.ext.grafana.app/v1alpha1
kind: Check
metadata:
  name: my-server-ping
spec:
  job: my-server-ping
  target: 10.0.1.50          # hostname or IP, no port
  frequency: 60000
  timeout: 10000
  enabled: true
  labels:
    - name: environment
      value: production
  probes:
    - Atlanta
    - Frankfurt
    - Singapore
  alertSensitivity: medium
  basicMetricsOnly: false
  settings:
    ping:
      packetCount: 3
      payloadSize: 0         # 0 = default (56 bytes)
      dontFragment: false
```

Key Ping fields:
- `packetCount`: ICMP packets per check run (1–20)
- `dontFragment`: `true` to test MTU path

## DNS Check

Tests DNS resolution for a domain and validates the response.

```yaml
apiVersion: syntheticmonitoring.ext.grafana.app/v1alpha1
kind: Check
metadata:
  name: my-domain-dns
spec:
  job: my-domain-dns
  target: example.com        # domain to resolve
  frequency: 120000          # 2min typical for DNS
  timeout: 10000
  enabled: true
  labels:
    - name: environment
      value: production
  probes:
    - Atlanta
    - Frankfurt
    - Tokyo
  alertSensitivity: medium
  basicMetricsOnly: false
  settings:
    dns:
      recordType: A           # A, AAAA, CNAME, MX, NS, SOA, TXT
      server: ""              # empty = use probe default resolver
      port: 53
      protocol: UDP           # UDP or TCP
      validRCodes: ["NOERROR"]
      validateAnswerRRS:
        failIfMatchesRegexp: []
        failIfNotMatchesRegexp: []
```

Key DNS fields:
- `recordType`: DNS record type to query
- `server`: custom resolver; empty = probe default
- `validRCodes`: acceptable return codes; `NOERROR` = success

## TCP Check

Tests TCP connectivity to a host:port endpoint.

```yaml
apiVersion: syntheticmonitoring.ext.grafana.app/v1alpha1
kind: Check
metadata:
  name: my-db-tcp
spec:
  job: my-db-tcp
  target: db.example.com:5432   # must be host:port format
  frequency: 60000
  timeout: 10000
  enabled: true
  labels:
    - name: environment
      value: production
  probes:
    - Atlanta
    - Frankfurt
    - Singapore
  alertSensitivity: high         # database connectivity is critical
  basicMetricsOnly: false
  settings:
    tcp:
      tls: false
      tlsConfig:
        insecureSkipVerify: false
      queryResponse: []          # protocol-level send/expect pairs
```

Key TCP fields:
- `tls`: `true` to initiate TLS handshake after TCP connect
- `queryResponse`: send/expect pairs for protocol validation

## Traceroute Check

Traces the network path to a URL or hostname, measuring per-hop latency.

```yaml
apiVersion: syntheticmonitoring.ext.grafana.app/v1alpha1
kind: Check
metadata:
  name: my-api-traceroute
spec:
  job: my-api-traceroute
  target: https://api.example.com   # URL or hostname
  frequency: 300000                 # 5min; traceroute is slower
  timeout: 30000                    # higher timeout for traceroute
  enabled: true
  labels:
    - name: environment
      value: production
  probes:
    - Atlanta
    - Frankfurt
    - Singapore
  alertSensitivity: low
  basicMetricsOnly: false
  settings:
    traceroute:
      maxHops: 64          # maximum hops to trace
      ptrLookup: false     # true = resolve hop IPs via PTR records
      hopTimeout: 500      # ms per hop
```
