# RFC 030: Organization Access and SCIM Commands

**Status**: proposed

## Summary

Add commands to manage organization users, service accounts, role assignments,
and SCIM provisioning. The commands use the selected gcx context. SCIM user
requests read a service account token from a file. The work will ship in small
PRs. Each PR will have a live read and write check before it is complete.

## Motivation

An operator who sets up SCIM must now use raw API calls for organization access
and SCIM settings. The operator must also manage a separate service account
token. This makes the steps hard to discover and review. It can also put the
token in shell history or command output.

The selected context already supplies the Grafana URL and organization identity.
Dedicated commands can show the subject, planned change, and result in the same
form as other gcx commands.

## Scope

This RFC covers organization users, service accounts and their tokens, fixed
role lookup and assignment, SCIM settings, and SCIM users. SCIM groups can use
the same client in a later PR. User removal affects only the organization in
the selected context. The first implementation PR will cover organization user
reads and removal. Later PRs will add service accounts and roles, SCIM settings,
then SCIM users. Each PR must be useful on its own.

Issue #126 also proposes a SCIM provider. The SCIM user work in this RFC is the
same work. It must have one implementation, not two command trees.

## User experience

The examples use [the command naming rules](../design/command-naming.md)
where those rules specify the operation. Token child operations and the new
domain verbs still need the review noted below.

```text
gcx org users list
gcx org users get <login|email|id>
gcx org users remove <login|email|id>... --dry-run
gcx org users remove <login|email|id>... --force

gcx org service-accounts list
gcx org service-accounts create <name> --role None
gcx org service-accounts delete <name|id> --force
gcx org service-accounts create-token <service-account-id> <token-name> --ttl 24h --output-file ./scim-token
gcx org service-accounts delete-token <service-account-id> <token-id> --force
gcx org roles list --filter org.users
gcx org roles assign <role-uid> --service-account-id <id>

gcx scim config get
gcx scim config update --user-sync=true --group-sync=false --reject-non-provisioned=false --dry-run
gcx scim config update --user-sync=true
gcx scim endpoint get
gcx scim users create --user-name test@example.com --external-id test-0001 --token-file ./scim-token
gcx scim users list --token-file ./scim-token
gcx scim users get <id> --token-file ./scim-token
gcx scim users deactivate <id> --token-file ./scim-token
```

The user first reads the current state. A dry run shows the planned change and
sends no write request. A successful write prints a structured result. A failed
write names the subject and the failed step. The token creation command writes
the secret to a new file with owner-only access. Normal output gives the file
path and token metadata. It never gives the token value. The user can delete
the test service account after verification. The operator can deactivate a test
SCIM user. Grafana retains that user record. `scim endpoint get` prints the
base URL for the selected stack. It does not print a token.

## Technical design

### Boundaries and authentication

