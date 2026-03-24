# Comparison Report Template

Copy this template and fill it in for every command in the verification plan.
Every row must have a status. Do not omit commands or mark them "skipped".

```markdown
## Comparison Report: {provider}

### Per-Command Pass/Fail

| command | status | captured output (truncated) |
|---------|--------|-----------------------------|
| gcx {resource} list | PASS / FAIL | {first 3 lines of output or error} |
| grafanactl {resource} list | PASS / FAIL | {first 3 lines of output or error} |
| gcx {resource} get {id} | PASS / FAIL | {first 3 lines} |
| grafanactl {resource} get {id} | PASS / FAIL | {first 3 lines} |
| grafanactl resources get {alias} | PASS / FAIL | {first 3 lines} |
| grafanactl {resource} {subcommand} | PASS / FAIL | {first 3 lines} |

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

### Output Format Check

| format | status | notes |
|--------|--------|-------|
| table | OK / FAIL | {error if FAIL} |
| wide | OK / FAIL | {error if FAIL} |
| json | OK / FAIL | {error if FAIL} |
| yaml | OK / FAIL | {error if FAIL} |

### Discrepancy Summary

| # | description | verdict | rationale or fix |
|---|-------------|---------|-----------------|
| 1 | {describe any mismatch or unexpected behavior} | justified / fix required | {written rationale or PR link} |

(Leave table empty if no discrepancies found.)
```
