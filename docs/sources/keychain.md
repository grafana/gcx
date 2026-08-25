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

## Plaintext fallback

gcx can keep a new credential in a mode-`0600` configuration file when no
credential store is available. gcx writes a warning when it does this.

gcx does not use plaintext fallback for these conditions:

- A locked credential store.
- A replacement or deletion of an existing credential.
- A missing or rejected credential reference.
- A value that is too large for the credential store.
- An unknown credential store error.

gcx uses plaintext fallback for a replacement when you disable the credential
store. See [Disable the credential store](#disable-the-credential-store).

## Disable the credential store

Set `GCX_KEYCHAIN=disabled` when the credential store is permanently
unavailable, such as on a headless box, a CI runner, or a session that can
never unlock the keyring. gcx then keeps credentials in the mode-`0600`
configuration file and does not use the credential store.

```shell
export GCX_KEYCHAIN=disabled
```

The values `disabled`, `off`, `false`, and `0` all disable the credential
store. gcx resolves any other value to enabled, so a typo cannot write
credentials in plaintext without your intent. Set the variable in your shell
profile or CI job environment to make it permanent for a machine.

gcx does not move stored credentials back into the configuration file when you
disable the credential store. It preserves their references and cannot read
them. You have two choices for each one.

Keep the reference. Unset `GCX_KEYCHAIN` and the credential works again.

Replace the credential. Authenticate again, and gcx writes the new value in
plaintext. This is not reversible: gcx cannot delete through a disabled store,
so the credential you replaced stays in the OS credential store with nothing
referencing it, and no gcx command can reach it again. gcx warns when this
happens. Delete that entry yourself to finish the change, and treat this as
required when you are replacing a leaked credential.

gcx cannot remove a stored credential while the credential store is disabled.
`gcx config unset` on the field, and deleting the stack or Cloud entry that
owns it, both fail. gcx does not drop the reference and leave the secret in the
credential store, because that reports a deletion that did not happen. Unset
`GCX_KEYCHAIN` and run the command again. When the credential store is
permanently unavailable, unsetting the variable does not help, because gcx
still cannot read the entry: edit the configuration file to remove the
reference, then delete the entry through your OS credential store.

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
