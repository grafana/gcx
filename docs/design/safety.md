# Confirmation and Safety

> Covers when to prompt users before destructive operations, the --force/GCX_AUTO_APPROVE pattern, dry-run support, and push idempotency.

---

## 3. Confirmation and Safety

### 3.1 When to Prompt

New destructive provider commands prompt before deleting remote resources
(single or bulk). `resources delete` retains its existing selector guard:
named targets may be deleted directly; broader selectors require `--force`.
It does not use an interactive confirmation prompt.

Do NOT prompt for:
- Push (create-or-update) — it is idempotent
- Pull (local write) — callers manage local changes through their normal workflow
- Config changes — low-risk, undoable

### 3.2 The `--force` Flag and `providers.ConfirmDestructive` `[IMPLEMENTED]`

All destructive provider commands use the shared `providers.ConfirmDestructive()`
helper. It applies this bypass chain, in order, before falling through to an
interactive prompt:

1. **`--force` flag** — explicit per-invocation bypass
2. **`GCX_AUTO_APPROVE` env var** — enables non-interactive operation in CI/CD
3. **Agent mode without either bypass above** — fails with actionable error
4. **Interactive prompt** — asks the user to confirm (`[y/N]`)

If none of the bypass conditions are met and stdin is closed/empty, the prompt's
`ReadString` returns EOF, surfacing a clear error.

```go
proceed, err := providers.ConfirmDestructive(
    cmd.InOrStdin(), cmd.ErrOrStderr(), opts.Force,
    fmt.Sprintf("Delete %d resource(s)?", count))
if err != nil {
    return err
}
if !proceed {
    exitCode := gcxerrors.ExitCancelled
    return &gcxerrors.DetailedError{
        Summary: "delete cancelled",
        ExitCode: &exitCode,
    }
}
```

For new commands, a declined prompt returns exit 5 as above. The helper itself
returns `(false, nil)` for a negative answer, so the caller owns the exit code.
Several shipped commands still return success on decline; preserve that
behavior until a deliberate compatibility change. See
[exit-codes.md § 2.4](exit-codes.md#24-declined-confirmations) for examples.

**Convention:** Use `--force` (long flag only, no `-f` shorthand per
[naming.md](naming.md) § 9.4). Do not use `--yes`, `--skip-confirmations`,
or other variants.

**Note:** Auto-approval does NOT enable `--include-managed` to protect resources
managed by external tools (Terraform, GitSync, etc.). Users must explicitly pass
`--include-managed` if needed.

The `resources delete` command additionally supports `--yes` (`-y`) which
auto-enables the `--force` flag. This is a legacy pattern specific to the
resources layer; new provider commands should use `--force` directly.

### 3.3 Agent Mode Rejects Unbypassed Destructive Operations `[IMPLEMENTED]`

Agent mode does not approve destructive operations. The shared bypass check
uses the following order; other documents should link here rather than copy it.

| `--force` | `GCX_AUTO_APPROVE` | Agent mode | Result from the shared bypass/prompt path |
|---|---|---|---|
| yes | any | any | Proceed without a prompt |
| no | `1`, `t`, `T`, `true`, `TRUE`, `True` | any | Proceed without a prompt |
| no | `0`, `f`, `F`, `false`, `FALSE`, `False`, empty, or unset | yes | Reject with `ErrAgentModeRequiresForce` |
| no | Same false/empty/unset values | no | Interactive `[y/N]` prompt |
| no | Any other value (such as `yes` or `on`) | any | Fail while parsing CLI options |

`GCX_AUTO_APPROVE` uses `strconv.ParseBool`; an empty value is skipped by
`parseEnvTags`. Setting the variable to `false` does not enable approval.
This differs from [agent-mode detection](agent-mode.md#61-detection), whose
environment vocabulary accepts `yes`/`no`.

The first row describes `CheckDestructiveBypass` itself, which short-circuits
on `--force`. A caller that loads CLI options earlier can still reject an
invalid environment value. `resources delete` does that and also accepts
`--yes`/`-y`; `gcx agent skills uninstall` has a separate `--yes`/`--all` flow
and loads the same options even for a single skill. Neither path should be
inferred from the provider confirmation table.

Implementation: `CheckDestructiveBypass` and `ConfirmDestructive` in
`internal/providers/confirm.go`; CLI parsing in `internal/config/envparse.go`.

### 3.4 Dry-Run

`gcx resources push` and `gcx resources delete` expose `--dry-run`;
`gcx resources validate` always uses the dry-run path. For K8s requests this
passes `DryRun: []string{"All"}` to API options. Provider commands vary: document
whether the specific command supports dry-run and whether it validates remotely,
previews locally, or skips unsupported operations. Resource-layer support does
not imply that every provider mutation has a `--dry-run` flag.

**Fail-safe guard.** Some Grafana APIs (all alerting resources today) ignore
server-side `dryRun` and apply the mutation anyway. A client-side guard
(`internal/resources/remote/dryrun_guard.go`) wraps the dynamic client and, for a
mutating dry-run against a resource **not** on the allowlist
(`dryrun_allowlist.go` — dashboards, folders, playlists), refuses to send the
request, warns on stderr, and records the resource as **skipped** (not pushed, not
failed; skips keep exit 0). Best-effort only — it confirms the operation is
well-formed and (for delete) that the target exists, not spec correctness ("not
verified"). Users who know a stack runs a resource on dual-write/unified can add it
via `--assume-server-dry-run <resource>.<group>` or the per-context
`resources.assume-server-dry-run` config. Robust long-term fix is server-side
(grafana-enterprise#12569).

### 3.5 Push Idempotency

Push is **idempotent** (create-or-update). The flow: Get → if exists: Update
with `resourceVersion`, if 404: Create. Safe to run repeatedly with the same
input. Document this explicitly in push-like commands:

```
# Push is idempotent: creates new resources and updates existing ones
gcx resources push ./dashboards/
```

Reference: `data-flows.md` Section 2 (PUSH Pipeline)
