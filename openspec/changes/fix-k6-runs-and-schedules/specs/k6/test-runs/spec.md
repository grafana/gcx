# Spec Delta

## Purpose

Lets users and agents see a k6 Cloud load test's runs and their outcomes correctly: which runs exist, newest first, and whether each one passed, failed or errored.

## ADDED Requirements

### Requirement: Report each test run's test and result
`gcx k6 runs list`, `gcx k6 test-run runs list` and `gcx k6 test-run status` SHALL report each run's `id`, `test_id`, `status`, `result`, `result_details`, `status_details`, `created` and `ended` as k6 Cloud returns them. `result` is `passed`, `failed`, `error`, or null while the run is unfinished. JSON and YAML output SHALL NOT emit `load_test_id` or `result_status`.

#### Scenario: Failed run
- **WHEN** a run finished with result `failed` and result details message "One or more thresholds failed."
- **THEN** JSON output shows `"result": "failed"` and that message under `result_details`

#### Scenario: Unfinished run
- **WHEN** a run is still in progress and k6 Cloud reports no result and no end time
- **THEN** JSON output shows `"result": null` and the table shows `-` in the RESULT and ENDED columns

#### Scenario: Table columns
- **WHEN** runs of load test 822 are listed in table format
- **THEN** each row shows `822` in the TEST ID column and the run's `result` value in the RESULT column

### Requirement: List a test's runs newest first
`gcx k6 runs list` and `gcx k6 test-run runs list` SHALL order runs by creation time, newest first. `--limit N` SHALL return the N most recently created runs.

#### Scenario: Limited listing
- **WHEN** a test has runs created on three different days and `gcx k6 runs list <test> --limit 2` runs
- **THEN** the output contains the two most recent runs, the newest first

### Requirement: List every run of a test on request
With `--limit 0`, run listings SHALL return every run of the test, including when the test has more runs than k6 Cloud returns in one page.

#### Scenario: More runs than one page
- **WHEN** a test has 1,336 runs and `gcx k6 runs list <test> --limit 0` runs
- **THEN** the output contains all 1,336 runs, newest first

#### Scenario: Test without runs
- **WHEN** a test has no runs and `gcx k6 runs list <test> -o json` runs
- **THEN** the output is an empty array `[]`

### Requirement: Report the newest run's status
`gcx k6 test-run status` SHALL report the most recently created run of the load test. When the test has no runs, it SHALL fail with an error stating that the test has no runs.

#### Scenario: Old and new runs
- **WHEN** a test has runs from earlier years and a run created yesterday
- **THEN** `gcx k6 test-run status` reports yesterday's run

#### Scenario: No runs
- **WHEN** a test has never run
- **THEN** `gcx k6 test-run status` exits non-zero with an error stating that the test has no runs
