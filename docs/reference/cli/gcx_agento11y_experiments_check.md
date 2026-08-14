## gcx agento11y experiments check

Grade a finished experiment against quality thresholds for CI.

### Synopsis

Compare a finished experiment against quality thresholds and exit non-zero
when the experiment falls short, so a CI job fails on a regression.

The main threshold, --min-pass-rate, applies to the report's pass_rate: of the
test cases that produced a verdict, the share that passed on the first
completed attempt. The denominator counts test cases, not trials. For the
all-trials figure, read rows[].summary.trial_pass_rate from get-report -o json.
When --min-pass-rate is omitted, the pass rate is not checked; the gate is then
the run's status plus, by default, full verdict coverage.

A test case whose trials all failed produces no verdict, so it changes neither
side of that fraction. Take a run where every trial failed for 90 of its 100
test cases: if the surviving 10 all passed, the report says 100%.
--min-verdict-coverage rejects that run by requiring a minimum share of the
suite to have produced a verdict. It defaults to 1, the whole suite.

With --wait the command polls the experiment every 5 seconds until it finishes,
up to --timeout (default 10m; raise it for a long suite), and then grades it.
Without --wait an unfinished experiment exits 1 immediately.

Exit codes:
  0  every threshold met
  1  general error, including an experiment that has not finished yet and a
     --wait that timed out
  2  a flag value is invalid, or --timeout is used without --wait
  4  the run missed a threshold; or the experiment finished with status failed
     or canceled; or a check could not measure what it grades while
     --on-unknown=fail (the default)

A check that cannot measure what it grades reports the verdict unknown, and
--on-unknown decides those runs: fail (the default) exits 4, and pass exits 0.
Two cases reach it. An experiment whose evaluators emit only reward or numeric
scores produces no pass or fail verdict, so there is no pass rate and no
coverage to measure. A server that does not report the size of the suite leaves
coverage unmeasurable on its own.

```
gcx agento11y experiments check <run-id> [flags]
```

### Examples

```
  # Fail the build when fewer than 90% of test cases pass
  gcx agento11y experiments check <run-id> --min-pass-rate 0.9

  # The run completed and every test case produced a verdict, no rate threshold
  gcx agento11y experiments check <run-id>

  # The harness is still running: wait for the run to finish, then gate
  gcx agento11y experiments check <run-id> --wait --timeout 30m --min-pass-rate 0.9

  # Machine-readable verdict for a CI step summary
  gcx agento11y experiments check <run-id> --min-pass-rate 0.9 -o json

  # Accept a run where a fifth of the test cases produced no verdict
  gcx agento11y experiments check <run-id> --min-pass-rate 0.9 --min-verdict-coverage 0.8

  # Reward-scored experiment: no pass or fail verdict exists, so do not fail on it
  gcx agento11y experiments check <run-id> --min-pass-rate 0.9 --on-unknown pass
```

### Options

```
  -h, --help                         help for check
      --jq string                    jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string                  Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --min-pass-rate float          Lowest acceptable pass rate, 0..1. When omitted, the pass rate is not checked. Zero accepts any measured rate; a rate that cannot be measured is still graded by --on-unknown. See --help for how pass_rate is measured.
      --min-verdict-coverage float   Lowest acceptable share of test cases that produced a pass or fail verdict, 0..1. Set to 0 to skip this check. (default 1)
      --on-unknown string            Verdict for a check that could not measure what it grades: fail (exit 4) or pass (exit 0). (default "fail")
  -o, --output string                Output format. One of: agents, json, text, yaml (default "text")
      --timeout duration             Maximum time to wait for the experiment to finish. Needs --wait. (default 10m0s)
      --wait                         Poll every 5 seconds until the experiment finishes, then grade it.
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, OPENCODE, PI_CODING_AGENT, or GCX_AGENT_MODE env vars.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx agento11y experiments](gcx_agento11y_experiments.md)	 - Manage eval experiment runs.

