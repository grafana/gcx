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
The grant must allow every requested scope. An untrusted auto-discovered `.gcx.yaml`
cannot opt your job into this exchange.

The saved config contains only non-secret settings. Each gcx process obtains a
new GitHub OIDC token and exchanges it for a 15-minute Assistant credential.
Credentials stay in memory and renew before expiry. Authentication failures do
not trigger credential renewal and replay. The shared HTTP transport can retry
requests, including mutations, after rate limits or transient connection errors;
make mutating workflows idempotent. Unlinking the user or removing the workflow
grant stops further use.
The original signed workflow actor determines identity, including reruns.

For local development, run the focused auth/config/login tests with mock HTTP
servers. Real GitHub OIDC is only available inside a GitHub Actions job; a tunnel
can point that job at a local Assistant backend. Publishing the Action and smoke
workflow is a separate step from local testing.
