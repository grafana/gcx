# Comparison Report Template

Copy this template and fill it in for every command in the verification tasks
from tasks.md. Every row must have a status. Do not omit commands or mark them
"skipped".

```markdown
## Comparison Report: {provider}

### Per-Command Pass/Fail

| command | status | captured output (truncated) |
|---------|--------|-----------------------------|
| gcx {resource} list | PASS / FAIL | {first 3 lines of output or error} |
| gcx {resource} list | PASS / FAIL | {first 3 lines of output or error} |
| gcx {resource} get {id} | PASS / FAIL | {first 3 lines} |
| gcx {resource} get {id} | PASS / FAIL | {first 3 lines} |
| gcx resources get {alias} | PASS / FAIL | {first 3 lines} |
| gcx {resource} {subcommand} | PASS / FAIL | {first 3 lines} |

### Per-Command Output Format Verification (MANDATORY)

Every show/list command MUST be tested across the formats it actually
declares — run the script in `../SKILL.md` § "Step 4B: Smoke Tests", which derives
them from each command's own `--help`. Do NOT skip a declared format, and do NOT
add a column for a format the command does not have: there is no repo-wide set,
so `table` and `wide` are absent from many commands by design.

One row per command, with a result for every declared format:

| command | formats (from `--help`) | result |
|---------|-------------------------|--------|
| gcx {resource} list | e.g. `agents, json, table, wide, yaml` | PASS / FAIL per format |
| gcx {resource} get {id} | e.g. `agents, json, yaml` | PASS / FAIL per format |
| gcx {resource} {subcommand} | … | PASS / FAIL per format |

For each FAIL: capture the error message and root cause. Record UNVERIFIED
with the reason when live access or a populated fixture is unavailable.

### Adapter Smoke Results (MANDATORY)

Every TypedCRUD resource MUST be verified via the adapter path.

| resource alias | `resources list-types` visible? | `resources get {alias}` works? | `resources get {alias}/{id} -o json` works? | notes |
|----------------|------------------------------|-------------------------------|---------------------------------------------|-------|
| {alias} | YES / NO | YES / NO | YES / NO | {error details if NO} |

**Fail criteria:** Investigate any NO in the first three columns and fix
confirmed registration or adapter defects. Use UNVERIFIED when access or
populated fixtures are unavailable; do not infer a wiring bug from that alone.

### List ID Comparison

```diff
=== List ID diff ===
{paste full diff output here, or "MATCH" if identical}
```

Verdict: MATCH | MISMATCH
If MISMATCH: {describe which IDs differ and probable cause}

### Get Field Comparison

```diff
=== Get field diff ===
{paste full diff output here, or "MATCH" if identical}
```

Verdict: MATCH | MISMATCH
If MISMATCH: {describe which fields differ -- note any acceptable differences
such as computed fields that differ by small values}

### Discrepancy Summary

| # | description | verdict | rationale or fix |
|---|-------------|---------|-----------------|
| 1 | {describe any mismatch or unexpected behavior} | justified / fix required | {written rationale or PR link} |

(Leave table empty if no discrepancies found.)

### Overall Verdict

**PASS** / **FAIL** — {one-line summary}

If FAIL: list blocking issues that must be resolved before approval.
```
