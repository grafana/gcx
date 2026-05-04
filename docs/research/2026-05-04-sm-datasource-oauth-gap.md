# OAuth gap: gcx → SM datasource plugin proxy returns 403

**Audience:** assistant-app maintainers; whoever owns the OAuth-scope → Grafana
RBAC-action mapping.

**Asking:** can OAuth-authed gcx clients be granted access to the
`grafana-synthetic-monitoring-app` plugin-proxy routes?

## Summary

When gcx is authenticated via the standard `gcx login` OAuth flow and tries to
call the Synthetic Monitoring datasource proxy at
`/api/datasources/proxy/uid/<sm-ds-uid>/sm/<path>`, Grafana returns:

```
HTTP 403 — {"message":"plugin proxy route access denied"}
```

The same call with a **service account token (Admin role)** succeeds end-to-end
and returns real probe data — confirming Grafana plugin-proxy RBAC and the SM
plugin's `plugin.json` route declarations are both sound. The gap is somewhere
in the OAuth-bearer → Grafana-RBAC translation.

## Why it matters

The Synthetic Monitoring team is moving SM management logic from the SM app
into the SM datasource. The datasource becomes the canonical control plane;
ecosystem tools (gcx, dashboards, integrations) target the datasource because
that's the surface that survives the migration.

OAuth is the default `gcx login` mode for users post the recent login
improvements. If OAuth-authed clients can't reach the SM datasource proxy,
"datasource as source of truth" doesn't reach the typical user — only those
willing to provision and manage a service account token.

This blocks an in-flight gcx sprint deliverable (`gcx datasources query
<sm-uid> probes/checks`) for the OAuth path. We're shipping the SAT path
this week regardless; OAuth would be an automatic follow-up the moment the
scope mapping is fixed.

## Reproduction

Two contexts on the same stack — one OAuth, one SAT (Admin role):

```bash
# OAuth login (default path)
gcx login --context=<oauth-context> --server https://<stack>.grafana.net

# SAT login
gcx login --context=<sat-context> --server https://<stack>.grafana.net --token <SAT>

# Same proxy URL, two different bearers:
gcx --context=<oauth-context> api -X GET '/api/datasources/proxy/uid/<sm-ds-uid>/sm/probe/list'
# → HTTP 403 plugin proxy route access denied

gcx --context=<sat-context> api -X GET '/api/datasources/proxy/uid/<sm-ds-uid>/sm/probe/list'
# → HTTP 200, [{...probe...}, ...]
```

Confirmed against `<stack>.grafana.net` on 2026-05-04.

## What we ruled out

| Hypothesis | How ruled out |
|------------|---------------|
| Grafana plugin-proxy RBAC fundamentally rejects this route | SAT with Admin clears 403 — Grafana *will* allow the call given the right identity |
| SM `plugin.json` `reqAction`s are misconfigured | Same SAT proof — the route accepts an Admin bearer |
| URL shape is wrong | 403 (not 404) implies route is registered; SAT call against same URL returns 200 |
| Assistant-proxy is dropping/rewriting the request | Other `/api/...` paths via OAuth (`/api/datasources/uid/<uid>`, `/api/plugins/<id>/settings`) reach Grafana fine and return 200 — the proxy is forwarding; Grafana is the one denying |
| Server-side `secureJsonData.accessToken` injection broken | SAT call returned real SM probe data, so injection works — same Grafana code path runs for the OAuth call |

## What we suspect

The OAuth bearer presented to Grafana resolves to an identity whose RBAC
permissions don't satisfy the SM plugin-proxy route's `reqAction`. Likely
candidates for the missing action:

- `plugins.app:access` against `grafana-synthetic-monitoring-app`, and/or
- An SM-specific action declared in the plugin's `plugin.json` route table

The current OAuth scopes gcx requests are
`grafana-api:read,grafana-api:write,grafana-api:delete,assistant:a2a,assistant:chat`.
Whatever scope→action map the assistant-proxy applies, those scopes apparently
don't translate to the action(s) the SM plugin route requires.

## Open questions

1. How does the assistant-proxy translate gcx OAuth scopes into Grafana RBAC
   actions? Is there a documented scope→action allowlist we could extend?
2. Should SM plugin-proxy access be bound to an existing scope (e.g.,
   `grafana-api:read`) or warrant a dedicated new scope (e.g.,
   `grafana-plugin-proxy:access`)?
3. Are there other plugin-proxy routes already accessible to OAuth-authed gcx
   clients today? If yes, what's different about SM's declaration vs theirs?
4. Is the right fix in the assistant-proxy's mapping, in the SM plugin's
   `reqAction` declarations, or in Grafana RBAC defaults? We don't have a
   strong opinion; happy to defer to whoever owns the layer.

## References

- `docs/research/2026-05-04-sm-datasource.md` — original gap analysis (mem)
- `docs/research/2026-05-04-sm-datasource-summary.md` — auth-path findings
