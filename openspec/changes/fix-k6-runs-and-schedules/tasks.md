# Tasks

Each numbered group is one PR, in dependency order. Every group writes its
tests first (red), then the code (green). The last task in each group is the
AGENTS.md PR checklist.

## 1. Test run fields (PR 1)

- [x] 1.1 Replace the pre-v6 run fixtures in `internal/providers/k6/proxy_client_test.go` and `commands_output_test.go` with anonymized v6 runs (`test_id`, `status: completed`, `result`, `result_details`). Add table, JSON and `gcx k6 test-run status` text-output cases for a passed run, a failed run and an unfinished run (`result: null`, no `ended`). Verify the new cases fail against the current run type.
- [x] 1.2 Change the run type to decode `id`, `test_id`, `status`, `status_details`, `result` (nullable), `result_details`, `created` and `ended`. Point the table's TEST ID and RESULT columns at `test_id` and `result` (`-` when null), make `gcx k6 test-run status`'s text output print `result` (`-` when null), and delete the integer result mapping. Verify `go test ./internal/providers/k6/...` passes, that JSON output contains neither `load_test_id` nor `result_status`, and that `test-run status` prints `Result:  failed` for a failed run.
- [x] 1.3 Create `docs/glossary/k6.md` with the terms *load test*, *test run*, run *status* and run *result* from design.md, and add a k6 row to `docs/glossary/README.md`. Verify the README table links to the new file.
- [ ] 1.4 Describe the JSON field rename (old names, new names, old values always 0) in the PR description and mark the commit as output-breaking. Run the AGENTS.md PR checklist (`GCX_AGENT_MODE=false mise run all`) and verify it passes.

## 2. Run ordering and paging (PR 2)

- [ ] 2.1 Add run-listing tests that run against both `ProxyClient` and `DirectClient` with one fake server. The fake asserts `$orderby=created desc`, a `$top` of at most 1,000 and `$skip` paging. Cases: `--limit 2` returns the two newest runs; `--limit 0` over 1,336 runs returns all 1,336, newest first; a test with no runs returns `[]`. Verify the tests fail against today's clients.
- [ ] 2.2 Give the client run-listing method a limit (0 means all) in both clients. Send the ordering and paging parameters, and stop at the limit or at a short page. Verify the 2.1 tests pass for both clients.
- [ ] 2.3 Pass `--limit` from `gcx k6 runs list` and `gcx k6 test-run runs list`, pass 1 from `gcx k6 test-run status`, and remove the client-side truncation. Verify with command tests that `test-run status` reports the newest run and that a test without runs still fails with the "no test runs" error.
- [ ] 2.4 Run the AGENTS.md PR checklist (`GCX_AGENT_MODE=false mise run all`) and verify it passes.

## 3. Schedules: recurrence fields and routes (PR 3)

The recurrence fields and the route fixes must land together. A route fix
alone would let updates replace cron schedules with one-time schedules.

- [ ] 3.1 Build a strict fake server for both clients that accepts only the documented schedule routes: GET and POST on `/cloud/v6/load_tests/{id}/schedule`, and GET and DELETE on `/cloud/v6/schedules/{id}`. Anything else gets the unknown-route 404 body. Add anonymized fixtures for a cron schedule and for a weekly rule with `byday`, `until` and `count`. Verify the existing update and delete tests now fail.
- [ ] 3.2 Add `cron` (`schedule`, `time_zone`) to schedule reads and writes, and `byday`, `until` and `count` to recurrence rules. Update the Schedule resource schema and example. Add the RECURRENCE column to the `gcx k6 schedules list` table. Verify that the "Show every recurrence setting" scenarios pass for `schedules get`, `schedules list` and `gcx resources get`, that the three "Show each schedule's recurrence in the schedules table" scenarios pass, and that the adapter schema tests pass.
- [ ] 3.3 Re-implement schedule updates in both clients: read the schedule's `load_test_id`, then POST the full request to that load test's schedule route. Use this for `gcx k6 schedules update` and the Schedule adapter's update. Verify the "Update to a cron schedule" scenario, and that pulling and pushing a cron schedule leaves its cron expression and time zone unchanged.
- [ ] 3.4 Re-implement schedule deletes in both clients. `gcx k6 load-tests delete-schedule` reads the load test's schedule and then calls `DELETE /cloud/v6/schedules/{id}`. The adapter's delete calls `DELETE /cloud/v6/schedules/{id}` directly. A not-found answer from the correct route means success; any other error fails. Replace `DeleteScheduleByLoadTest` in the `API` interface. Verify the three "Delete a load test's schedule" scenarios pass.
- [ ] 3.5 Add *schedule*, *cron schedule* and *recurrence rule* to `docs/glossary/k6.md`. Describe the `delete-schedule` behavior change in the PR description. Run the AGENTS.md PR checklist (`GCX_AGENT_MODE=false mise run all`) and verify it passes.

## 4. Load test project guard (PR 4)

- [ ] 4.1 Add tests for `gcx k6 load-tests update -f` and the LoadTest adapter's update. A different `project_id` fails with an error naming `project_id`, and the fake asserts no PATCH or script PUT was sent. A matching or omitted `project_id` updates the name. Verify the different-project case fails against today's code.
- [ ] 4.2 Add one shared check, used by both update paths, that reads the load test and rejects a non-zero `project_id` different from its project before any write. Verify the 4.1 tests pass.
- [ ] 4.3 Run the AGENTS.md PR checklist (`GCX_AGENT_MODE=false mise run all`) and verify it passes.

## 5. Live verification

- [ ] 5.1 With a `bin/gcx` built from the merged changes and an OAuth context, verify on a Grafana Cloud stack that:
  - `gcx k6 runs list <test> --limit 3` shows the three newest runs with real results;
  - `gcx k6 test-run status --id <test>` shows the newest run;
  - `gcx k6 schedules list` shows cron schedules with their expression and time zone.
- [ ] 5.2 On a throwaway load test with a throwaway schedule, run `gcx k6 schedules update` with a cron schedule and then `gcx k6 load-tests delete-schedule`. Verify the schedule changes and is then gone. Record whether the update kept the schedule ID, which answers design.md's open question.

## Workflow follow-up

- After a gcx release, remove the k6-manage skill's workarounds for these bugs in grafana/skills: the run-ordering warning and `gcx api` run recipes, the `update -f` warning, and the references to the non-existent `gcx k6 schedules delete`.
- Archive the change after review.
