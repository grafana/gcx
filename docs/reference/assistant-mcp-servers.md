---
title: Assistant MCP servers
---

# Assistant MCP servers

`gcx assistant mcp-servers` manages remote Model Context Protocol (MCP) server integrations for Grafana Assistant in the current Grafana Cloud stack.

Use this command group when you need to list, inspect, create, update, or remove the MCP servers that Assistant can call for tools.

## Prerequisites

The current gcx context must point at a Grafana Cloud stack with Grafana Assistant available. Configure the context with `gcx login` or another supported Grafana authentication method before running MCP server commands.

## Scopes

MCP server integrations support two scopes:

| Scope | Grafana UI label | Use for |
|-------|------------------|---------|
| `user` | Just me | OAuth-based servers and personal MCP servers |
| `tenant` | Everybody | Shared MCP servers authenticated with headers |

The default scope is `user`. GitHub Copilot MCP is OAuth-based, so it should remain user-scoped:

```sh
gcx assistant mcp-servers create \
  --name GitHub \
  --url https://api.githubcopilot.com/mcp
```

When Grafana reports that OAuth is required, gcx initiates the Assistant OAuth flow and opens the authorization URL in your browser. If the browser cannot open automatically, use the URL printed by the command.

## Header authentication

Tenant-scoped MCP servers are shared by the stack and must provide at least one non-empty authentication header. The CLI accepts repeatable `--header NAME=VALUE` flags.

```sh
gcx assistant mcp-servers create \
  --name SharedTools \
  --url https://mcp.example.com/mcp \
  --scope tenant \
  --header "Authorization=Bearer <token>"
```

Recognized authentication headers include:

| Header |
|--------|
| `Authorization` |
| `X-API-Key` |
| `API-Key` |
| `X-Auth-Token` |
| `X-Grafana-API-Key` |
| `X-CH-Auth-API-Token` |

Header values are sent to Grafana during create or update, but stored values are not returned by the API. Existing tenant-scoped servers can be updated without re-supplying hidden header values:

```sh
gcx assistant mcp-servers update SharedTools \
  --description "Shared internal MCP tools"
```

Changing a user-scoped server to tenant scope requires a new authentication header value:

```sh
gcx assistant mcp-servers update LocalTools \
  --scope tenant \
  --header "X-API-Key=<token>"
```

## File input

Create and update commands can read YAML or JSON input with `--file`.

```yaml
name: SharedTools
description: Shared internal MCP tools
url: https://mcp.example.com/mcp
scope: tenant
enabled: true
applications:
  - assistant
headers:
  - name: Authorization
    value: Bearer <token>
```

Apply the file with:

```sh
gcx assistant mcp-servers create --file server.yaml --if-not-exists
```

## Output formats

`list` defaults to text table output:

```sh
gcx assistant mcp-servers list
```

Use `wide` to include scope and applications, or use structured output for automation:

```sh
gcx assistant mcp-servers list --output text
gcx assistant mcp-servers list --output table
gcx assistant mcp-servers list --output wide
gcx assistant mcp-servers list --output json
gcx assistant mcp-servers get GitHub --output yaml
```

Supported list formats are `text`, `table`, `wide`, `json`, `yaml`, and `agents`.

For `json` and `yaml`, `list` and `get` emit the same `{apiVersion, kind, metadata, spec}` K8s-envelope shape as `gcx resources get mcpservers` — see [Resources pipeline (GitOps)](#resources-pipeline-gitops) below.

## Delete

Delete prompts for confirmation by default. `--force` bypasses the prompt, `GCX_AUTO_APPROVE=1` auto-approves in non-interactive workflows, and agent mode still requires explicit `--force`.

```sh
gcx assistant mcp-servers delete GitHub --force
```

## Resources pipeline (GitOps)

MCP servers are also addressable as a `gcx resources` type (`mcpservers`, GVK `assistant.ext.grafana.app/v1alpha1`), so they can be pulled, versioned, and pushed like any other resource:

```sh
gcx resources pull mcpservers
gcx resources get mcpservers/tenant-github -o yaml
gcx resources push mcpservers/tenant-github.yaml
```

`metadata.name` is computed as `{scope}-{slug(name)}`; matching across stacks uses the natural key `(scope, name, url)`, not the server-assigned ID. On `push`, manifest headers follow the same explicit write-intent model as the command path (a header with a value overwrites, a name-only header preserves the stored secret on update, and an omitted header is removed) — with `fromEnv`/`fromFile` sourcing available for CI. See [ADR-021](../adrs/assistant-provider/001-assistant-provider-and-mcp-servers-as-resources.md) for the full design.

## CLI reference

For the generated command reference, see [`gcx assistant mcp-servers`](./cli/gcx_assistant_mcp-servers.md).
