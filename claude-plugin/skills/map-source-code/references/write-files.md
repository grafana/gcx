# Write files

Write through files (`-f`), not through the `--from` and `--to` flags. In a
file, the domain, type, name and scope are separate fields, and one command
writes many entries. The delete commands take flags only: see
[removals.yaml](#removalsyaml).

Put every string value that comes from a repository in double quotes, so a
name with `:` or `#` stays one YAML string.

Leave out `ttlSeconds`. gcx then writes the object with no expiry, and it
stays until a later run removes it. Writing an object again without
`ttlSeconds` also clears a TTL that an earlier write gave it.

## repositories.yaml

One entry per repository, in the `sourcecode` domain. A `Repository` has no
scope.

```yaml
- domain: sourcecode
  type: Repository
  name: "github.com/contoso/shop"
  properties:
    provider: github
    vcs: git
    host: "github.com"
    url: "https://github.com/contoso/shop"
    hosting: cloud
    owner: "contoso"
    fullName: "contoso/shop"
    defaultBranch: "main"
    managed_by: gcx-map-source-code
- domain: sourcecode
  type: Repository
  name: "github.com/contoso/deploy-config"
  properties:
    provider: github
    vcs: git
    host: "github.com"
    url: "https://github.com/contoso/deploy-config"
    hosting: cloud
    owner: "contoso"
    fullName: "contoso/deploy-config"
    managed_by: gcx-map-source-code
```

## resources.yaml

One entry per resource the services use, in the `sourcecode` domain. A
`CloudResource` has no scope: its name is the provider's full id. See
[Resources](resources.md) for names and properties.

```yaml
- domain: sourcecode
  type: CloudResource
  name: "arn:aws:sqs:us-east-1:111122223333:orders"
  properties:
    kind: sqs-queue
    provider: aws
    resource_name: "orders"
    arn: "arn:aws:sqs:us-east-1:111122223333:orders"
    account_id: "111122223333"
    region: "us-east-1"
    managed_by: gcx-map-source-code
- domain: sourcecode
  type: CloudResource
  name: "arn:aws:s3:::contoso-invoices"
  properties:
    kind: s3-bucket
    provider: aws
    resource_name: "contoso-invoices"
    arn: "arn:aws:s3:::contoso-invoices"
    managed_by: gcx-map-source-code
```

Quote an account id, as above, so YAML reads it as a string.

## Link files

One entry per link. The link itself is in the `sourcecode` domain. Put
repository links and resource links in separate files: a stack with an older
`sourcecode` schema rejects the resource links, and gcx stops a file at its
first failing entry.

- **`from`** is the telemetry Service. Use `domain: metrics` and copy the
  Service's scope exactly as Step 5 found it.
- **`to`** is the repository or the `CloudResource`, by its name.

### repository-links.yaml

```yaml
- domain: sourcecode
  type: BUILT_FROM
  from:
    domain: metrics
    type: Service
    name: "checkout"
    scope:
      env: "prod"
      namespace: "shop"
  to:
    domain: sourcecode
    type: Repository
    name: "github.com/contoso/shop"
  properties:
    path: "services/checkout"
    managed_by: gcx-map-source-code
- domain: sourcecode
  type: DEPLOYED_FROM
  from:
    domain: metrics
    type: Service
    name: "checkout"
    scope:
      env: "prod"
      namespace: "shop"
  to:
    domain: sourcecode
    type: Repository
    name: "github.com/contoso/deploy-config"
  properties:
    path: "environments/prod/checkout.yaml"
    managed_by: gcx-map-source-code
```

### component-links.yaml

A `BUILT_FROM` link from the service catalog `Component`, in place of the
per-Service `BUILT_FROM` links (see Step 5 of the skill). A Component has no
scope, so `from` has no `scope`. Copy the Component name from the query result.

```yaml
- domain: sourcecode
  type: BUILT_FROM
  from:
    domain: servicemodel
    type: Component
    name: "checkout"
  to:
    domain: sourcecode
    type: Repository
    name: "github.com/contoso/shop"
  properties:
    path: "services/checkout"
    managed_by: gcx-map-source-code
```

### resource-links.yaml

```yaml
- domain: sourcecode
  type: WRITES_TO
  from:
    domain: metrics
    type: Service
    name: "checkout"
    scope:
      env: "prod"
      namespace: "shop"
  to:
    domain: sourcecode
    type: CloudResource
    name: "arn:aws:sqs:us-east-1:111122223333:orders"
  properties:
    path: "services/checkout/internal/orders/publisher.go"
    managed_by: gcx-map-source-code
```

- `BUILT_FROM.path` is the folder or build module of the service.
- `DEPLOYED_FROM.path` is the file or folder that renders the service for that
  environment.
- If the same service runs in several environments, write one `BUILT_FROM` link
  per Service scope, or one link from its Component. Write `DEPLOYED_FROM` only
  for the environment each config deploys to.
- `READS_FROM.path` and `WRITES_TO.path` are the file or folder where the code
  reads or writes the resource. A resource is usually per environment, so link
  it only from the Service in that environment.
- Scope keys must match the Service exactly. A Service without `site` has no
  `site` key in `scope`.
- Write at most one link per link type, Service and target. A link is keyed on
  its type and its two ends, not on `path`, so a second entry with another
  `path` replaces the first. When several paths apply, pick one the same way on
  every run, so repeated runs do not flip it: for `BUILT_FROM`, the deepest
  folder that contains them all; for the other types, the first path in sorted
  order. List the other paths in the summary.

## removals.yaml

The stale links from Step 6 of the skill, and the resources that the user said
were deleted, in the same format as the link and entity files, without
`properties`. It is the record of what the run removes. The delete commands take flags, so run one command per entry:

```yaml
- domain: sourcecode
  type: DEPLOYED_FROM
  from:
    domain: metrics
    type: Service
    name: "checkout"
    scope:
      env: "prod"
      namespace: "shop"
  to:
    domain: sourcecode
    type: Repository
    name: "github.com/contoso/deploy-config"
- domain: sourcecode
  type: CloudResource
  name: "arn:aws:sqs:us-east-1:111122223333:orders"
```

- A link entry becomes `gcx kg relationships delete --type <type> --from
  '<domain>/<Type>/<name>' --from-scope '<key>=<value>,...' --to
  '<domain>/<Type>/<name>' --force`. The name in a reference keeps every `/`
  after the type, so `sourcecode/Repository/github.com/contoso/shop`
  names the repository `github.com/contoso/shop`.
- A resource entry becomes `gcx kg entities delete --domain sourcecode --type
  CloudResource --name '<name>' --force`.
- Put the links before the resources: removing a resource also removes its
  links.
