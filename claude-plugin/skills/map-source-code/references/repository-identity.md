# Repository identity

A `Repository` entity's **name** is its canonical id: host and path, with no
scheme, no user, no default port and no `.git` suffix. The same repository must
always get the same name, whoever scans it and whichever remote form their
checkout uses. Otherwise the graph gets duplicates. So apply only the rules
below, never a rule that depends on who scans or what they say.

Two different repositories must never get the same name: their links would
merge. When a rule cannot tell, it keeps the names apart. A duplicate node for
one repository is the smaller harm.

## From remote URL to canonical name

Lowercase the host. On github.com, gitlab.com, bitbucket.org, dev.azure.com
and visualstudio.com, lowercase the path too: these hosts ignore case in
repository paths, so `Contoso/Shop` and `contoso/shop` are the same repository.
Keep the original case in `fullName`. On any other host, including self-hosted
servers, keep the path's case as the remote shows it: you cannot tell from the
remote whether that server ignores case.

Drop a default port: 22 on an `ssh://` remote, 80 on `http`, 443 on `https`.
Keep any other port, as `host:port`: one host can run different servers on
different ports. An scp-style remote has no port. So the SSH and HTTPS remotes
of one repository get one name only when both use default ports; otherwise the
repository gets two names, which is the smaller harm.

Azure DevOps has its own SSH and legacy host names. Rewrite each of them to the
`dev.azure.com` HTTPS form, `dev.azure.com/<organization>/<project>/_git/<repository>`:

- `ssh.dev.azure.com:v3/<organization>/<project>/<repository>`
- `vs-ssh.visualstudio.com:v3/<organization>/<project>/<repository>`
- `<organization>.visualstudio.com/<project>/_git/<repository>`, with or without
  `DefaultCollection/` after the host

The table shows remotes as Step 2 prints them, with any user part already
removed: `git@github.com:contoso/shop.git` prints as `github.com:contoso/shop.git`.

| Remote URL form | Canonical name |
|---|---|
| `https://github.com/contoso/shop.git` | `github.com/contoso/shop` |
| `github.com:contoso/shop.git` (scp-style) | `github.com/contoso/shop` |
| `ssh://github.example.com:2222/platform/shop.git` (non-default SSH port) | `github.example.com:2222/platform/shop` |
| `ssh://github.example.com:22/platform/shop.git` | `github.example.com/platform/shop` |
| `https://git.example.com:8443/Platform/Shop.git` (self-hosted, web port) | `git.example.com:8443/Platform/Shop` |
| `https://git.example.com:443/Platform/Shop.git` | `git.example.com/Platform/Shop` |
| `https://gitlab.com/contoso/team/shop.git` (nested groups) | `gitlab.com/contoso/team/shop` |
| `https://bitbucket.org/contoso/shop.git` | `bitbucket.org/contoso/shop` |
| `https://dev.azure.com/contoso/payments/_git/api` | `dev.azure.com/contoso/payments/_git/api` |
| `ssh.dev.azure.com:v3/contoso/payments/api` | `dev.azure.com/contoso/payments/_git/api` |
| `vs-ssh.visualstudio.com:v3/contoso/payments/api` | `dev.azure.com/contoso/payments/_git/api` |
| `https://contoso.visualstudio.com/payments/_git/api` | `dev.azure.com/contoso/payments/_git/api` |
| A form that still has a user name or token before the host, ending with `@` | drop them; never write credentials |

If a checkout has no `origin`, or the remote is a local path, skip it and say
why.

## Properties

The schema declares more properties than this table. Write only the string
properties below: `gcx kg entities upsert -f` sends property values as strings,
so leave out `languages` (a list) and `archived` (a bool).

| Property | Value | Required |
|---|---|---|
| `provider` | `github`, `gitlab`, `bitbucket`, `azuredevops`, `gitea`, `gerrit`, `codecommit`, or another short name. Take it from the host, for example `github.com` or a host the user names as GitHub Enterprise gives `github` | yes |
| `vcs` | `git` for every repository this skill reads | yes |
| `host` | The host part of the canonical name, without any port | no |
| `url` | The repository's web URL, for example `https://github.com/contoso/shop` | no |
| `hosting` | `cloud` for `github.com`, `gitlab.com`, `bitbucket.org`, `dev.azure.com` and `visualstudio.com`; `selfhosted` otherwise | no |
| `owner` | The org, user or group. For Azure DevOps, `org/project` | no |
| `fullName` | The display name in the case the remote shows, for example `Contoso/Shop` | no |
| `defaultBranch` | From `git symbolic-ref --short refs/remotes/origin/HEAD`, without the `origin/` prefix | no |
| `visibility` | `public`, `internal` or `private`, only when the user tells you or a provider CLI they already use reports it. Otherwise leave it out | no |
| `license` | The SPDX id, only when the license file clearly matches one. Otherwise leave it out | no |
| `managed_by` | `gcx-map-source-code` | always set |

Do not guess a value. A missing property is better than a wrong one.
