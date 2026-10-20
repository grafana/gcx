# Proposal

## Why

`gcx k6` gives wrong answers about test runs and schedules without reporting an
error. Checked through gcx's OAuth login against a live Grafana Cloud stack on
2026-10-19:

- Run listings return a test's oldest runs first. A test with 1,336 runs listed
  runs from 2023, and `--limit 0` stops at the oldest 1,000.
- Every run's result shows as `pending` and its test ID as `0`.
- `gcx k6 test-run status`, which promises the most recent run, reports a
  three-year-old run.
- Cron schedules appear to have no recurrence at all.
- `gcx k6 schedules update` always fails.
- `gcx k6 load-tests delete-schedule` reports success while deleting nothing.

Agents, such as the k6-manage skill, work around these with raw `gcx api` calls.
These fixes are also the foundation for the new run commands planned in
`add-k6-run-commands`.

## What Changes

- Test runs decode the k6 v6 API's fields: `test_id`, `result` (`passed`,
  `failed`, `error`, or null while running), `result_details` and
  `status_details`. **BREAKING (output only):** JSON and YAML output of
  `gcx k6 runs list`, `gcx k6 test-run runs list` and `gcx k6 test-run status`
  renames `load_test_id` to `test_id` and replaces the integer `result_status`
  with the string `result`. The old fields were always `0`.
- Run listings are newest-first and page past the API's 1,000-run page limit.
  `--limit N` returns the N newest runs and `--limit 0` returns all of them.
- `gcx k6 test-run status` reports the newest run.
- Schedules keep every recurrence setting the API supports: `cron`
  (`schedule` and `time_zone`) and, on recurrence rules, `byday`, `until`,
  `count` and the `YEARLY` frequency. The `gcx k6 schedules list` table
  shows each schedule's recurrence in a new RECURRENCE column.
- `gcx k6 schedules update` and schedule updates through `gcx resources push`
  use the route the API actually provides, which replaces the schedule and
  carries every recurrence setting. Today they call a route that doesn't exist.
- `gcx k6 load-tests delete-schedule` and schedule deletes through
  `gcx resources delete` use the route the API actually provides. Today they
  call a route that doesn't exist, read the 404 as "already deleted", and
  report success.
- Updating a load test with `gcx k6 load-tests update -f` or
  `gcx resources push` fails with a clear error when the manifest's
  `project_id` differs from the test's project. Today the field is ignored and
  the command reports success.
- No command paths, aliases, flags or positional arguments change.

Out of scope:

- New commands. These are planned in `add-k6-run-commands`.
- Verifying the service-account login path. Both k6 clients get the same
  fixes, but only the OAuth path was checked live.
- Load test creation over OAuth. The k6-manage skill reports it as broken,
  but it doesn't reproduce: multipart uploads reach k6 intact.

## Capabilities

### New Capabilities

- `k6/test-runs`: listing and reporting k6 Cloud test runs, including their
  fields, ordering, completeness and the newest-run status view.
- `k6/schedules`: reading and changing the schedules that run k6 Cloud load
  tests, including recurrence settings, updates and deletes.
- `k6/load-tests`: managing k6 Cloud load tests. This change covers updates
  that the API cannot apply.

### Modified Capabilities

None.

## Impact

- **Code:** all in `internal/providers/k6/`.
  - `types.go`: run and schedule types.
  - `proxy_client.go` and `direct_client.go`: run listing, schedule update and
    delete.
  - `commands.go`: run table, `test-run status`, `load-tests update`.
  - `resource_adapter.go`: Schedule and LoadTest adapters.
  - The tests for all of the above. Test fixtures move from the pre-v6 run
    shape to the v6 shape.
- **APIs:** k6 Cloud v6 REST endpoints from the public OpenAPI spec only.
- **Users and agents:** run output gains real results. Scripts that read
  `load_test_id` or `result_status` must switch to `test_id` and `result`. The
  k6-manage skill's workarounds for these bugs can go after a release.
- **Dependencies:** none added.