The `cmd/` packages parse arguments and format results. Domain code in
`internal/` calls the relevant Grafana API. This follows [the layer rules](../../CONSTITUTION.md#architecture-invariants).
Organization access commands use the authentication of the selected context.
SCIM user commands read `--token-file` for each invocation and send that
token only to the SCIM endpoint for the selected stack. They do not change the
context, write the token to configuration, or print it in normal output or logs.
The existing `--insecure-log-http-payload` flag can expose secrets when the user
explicitly enables it. Its global warning and behavior still apply.

The SCIM client must construct its base URL from trusted context data. It must
reject a context without the required stack identity. It uses the existing
token file reader for size and empty-value checks. It must not send the token
to a URL from a redirect or an untrusted response. A service account token has
the permissions granted to that account; gcx does not grant permissions on
behalf of the user.

### Subjects and writes

User lookup accepts an ID, login, or email. A command resolves login or email
to one exact user in the selected organization before it writes. It fails if
there is no match or more than one match. `org users remove` removes only the
selected organization's membership. It leaves the user account and other
organization memberships intact. It reports each batch failure.
Destructive commands use `providers.ConfirmDestructive` and accept `--force`.
They follow [the shared confirmation rules](../design/safety.md#32-the---force-flag-and-providersconfirmdestructive-implemented).

Service account creation and role assignment are separate actions. This keeps
the state after each action visible. A later setup workflow can combine them
only if it defines partial failure and cleanup behavior. Role assignment uses
the fixed role UID returned by the role API. It does not infer a role from a
display name alone. `org roles assign` adds one direct role assignment to one
service account. It does not replace other assignments. The service account ID
selects the recipient. The role UID selects the role.

SCIM config uses the server-discovered `SCIMConfig` resource through the shared
native binding. The update changes only flags supplied by the user and keeps
the other fields. It accepts user sync, group sync, and rejection of users who
were not provisioned. A dry run shows the current and proposed values. A
conflict returns an error; it does not overwrite a concurrent update.

SCIM user commands call the SCIM user API with a service account identity.
They preserve the server's user ID and return the server's resource fields.
When user sync is off, the error points to `gcx scim config update
--user-sync=true`. The command does not turn sync on without a request.
The deactivate command sets the user's active state to false. A live write test
must use an owned test identity. It must record that Grafana keeps the user
record after deactivation.

### Token file failure handling

Token creation opens the requested file with `O_CREATE|O_EXCL` and mode 0600
before it calls the API. This fails if the path exists. It then creates the
token and writes the secret to that open file. If a file write fails after the
API creates the token, gcx revokes that token by its ID and removes the partial
file. If revocation also fails, the error names the service account and token
ID so the operator can revoke it. It never prints the token secret.

### Resource discovery

No SCIM discovery change is proposed. On 2026-11-03, a read-only check of a
Grafana Cloud test context showed that `/apis` advertises `scim.grafana.app/v0alpha1`.
The resource list marks `SCIMConfig` as namespaced. The current gcx binary lists
and reads that config through `gcx resources`. The issue's discovery criterion
is met on this context. If another server omits it, capture its `/apis` and
group-version discovery responses before changing discovery code.

## Drawbacks

The first PR does not complete the whole SCIM setup. A token file adds a local
secret that the operator must protect and remove. The commands expose more
access-management actions, so exact subject resolution and reviewable dry runs
are required.

## Rationale and alternatives

The `org` area means the organization inside the selected Grafana stack.
`gcx cloud orgs` lists Grafana Cloud organizations at the Cloud account level.
The context name and the `org users` noun make the stack scope visible in help.
The issue's `gcx users list` proposal has no area. Separate service account
and role commands make partial setup visible. A combined setup command can
follow after these operations have live tests.

A token file avoids placing the secret in the command line and avoids saving a
SCIM-only token in the context. It requires a file for each SCIM invocation.

## Prior art

The design uses `providers.ConfirmDestructive` for removals, the native binding
for `SCIMConfig`, and the existing `--token-file` reader for secret input.
Exclusive file creation gives token output a no-replace rule without a rename.

## Approved naming exception

On 2026-11-03, the requester approved
`gcx org service-accounts create-token <service-account-id> <name>` as an
explicit constitutional waiver for the parent-required token create shape.
The service account ID selects the parent. The token name identifies the new
token. This approval applies to this command shape in this RFC.

## Unresolved questions

- Approve `org users remove` as a domain verb. It removes one or more
  memberships from the selected organization. It does not delete a Grafana
  account. The name follows the subject of the actual change.
- Approve `org roles assign` as a domain verb. It adds one direct role
  assignment to one service account and preserves other assignments. The
  [naming guide](../design/command-naming.md#domain-verbs) requires explicit
  review for a new domain verb.
- Verify the SCIM base URL rule and redirect behavior against public Grafana
  documentation and a live test before implementing the SCIM client.
- Verify the API's exact error response when user sync is off. The command must
  match that response without treating every 403 as disabled sync.
- Define the role and permission checks for each write after testing the
  endpoints with the identities used in the implementation PR.
- Verify the SCIM API's deactivation and reactivation behavior with an owned
  test identity before the live write test.

## Future possibilities

Add SCIM groups, organization user role changes, and a guided setup command.
Each can use the clients and output contracts established here.

## Terminology

- **Organization user**: A Grafana user who belongs to the organization in the
  selected context.
- **Service account**: A Grafana identity used by a program or integration.
- **Fixed role**: A Grafana access-control role with a stable UID.
- **SCIM**: The identity provisioning protocol used to manage users and groups.
