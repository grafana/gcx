---
name: map-source-code
description: >-
  Links services to the repositories that build and deploy them and to the
  buckets, queues and tables their code uses: scans local checkouts and writes
  the links to the Knowledge Graph sourcecode schema. Not for debugging missing
  telemetry entities or edges (use diagnose-entity-graph).
---

# Map Source Code to the Knowledge Graph

Read local repositories, find the services they build or deploy and the
resources those services use, match each service to a Service that the
Knowledge Graph already knows, and write the links. The result in the graph:

- one `Repository` entity per repository, in the `sourcecode` domain
- `Service -[BUILT_FROM]-> Repository` for application code
- `Service -[DEPLOYED_FROM]-> Repository` for deployment config (Helm,
  Kustomize, Tanka/jsonnet, Terraform, plain manifests)
- one `CloudResource` entity per bucket, queue, topic or table the code uses,
  in the `sourcecode` domain
- `Service -[READS_FROM]-> CloudResource` and
  `Service -[WRITES_TO]-> CloudResource`

## Rules

- **Read the code, never change it.** Do not edit, build, commit or push in the
  scanned repositories, and do not run their scripts.
- **Send only metadata.** What goes to the Knowledge Graph is repository URLs,
  folder and file paths, service names, and resource ids (names, ARNs, queue
  URLs). Never copy file contents, secrets, credentials, connection strings or
  other configuration values into an entity or a link.
- **Link only from Services that exist.** Services come from telemetry. This
  skill does not create them, and the write API rejects writes to the telemetry
  domain.
- **Confirm before writing.** Show the plan and wait for the user's go-ahead
  before the first `upsert`.
- The write commands are experimental. Their flags and responses may change.

## Step 1: Check that the stack accepts writes

```bash
gcx kg status
```

- `kgWriteApiEnabled: true` means you can write. Go on.
- `kgWriteApiEnabled: false` means this stack cannot write yet: the Knowledge
  Graph write API is not enabled for it. Stop. You can still prepare the plan
  (Steps 2 to 6) so the user can apply it later.
- No `kgWriteApiEnabled` field means the server is too old to report it. Treat
  it as "unknown": prepare the plan and ask the user before you try to write.
- If `status` is not `complete` or `enabled` is `false`, the Knowledge Graph
  itself is not ready. Stop and say so.

The stack also needs the `sourcecode` system schema. There is no read-only check
for it: if the first write answers 422 about an unknown domain or type, the
schema is not installed on this stack yet. Stop and say so.

## Step 2: Identify the repositories

Ask which local checkouts to scan if the user has not said. For each one:

```bash
git -C <dir> remote get-url origin
git -C <dir> symbolic-ref --short refs/remotes/origin/HEAD
```

Turn the remote URL into the canonical repository name and the `Repository`
properties as described in
[Repository identity](references/repository-identity.md). The canonical name is
host and path, for example `github.com/contoso/shop`.

## Step 3: Find the services in the code

Look for things that run as a service, and the name each one has in telemetry:

- **Application code** (`BUILT_FROM`): Dockerfiles and their build context,
  `main` packages, application modules, OpenTelemetry `service.name` settings.
  The link's `path` is the folder or module, for example `services/checkout`.
- **Deployment config** (`DEPLOYED_FROM`): Kubernetes manifests, Helm charts and
  values, Kustomize overlays, Tanka/jsonnet environments, Terraform. The link's
  `path` is the file or folder that renders the service, usually one per
  environment.

A repository can be both. Write down, for each service: the name it is likely to
have in telemetry (workload name, container name, `service.name`), and for
deployment config, the environment and namespace it deploys to.

## Step 4: Find the resources each service uses

Look for buckets, queues, topics and tables that each service reads or writes:
infrastructure code, IAM policies, SDK calls, and the environment variables and
values in the deploy config. For each resource, find its full id for each
environment and whether the service reads it, writes it, or both.

[Resources](references/resources.md) lists where to look, how to tell read from
write, and the name and properties for each kind.

- Write a `CloudResource` for every resource whose full id resolves, whether or
  not telemetry knows it. Do not look it up first, and never link to a
  telemetry entity such as `S3Bucket` instead.
- A resource whose id does not resolve to fixed values (built at runtime, a
  wildcard, unknown account or region) is not written. List it as unresolved,
  with the file where you saw it.

