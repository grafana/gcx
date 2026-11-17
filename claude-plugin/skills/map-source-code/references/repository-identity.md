# Repository identity

A `Repository` entity's **name** is its canonical id: host and path, with no
scheme, no user, no port and no `.git` suffix. The same repository must always
get the same name, whoever scans it and whichever remote form their checkout
uses. Otherwise the graph gets duplicates.

## From remote URL to canonical name

Lowercase the host. On github.com, gitlab.com, bitbucket.org, dev.azure.com,
and on GitHub Enterprise, GitLab, Bitbucket, Azure DevOps Server or Gitea
servers, lowercase the path too: these providers ignore case in repository
paths, so `Contoso/Shop` and `contoso/shop` are the same repository. Keep the
original case in `fullName`. On any other host, keep the path's case as the
remote shows it.

Drop the port: the SSH and HTTPS remotes of one repository use different ports.
Keep `host:port` only when the user says that one host serves different
repositories on different web ports.

| Remote URL form | Canonical name |
|---|---|
| `https://github.com/contoso/shop.git` | `github.com/contoso/shop` |
| `git@github.com:contoso/shop.git` | `github.com/contoso/shop` |
| `ssh://git@github.example.com:2222/platform/shop.git` | `github.example.com/platform/shop` |
| `https://gitlab.com/contoso/team/shop.git` (nested groups) | `gitlab.com/contoso/team/shop` |
| `https://bitbucket.org/contoso/shop.git` | `bitbucket.org/contoso/shop` |
| `https://dev.azure.com/contoso/payments/_git/api` | `dev.azure.com/contoso/payments/_git/api` |
| `git@ssh.dev.azure.com:v3/contoso/payments/api` | `dev.azure.com/contoso/payments/_git/api` |
| `https://user:token@host/...` | drop the user and token; never write credentials |

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
| `host` | The host part of the canonical name | no |
| `url` | The repository's web URL, for example `https://github.com/contoso/shop` | no |
| `hosting` | `cloud` for `github.com`, `gitlab.com`, `bitbucket.org` and `dev.azure.com`; `selfhosted` otherwise | no |
| `owner` | The org, user or group. For Azure DevOps, `org/project` | no |
| `fullName` | The display name in the case the remote shows, for example `Contoso/Shop` | no |
| `defaultBranch` | From `git symbolic-ref --short refs/remotes/origin/HEAD`, without the `origin/` prefix | no |
| `visibility` | `public`, `internal` or `private`, only when the user tells you or a provider CLI they already use reports it. Otherwise leave it out | no |
| `license` | The SPDX id, only when the license file clearly matches one. Otherwise leave it out | no |
| `managed_by` | `gcx-map-source-code` | always set |

Do not guess a value. A missing property is better than a wrong one.
