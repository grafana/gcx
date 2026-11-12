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

## links.yaml

One entry per link. The link itself is in the `sourcecode` domain.

- **`from`** is the telemetry Service. Use `domain: metrics` and copy the
  Service's scope exactly as Step 4 found it.
- **`to`** is the repository, by its canonical name.

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
```

- `BUILT_FROM.path` is the folder or build module of the service.
- `DEPLOYED_FROM.path` is the file or folder that renders the service for that
  environment.
- If the same service runs in several environments, write one `BUILT_FROM` link
  per Service scope. Write `DEPLOYED_FROM` only for the environment each config
  deploys to.
- Scope keys must match the Service exactly. A Service without `site` has no
  `site` key in `scope`.
