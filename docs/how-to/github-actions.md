# Use gcx in GitHub Actions

This authentication mode requires an Assistant deployment with GitHub Actions
exchange enabled and an explicit grant for your repository, workflow ref, event,
GitHub user ID, stack ID, and scopes. Link your GitHub account to your Grafana user
in Assistant first. Workflows run with that user's permissions.

```yaml
permissions:
  contents: read
  id-token: write

jobs:
  assistant:
    runs-on: ubuntu-latest
    steps:
      # Replace GCX_COMMIT_SHA with a reviewed full commit SHA containing this Action.
      - uses: grafana/gcx@GCX_COMMIT_SHA
        with:
          tenant-id: '12345'
          assistant-endpoint: https://YOUR_ASSISTANT_BACKEND
          scopes: assistant:a2a,grafana-api:read
      - run: gcx api /api/user
      - run: gcx assistant prompt 'Reply with hello. Do not call tools.'
```

The Action builds gcx from the pinned Action revision using Go on the runner.
An arbitrary commit pin need not have a release artifact, and released versions
do not yet contain this authentication mode. The installer selects release
versions, not arbitrary source commits; using it here could run a different
revision than the reviewed Action. Source builds cost toolchain/module downloads
on a cold runner. A future binary path needs a verified release-to-commit mapping.
Use `actions/setup-go` first if Go is unavailable; the required version is declared
in gcx's `go.mod`. The Action adds gcx to PATH and exports an isolated `GCX_CONFIG`
for subsequent steps, then removes its temporary directory in the post-job step.
It does not change your existing gcx configuration. Hosted Ubuntu runners include
Go; source builds use Go's automatic toolchain selection.

For an existing gcx installation, the equivalent login is:

```sh
gcx login --github-actions \
  --tenant-id 12345 \
  --assistant-endpoint https://YOUR_ASSISTANT_BACKEND \
  --scopes assistant:a2a,grafana-api:read \
  --config "$RUNNER_TEMP/gcx.yaml"
```

Select that same explicit config for subsequent commands. The job must grant
`id-token: write`. No browser login or long-lived Grafana token is needed. Remove
`GRAFANA_TOKEN` and `GRAFANA_CLOUD_TOKEN` from the job when selecting this mode.
Possible scopes are `assistant:a2a`, `assistant:chat`, `grafana-api:read`,
`grafana-api:write`, and `grafana-api:delete`; request only the scopes you need.
These scope names are validated by gcx; using a newly introduced backend scope
requires a gcx version that supports it. The grant must allow every requested
scope. An untrusted auto-discovered `.gcx.yaml` cannot opt your job into this
exchange.

The exchange contract is all-or-nothing: the backend issues every requested scope
or denies the request. It returns that exact scope set and its configured public
API base URL, including the routing path, with a trailing slash ignored. Configure
`--assistant-endpoint` to that same URL. Same-host alternate paths are not treated
as interchangeable backends, and the client never follows an endpoint returned
by the exchange. `tenant-id` matches the Assistant grant and request terminology;
it identifies the Grafana stack and is not a GitHub organization ID.

The saved config contains only non-secret settings. Each gcx process obtains a
new GitHub OIDC token and exchanges it for a 15-minute Assistant credential.
Credentials stay in memory and renew before expiry. Authentication failures do
not trigger credential renewal and replay. The shared HTTP transport can retry
requests, including mutations, after rate limits or transient connection errors;
make mutating workflows idempotent. Unlinking the user or removing the workflow
grant stops further use.
The original signed workflow actor determines identity, including reruns.

Existing Assistant-proxy consumers use this mode too: k6 routes through the
Grafana plugin proxy, and Synthetic Monitoring discovers its API URL through
Grafana plugin settings. Product-specific credentials and permission checks
still apply; OIDC does not supply a Grafana Cloud API token. `gcx dev serve` may
use this mode inside a job: it keeps the Assistant proxy path and renews in memory,
without the persistent refresh-token rotation that prevents browser OAuth there.
The server stops authenticating when the job can no longer obtain OIDC tokens.
The main reverse proxy reuses its in-memory client. Dashboard HTML requests
(`/d/{uid}/{slug}`) currently construct a client per request and therefore perform
a fresh OIDC exchange on each load; account for this when using a limited backend.
An explicit loopback HTTP backend is accepted for local tests; remote backends
require HTTPS. Shared HTTP debug logs include request URLs and status codes;
exchange request/response bodies and authorization headers are not dumped.

For local development, run the focused auth/config/login tests with mock HTTP
servers. Real GitHub OIDC is only available inside a GitHub Actions job; a tunnel
can point that job at a local Assistant backend. Publishing the Action and smoke
workflow is a separate step from local testing.
