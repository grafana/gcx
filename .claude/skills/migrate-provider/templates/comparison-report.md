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

Every show/list command MUST be tested with all four output formats.
Do NOT skip any format or mark as "not applicable".

| command | json | table | wide | yaml |
|---------|------|-------|------|------|
| gcx {resource} list | PASS / FAIL | PASS / FAIL | PASS / FAIL | PASS / FAIL |
| gcx {resource} get {id} | PASS / FAIL | PASS / FAIL | PASS / FAIL | PASS / FAIL |
| gcx {resource} {subcommand} | PASS / FAIL | PASS / FAIL | PASS / FAIL | PASS / FAIL |

For each FAIL: capture the error message and root cause.

### Adapter Smoke Results (MANDATORY)

Every TypedCRUD resource MUST be verified via the adapter path.

| resource alias | `resources schemas` visible? | `resources get {alias}` works? | `resources get {alias}/{id} -o json` works? | notes |
|----------------|------------------------------|-------------------------------|---------------------------------------------|-------|
| {alias} | YES / NO | YES / NO | YES / NO | {error details if NO} |

**Fail criteria:** Any NO in the first three columns is a registration or
adapter wiring bug that must be fixed before the report is approved.

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