## Step 5: Match each service to a Knowledge Graph Service

Find the Service and its exact scope. Scope is part of a Service's identity, so
an edge needs the same `env`, `namespace` and `site` values as the Service:

```bash
gcx kg meta scopes
gcx kg entities list --type Service --env <env> --namespace <namespace> --property name=~<part-of-name>
```

For several names at once, use Cypher. Keep `LIMIT` at 300 or lower:

```bash
gcx kg graph query "MATCH (s:Service) WHERE s.name IN ['checkout', 'cart'] RETURN s.name, s.env, s.namespace, s.site LIMIT 300"
```

- **One match:** use it.
- **Several matches in different environments:** a `BUILT_FROM` link is the same
  for each, so add one link per Service. A `DEPLOYED_FROM` link goes only to
  the environment that config deploys to. A `READS_FROM` or `WRITES_TO` link
  goes only from the Service in the environment that uses that resource.
- **No match:** do not write a link. List the service as unmatched, with the
  names you tried. Do not write the resources that only it uses.

## Step 6: Prepare the plan

Write three files in a working folder outside the scanned repositories:
`repositories.yaml` (the `Repository` entities), `resources.yaml` (the
`CloudResource` entities) and `links.yaml` (the relationships). The exact
format is in [Write files](references/write-files.md).

- Set `ttlSeconds` on every object. The default is 7 days (`604800`). A link
  or resource that a later run does not write again then expires once the code
  no longer supports it.
- Set `managed_by: gcx-map-source-code` on every entity and link. A team can
  then find and clean up its own writes.

Show the user a short summary: the repositories, the resources, each link
(Service with scope, link type, repository or resource, path), the unmatched
services and the unresolved resources. Wait for approval.

## Step 7: Write

Write the entities first, then the links. A link fails if its repository or
resource does not exist yet.

```bash
gcx kg entities upsert -f repositories.yaml
gcx kg entities upsert -f resources.yaml
gcx kg relationships upsert -f links.yaml
```

Each file entry is written separately, so one failure does not undo the others.
Upserts are idempotent, so running the same files again is safe.

| Answer | Meaning | What to do |
|---|---|---|
| 403 | The write API is not enabled for this stack | Stop. Keep the files for later. |
| 422 that names the domain or `Repository` | The `sourcecode` schema is not installed on this stack | Stop and say so. |
| 422 that names `CloudResource`, `READS_FROM` or `WRITES_TO` | The stack has an older `sourcecode` schema without resources | Skip `resources.yaml` and the resource links. Write the rest and say so. |
| 422 or 404 about an endpoint | No Service with that name and scope | Check the match from Step 5 and fix that entry. |
| 409 | Graph writes are paused, or the graph does not exist yet | Try again later. |

## Step 8: Check the result

Query one relationship type at a time:

```bash
gcx kg graph query "MATCH (s:Service)-[r:BUILT_FROM]->(repo:Repository) RETURN s.name, s.env, repo.name, r.path LIMIT 100" --since 1h
gcx kg graph query "MATCH (s:Service)-[r:DEPLOYED_FROM]->(repo:Repository) RETURN s.name, s.env, repo.name, r.path LIMIT 100" --since 1h
gcx kg graph query "MATCH (s:Service)-[r:READS_FROM]->(res:CloudResource) RETURN s.name, s.env, res.name, r.path LIMIT 100" --since 1h
gcx kg graph query "MATCH (s:Service)-[r:WRITES_TO]->(res:CloudResource) RETURN s.name, s.env, res.name, r.path LIMIT 100" --since 1h
```

The links are in the `sourcecode` domain. Default views of a Service hide nodes
from other domains, so a `Repository` or `CloudResource` may not appear next to
its Service in the entity graph UI even when the link exists.

When telemetry also has an AWS resource, the Knowledge Graph links it to the
`CloudResource` with `DEPLOYMENT_OF` on the next model build. This needs no
write from you, and may take a while to appear.

## Keeping links fresh

Run the skill again after changes to the code or deployment config, or on a
schedule, for example in CI after each merge. Each run writes the links and
resources again, which renews their TTL. Links to code that moved or was
deleted, and resources no code uses any more, are not written again and expire.
