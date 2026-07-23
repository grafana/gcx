# Phases 0-3: Foundation

Bootstrap, discovery, test definitions, and instrumentation. These phases run sequentially - see SKILL.md for the dependency rules and the verification/idempotency principles that apply to every phase.

## Contents

- [Phase 0: Bootstrap](#phase-0-bootstrap)
- [Phase 1: Discovery & Context](#phase-1-discovery--context)
- [Phase 2: Test Definitions](#phase-2-test-definitions)
- [Phase 3: Instrumentation](#phase-3-instrumentation)

---

## Phase 0: Bootstrap

Mark task in_progress. Run sequentially (everything depends on this).

Run `gcx config check` to verify the stack is initialized and authenticated. Then run `gcx config view` to capture the stack URL and context details.

If not configured: ask for the Grafana instance URL and an API token (service account with Admin role), then set up a context:

```bash
gcx config set stacks.<name>.grafana.server <url>
gcx config set stacks.<name>.grafana.token <token>
gcx config set contexts.<name>.stack <name>
gcx config use-context <name>
gcx config check
```

Store: stack URL, context name. Mark task completed.

---

## Phase 1: Discovery & Context

Mark task in_progress. Ask the user all questions in a **single `AskUserQuestion` call** (don't ask one at a time):

1. Application name and brief description
2. K8s cluster(s) and namespaces
3. Frontend stack (React / Vue / Angular / vanilla JS / none)
4. Key user journeys (e.g. "login", "checkout", "search") - list them
5. Critical API endpoints for synthetic checks and load tests
6. On-call team structure (names/emails, time zones, escalation order)

Store all answers in memory - every subsequent phase references them. Mark task completed.

---

## Phase 2: Test Definitions

Mark task in_progress.

> **This is the test-driven foundation.** Before any infrastructure is deployed, define the contracts that describe a healthy system. These definitions will be referenced and validated in every subsequent phase.

**Pre-check - skip files that already exist:**
List local files matching `slo-*.yaml`, `k6-test-*.js`, `k6-schedule-*.yaml`, and `check-*.yaml`. For any files already present, skip writing them and use the existing versions in later phases. Only write files that are missing.

Ask the user a **single `AskUserQuestion`** to confirm/adjust these defaults:

- SLO targets per journey (default: 99.9% availability, p95 latency < 500ms over 28d)
- k6 load profile per endpoint (default: 10 VUs, 30s, p95 < 500ms threshold)
- k6 schedule (default: every 6 hours - schedules are required, not optional)
- Synthetic check frequency (default: 30s for critical, 60s for standard)
- Synthetic check assertions (default: status=200, latency < 500ms)

**Step 1 - Create SLO definitions (one per journey, parallel):**

For each journey `J` from Phase 1, launch an agent that:
- Discovers the SLO command group (`gcx slo --help`, `gcx slo definitions --help`) to find available subcommands and flags.
- Runs `gcx resources list-examples slo -o yaml` to get a template (the default text
  output is only a descriptor table), then customizes it: name, availability
  target, latency target, 28d window.
- Adds an `alerting` section with `fastBurn` and `slowBurn` entries under
  `spec.alerting`. The example template omits this section, and an SLO without
  it deploys fine but never generates burn-rate alert rules — the SLO plugin
  creates those rules server-side from this section, and Phase 4 only wires
  notification routing on top, so the omission surfaces late.
- Writes the result to `slo-J.yaml`.

Do **not** create the SLOs yet - Phase 4 does that after signals are flowing. Store all `slo-*.yaml` files for Phase 4.

**Step 2 - Create k6 test scripts (one per endpoint, parallel):**

For each critical endpoint from Phase 1, write a `k6-test-<endpoint>.js` script with:
- 10 VUs, 30s duration
- Thresholds: p95 latency < 500ms, failure rate < 1%
- A default function that hits the endpoint and checks status=200 and response time < 500ms

Also discover the k6 schedules command group (`gcx k6 schedules --help`) and run the schedules example subcommand if available. Customize it for a 6-hour frequency and write to `k6-schedule-<endpoint>.yaml`.

Store all scripts and schedule YAMLs for Phase 6.

**Step 3 - Create synthetic check definitions (one per endpoint, parallel):**

For each critical endpoint, discover the synthetic monitoring checks command group (`gcx synthetic-monitoring checks --help`) and check for an example subcommand. Customize it: target=real URL, frequency=30s for critical / 60s for standard, assertions: status=200 and latency < 500ms. Do NOT set basicMetricsOnly: true. Leave `alertSensitivity` unset or `none` — on unified-alerting stacks any other value is rejected with a 403 (legacy SM alerts). Write to `check-<endpoint>.yaml`.

Store all `check-*.yaml` files for Phase 5.

Show a summary of all test definitions created. Mark task completed.

---

## Phase 3: Instrumentation

Mark task in_progress.

**Pre-check - skip if already deployed:**
Run `gcx instrumentation status` to check current signal status. Also check whether Alloy pods are already running in the monitoring namespace using kubectl (list pods filtered by alloy labels). If Alloy is running and infrastructure signals are healthy, skip Step 1. If app signals are also healthy, skip the rest and mark the phase completed.

**Pre-check - application must be running in Kubernetes first.**

Before deploying Alloy, use kubectl to list deployments, daemonsets, and statefulsets in the application namespace. If no workloads are found, stop and ask the user to deploy their application first - autodiscovery and instrumentation will only detect workloads that exist at the time Alloy runs.

**Pre-check - container image compatibility for observability**

Scan the project's Dockerfiles for patterns that interfere with observability tooling (eBPF, profiling, log collection, debugging). For each Dockerfile found, check for and warn about:

- **Alpine / musl-based runtime**: breaks eBPF uprobe attachment (Beyla), profiling, and SSL inspection. Recommend `debian:bookworm-slim` or `ubuntu:24.04`.
- **Scratch / distroless runtime**: missing shared libraries, shell, and debug tools. Recommend a minimal Debian-based image instead.
- **Stripped binaries**: (`strip`, `-s -w` ldflags) removes symbol tables needed by eBPF and profilers. Recommend keeping symbols.
- **No signal handling**: missing `tini` or equivalent init - can cause zombie processes and missed graceful shutdown signals, leading to metric gaps.

If issues are found, list them and ask the user whether to fix before proceeding.

---

**Step 1 - Register Fleet collector configuration (sequential, everything else depends on this):**

Use `gcx fleet collectors --help` to discover collector management commands. Create an Alloy collector configuration record:

```bash
gcx fleet collectors create -f alloy-collector.yaml
```

Registering a collector or pipeline in Fleet Management only stores
configuration — **nothing is deployed to the cluster by this step**. The
actual deployment happens when the helm command printed by
`gcx instrumentation setup` (Step 2, Agent A) is run on the cluster, so do
not poll for signals or expect Alloy pods yet.

Use `gcx fleet pipelines --help` to discover pipeline management. List pipelines (`gcx fleet pipelines list`). If no pipeline contains an OTLP receiver on port 4317, create or update a pipeline record to add one:

```bash
gcx fleet pipelines create -f pipeline.yaml
# or
gcx fleet pipelines update <name> -f pipeline.yaml
```

Once Step 2's helm install has run on the cluster, use kubectl to verify Alloy pods are Running and Ready (not CrashLoopBackOff) and that a Service exposing port 4317 exists in the monitoring namespace; signal polling happens in Step 3.

---

**Step 2 - Parallel wave - launch all four agents simultaneously in one message:**

- **Agent A** - Instrumentation setup:
  Run `gcx instrumentation clusters list` to see all clusters and their current status.
  Run `gcx instrumentation clusters get <cluster>` to review the cluster's current config.
  Run `gcx instrumentation setup <cluster> --use-defaults` to configure K8s monitoring and print the helm install command to connect the cluster to Grafana Cloud via Fleet Management. (`--use-defaults` is required when stdin is not a TTY, which is always the case for agents.)
  To adjust individual feature flags after initial setup (RMW): `gcx instrumentation clusters configure <cluster> --cost-metrics --cluster-events`.
  Verify with `gcx instrumentation status`.

- **Agent B** - Fleet pipelines verification:
  List pipelines (`gcx fleet pipelines list`) to confirm pipeline exists and is receiving data.
  Verify collectors are healthy: `gcx fleet collectors list`.

- **Agent C** - Frontend observability (skip if no frontend stack from Phase 1):
  Discover the frontend command group (`gcx frontend --help`, `gcx frontend apps --help`).
  List existing Frontend Observability apps: `gcx frontend apps list`. If an app for this project already exists, skip creation.
  Otherwise, create a Frontend Observability app configured for the application URL and name:
  ```bash
  gcx frontend apps create -f faro-app.yaml
  ```
  Verify: `gcx frontend apps list` to confirm the app was created and capture the app ID.
  If the frontend uses sourcemaps, upload them: `gcx frontend apps apply-sourcemap <app-name> -f <sourcemap>`.

- **Agent D** - Synthetic checks (early deployment for traffic seeding):
  Deploy the `check-*.yaml` files from Phase 2 now, before instrumentation is fully verified. For each endpoint, check if the check already exists (`gcx synthetic-monitoring checks list`); if not, create it: `gcx synthetic-monitoring checks create -f check-<endpoint>.yaml`. List checks to confirm each is enabled with probes assigned. After each create, read the check back with `gcx synthetic-monitoring checks get <id> -o json` and confirm `basicMetricsOnly` persisted as intended — if the server keeps it `true`, report that full metrics are unavailable for that check rather than re-submitting.
  > **Purpose:** synthetic checks start probing endpoints immediately, generating real HTTP traffic that flows through Alloy. This seeds the telemetry pipeline so Step 3's signal verification has live data.
  > If endpoints are private, first list available probes (`gcx synthetic-monitoring probes list`), identify private probes, and ensure they are online before creating checks.

Wait for all four agents. Report combined results.

> **Note:** For SDK-based instrumentation, use the OTLP endpoint reported by the collector configuration. No additional credentials needed - apps send OTLP to Alloy's in-cluster endpoint.

---

**Step 3 - Verify app signals are flowing after instrumentation:**
Poll `gcx instrumentation status` (timeout 5 minutes). Synthetic checks deployed in Step 2 should already be generating traffic, making this verification reliable. If this times out, check that instrumentation was applied correctly and that synthetic checks are active and targeting the correct endpoints.

Mark task completed.

After Wave A (Phases 4-6, 8-9) completes, do a final signal check using `gcx instrumentation status`. If signals are unhealthy, check that app deployments have OTEL instrumentation and are sending to Alloy.
