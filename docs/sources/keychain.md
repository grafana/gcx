---
aliases:
  - /docs/grafana-cloud/as-code/observability-as-code/grafana-cli/gcx/keychain/
title: Keychain credential storage
labels:
  products:
    - cloud
    - enterprise
    - oss
weight: 5
---

# Keychain credential storage

gcx stores token-shaped credentials in the operating system credential store.
It uses Keychain on macOS, Credential Manager on Windows, and Secret Service on
Linux and BSD. The YAML file contains a reference to the stored credential.

The reference is bound to these values:

- The canonical configuration file path.
- The owner type and owner name.
- The credential field.
- The normalized credential destination.

A copied configuration file cannot use the stored credential. Authenticate the
copied file separately.

## Configure credential storage

The credential-storage policy is a trusted, process-wide setting. It accepts
only `on` and `off` (case-insensitive after trimming); omitted means `on`.
Configure it in a system or user configuration file:

```yaml
credentials:
  keychain: off
```

Use `off` only when plaintext storage in a mode-`0600` YAML file is deliberate,
such as for a headless machine or CI runner whose OS credential store is not
available. In `off` mode gcx does not contact the OS store; new and refreshed
credentials are persisted in that configuration file.

`GCX_KEYCHAIN` can override the configuration for one invocation or a shell
environment:

```shell
export GCX_KEYCHAIN=off
```

The precedence, highest first, is:

1. `GCX_KEYCHAIN`
2. A deliberately selected file (`--config` or `GCX_CONFIG`)
3. User configuration
4. System configuration
5. The default, `on`

An automatically discovered repository-local `.gcx.yaml` is not trusted to set
this policy. gcx ignores its `credentials.keychain` value and warns once per
invocation, but still merges that file's ordinary configuration fields. Review
and deliberately select a repository file with `--config .gcx.yaml` or
`GCX_CONFIG=.gcx.yaml` if its policy should apply.

An invalid `GCX_KEYCHAIN` value warns and resolves to `on`, so a typo cannot
silently enable plaintext storage. An invalid value in a trusted configuration
file fails validation and names `credentials.keychain` and the source file. An
invalid value in an automatically discovered local file is ignored with the
same local-policy warning.

## No automatic plaintext fallback

Configured `off` mode is an explicit plaintext-storage choice; it is not an
outage-triggered fallback. When the resolved policy is `on`, an unavailable or
locked OS credential store fails credential writes closed. gcx never dynamically
downgrades to plaintext during login, token refresh, or ordinary credential
writes. Unlock or restore the store, or explicitly configure `off` before
retrying.

An optional automatic-fallback design is deferred. It would require its own
ownership and replacement-safety rules; it is not active in this release.

## Replacing a stored credential in `off` mode

gcx does not move stored credentials back into the configuration file when you
switch to `off`. It preserves their references and cannot read them. You have
two choices for each one.

Keep the reference. Set the policy to `on` again (or unset `GCX_KEYCHAIN`) and
the credential works again.

Replace the credential. Authenticate again, and gcx writes the new value in
plaintext. This is not reversible: gcx cannot delete through a disabled store,
so the credential you replaced stays in the OS credential store with nothing
referencing it, and no gcx command can reach it again. gcx warns with cleanup
guidance when this happens. Delete that stale OS-store entry yourself to finish
the change, and treat that cleanup as required when you are replacing a leaked
credential.

gcx cannot remove a credential that is in the credential store while `off` is
resolved. `gcx config unset` on that field, and deleting the stack or Cloud
entry that owns it, both fail. gcx does not drop the reference and leave the
secret in the credential store, because that reports a deletion that did not
happen. Set the policy to `on` and run the command again. When the credential
store is permanently unavailable, setting it to `on` does not help, because
gcx still cannot read the entry: edit the configuration file to remove the
reference, then delete the entry through your OS credential store.

This applies only to credentials that are in the credential store. A credential
that is already plaintext in the configuration file has nothing to remove from
the store, so `gcx config unset` and entry deletion both work as usual.

## `Keychain unavailable`

This error means gcx cannot reach the OS credential store. With the resolved
policy set to `on`, gcx cannot store or use the credential and stops rather than
writing it in plaintext. Restore the credential store, or deliberately set
`credentials.keychain: off` in a trusted configuration file before retrying.
`GCX_KEYCHAIN=off` is an equivalent one-invocation or environment override.

## `Keychain locked`

This error means that macOS Keychain or a Linux or BSD Secret Service is
available, but gcx cannot unlock it in the current session. gcx stops commands
that need the credential. It does not use or write a plaintext credential.
Configuration inspection and repair commands remain available.

Windows Credential Manager lock failures are not in this error class.

You can supply a credential with an environment variable when you cannot unlock
the credential store. For example, you can use `GRAFANA_TOKEN`.

### Unlock macOS Keychain

Unlock the login keychain in the same security session that runs gcx. An unlock
in a different terminal or process tree might not apply to the gcx process.

Run this command in the gcx session:

```shell
security unlock-keychain
```

The command asks for the keychain password. Do not use the `-p` option. This
option exposes the password in the process arguments.

Run the gcx command again after the unlock. If gcx still cannot use the
keychain, run gcx from an unlocked desktop session.

### Unlock a GNOME keyring in a headless session

A headless or SSH session might not have an agent that can answer the unlock
prompt. First, read the lock state:

```shell
busctl --user get-property org.freedesktop.secrets \
  /org/freedesktop/secrets/collection/login \
  org.freedesktop.Secret.Collection Locked
```

`b true` means that the collection is locked.

`gnome-keyring-daemon --unlock` reads the password from standard input. It does
not show a prompt. The `--daemonize` option creates a child process, so a
password that you type does not reach that process. A trailing newline also
prevents the unlock. Use this procedure:

```shell
stty -echo; printf 'Keyring password: '; read -r PW; stty echo; echo
printf '%s' "$PW" | gnome-keyring-daemon --replace --daemonize --unlock
unset PW
```

Read the lock state again. `b false` means that the collection is unlocked.

If the state does not change, a service manager might own the
`org.freedesktop.secrets` name. Stop the service, and run the unlock procedure
again:

```shell
systemctl --user stop gnome-keyring-daemon.service gnome-keyring-daemon.socket
```

## A credential was `rejected before network use`

This error means that gcx did not send the credential. The error can have one
of these causes:

- The keychain reference is missing or belongs to a different source.
- The credential destination changed.
- The keychain is locked.
- An environment credential is paired with an automatically discovered
  repository destination.

If the keychain is locked, gcx shows the `Keychain locked` error and the
procedures on this page apply.

For another cause, review the file and run the exact repair command from the
error. You can use `gcx config edit user` or
`gcx config edit --config "<path>"` when the error gives that command. Then,
re-authenticate, replace the field, or unset the field.

An explicit configuration path does not make a missing, foreign, or
destination-mismatched keychain reference valid.
