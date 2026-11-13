# Write files

Write through files (`-f`), not through the `--from` and `--to` flags. A flag
reference is `domain/Type/name` with exactly three parts, so a repository name
that contains `/` does not fit. In a file, the domain, type and name are
separate fields.

Every object needs `ttlSeconds`. Without it, gcx writes the object with no
expiry. Use `604800` (7 days) unless the user asks for another value.

## repositories.yaml

One entry per repository, in the `sourcecode` domain. A `Repository` has no
scope.

```yaml
- domain: sourcecode
  type: Repository
  name: github.com/contoso/shop
  properties:
    provider: github
    vcs: git
    host: github.com
    url: https://github.com/contoso/shop
    hosting: cloud
    owner: contoso
    fullName: contoso/shop
    defaultBranch: main
    managed_by: gcx-map-source-code
  ttlSeconds: 604800
- domain: sourcecode
  type: Repository
  name: github.com/contoso/deploy-config
  properties:
    provider: github
    vcs: git
    host: github.com
    url: https://github.com/contoso/deploy-config
    hosting: cloud
    owner: contoso
    fullName: contoso/deploy-config
    managed_by: gcx-map-source-code
  ttlSeconds: 604800
```

## resources.yaml

One entry per resource the services use, in the `sourcecode` domain. A
`CloudResource` has no scope: its name is the provider's full id. See
[Resources](resources.md) for names and properties.

```yaml
- domain: sourcecode
  type: CloudResource
  name: arn:aws:sqs:us-east-1:111122223333:orders
  properties:
    kind: sqs-queue
    provider: aws
    resource_name: orders
    arn: arn:aws:sqs:us-east-1:111122223333:orders
    account_id: "111122223333"
    region: us-east-1
    managed_by: gcx-map-source-code
  ttlSeconds: 604800
- domain: sourcecode
  type: CloudResource
  name: arn:aws:s3:::contoso-invoices
  properties:
    kind: s3-bucket
    provider: aws
    resource_name: contoso-invoices
    arn: arn:aws:s3:::contoso-invoices
    managed_by: gcx-map-source-code
  ttlSeconds: 604800
```

Quote an account id, as above, so YAML reads it as a string.

## links.yaml

One entry per link. The link itself is in the `sourcecode` domain.

- **`from`** is the telemetry Service. Use `domain: metrics` and copy the
  Service's scope exactly as Step 5 found it.
- **`to`** is the repository or the `CloudResource`, by its name.

```yaml
- domain: sourcecode
  type: BUILT_FROM
  from:
    domain: metrics
    type: Service
    name: checkout
    scope:
      env: prod
      namespace: shop
  to:
    domain: sourcecode
    type: Repository
    name: github.com/contoso/shop
  properties:
    path: services/checkout
    managed_by: gcx-map-source-code
  ttlSeconds: 604800
- domain: sourcecode
  type: DEPLOYED_FROM
  from:
    domain: metrics
    type: Service
    name: checkout
    scope:
      env: prod
      namespace: shop
  to:
    domain: sourcecode
    type: Repository
    name: github.com/contoso/deploy-config
  properties:
    path: environments/prod/checkout.yaml
    managed_by: gcx-map-source-code
  ttlSeconds: 604800
- domain: sourcecode
  type: WRITES_TO
  from:
    domain: metrics
    type: Service
    name: checkout
    scope:
      env: prod
      namespace: shop
  to:
    domain: sourcecode
    type: CloudResource
    name: arn:aws:sqs:us-east-1:111122223333:orders
  properties:
    path: services/checkout/internal/orders/publisher.go
    managed_by: gcx-map-source-code
  ttlSeconds: 604800
```

- `BUILT_FROM.path` is the folder or build module of the service.
- `DEPLOYED_FROM.path` is the file or folder that renders the service for that
  environment.
- If the same service runs in several environments, write one `BUILT_FROM` link
  per Service scope. Write `DEPLOYED_FROM` only for the environment each config
  deploys to.
- `READS_FROM.path` and `WRITES_TO.path` are the file or folder where the code
  reads or writes the resource. A resource is usually per environment, so link
  it only from the Service in that environment.
- Scope keys must match the Service exactly. A Service without `site` has no
  `site` key in `scope`.
