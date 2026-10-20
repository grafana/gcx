# Design

## Context

See proposal.md for why. The facts below shape the approach. They come from
the k6 Cloud v6 OpenAPI spec (`https://api.k6.io/cloud/v6/openapi`) and from
requests made through gcx's OAuth login on 2026-10-19.

- **Two clients.** gcx calls k6 Cloud through one of two clients that implement
  the same `API` interface:
  - `ProxyClient`, for OAuth contexts, goes through the Grafana k6 plugin at
    `/api/plugins/k6-app/resources/cloud`.
  - `DirectClient`, for service-account contexts, calls `https://api.k6.io`.

  Each endpoint is implemented in both clients, and both need every fix. Only
  `ProxyClient` was exercised live. `DirectClient`'s existing tests cover
  authentication, not endpoints.
- **Runs.** `GET /cloud/v6/load_tests/{id}/test_runs` returns runs oldest-first
  unless `$orderby=created desc` is sent. A page holds at most 1,000 runs
  (`$top`), and `$skip` moves through pages. The response's `@nextLink` is an
  absolute `api.k6.io` URL.
  - A run has `test_id`, `status` and `result`.
  - `status` is one of `created`, `queued`, `initializing`, `running`,
    `processing_metrics`, `completed` or `aborted`.
  - `result` is `passed`, `failed` or `error`, and null while the run is
    unfinished.
  - Runs also carry `result_details` (`type`, `message`, `code`) and
    `status_details` (`type`, `entered`, `extra`).
  - gcx's run type and its test fixtures still use the pre-v6 fields
    `load_test_id` and `result_status`.
- **Schedule routes.**

  | Route | Methods | Notes |
  |---|---|---|
  | `/cloud/v6/load_tests/{id}/schedule` | GET, POST | POST replaces any existing schedule |
  | `/cloud/v6/schedules/{id}` | GET, DELETE | |
  | any schedule route | no PUT | |

  The two kinds of 404 can be told apart by their bodies:
  - An unknown route returns `{"error_msg":"404 Route Not Found"}`.
  - A known route with a missing object returns
    `{"error":{"message":"Resource matching query does not exist: ..."}}`.

  gcx updates with `PUT /schedules/{id}` and deletes with
  `DELETE /load_tests/{id}/schedule`. Both are unknown routes. The delete
  treats any 404 as success.
- **Schedule shape.** A schedule recurs by either `cron` (`schedule`,
  `time_zone`) or `recurrence_rule` (`frequency`, `interval`, `byday`, `until`,
  `count`), never both. `frequency` is one of `HOURLY`, `DAILY`, `WEEKLY`,
  `MONTHLY` or `YEARLY`, and `byday` values are `MO` through `SU`. gcx's
  schedule types only carry `frequency` and `interval`.
- **Load test updates.** `PATCH /cloud/v6/load_tests/{id}` accepts only `name`,
  `k6_version` and the deprecated `baseline_test_run_id`. Moving a test
  between projects needs `PUT /cloud/v6/load_tests/{id}/move`.

### Terms

These are new to gcx's glossary. Tasks add them to `docs/glossary/k6.md` when
the change ships.

- **Load test:** a k6 Cloud test definition (script and settings) in a project.
  Runs refer to it as `test_id`.
- **Test run:** one execution of a load test. Its *status* is where it is in
  its lifecycle; its *result* is the outcome once it finishes.
- **Schedule:** the single schedule attached to a load test. It recurs by a
  cron expression or by a recurrence rule.

## Goals / Non-Goals

**Goals:**

- Both clients behave identically for every fixed operation, and tests cover
  both.
- Test fakes accept only the routes the v6 API documents, so a wrong route
  fails a test instead of passing silently.

**Non-Goals:**

- Removing the duplication between the two clients. That refactor is planned
  in `add-k6-run-commands`.
- Supporting `k6_version` or the deprecated `baseline_test_run_id` in load
  test manifests.

## Decisions

### Decode only the run fields the specs name

The run type changes to decode `id`, `test_id`, `status`, `status_details`,
`result`, `result_details`, `created` and `ended`. `result` is nullable so an
unfinished run shows `null`. The table's TEST ID column shows `test_id`, and
its RESULT column shows `result`, or `-` when that is null.

*Alternative:* pass the whole API object through as a map, so no field is
ever dropped. Rejected because the table codec needs typed fields and gcx
providers decode into typed structs. `add-k6-run-commands` extends the type
for `runs get`.

### Order and page run listings on the server

Listing sends `$orderby=created desc` and pages with `$skip` and `$top`. The
page size is the smaller of the remaining limit and 1,000. Paging stops when
the limit is reached or a page comes back short. This follows the loop
`listLoadTests` already uses. The client method takes a limit, and 0 means
all.
- `gcx k6 runs list` and `gcx k6 test-run runs list` pass their `--limit`.
- `gcx k6 test-run status` passes 1.

The client-side truncation after listing goes away.

*Alternative:* follow `@nextLink`. Rejected because it is an absolute
`api.k6.io` URL, which `ProxyClient` would have to rewrite into the plugin
path. `$skip` works the same way through both clients.

### Carry every recurrence field in both directions

- Schedule reads and writes gain `cron` (`schedule`, `time_zone`).
- Recurrence rules gain `byday`, `until` and `count`.
- Writes send whichever of `cron` or `recurrence_rule` the input sets and
  leave validation to the API.
- The Schedule resource schema and example are updated to match, so
  `gcx resources` validation stays accurate.
- The `gcx k6 schedules list` table gains a RECURRENCE column. A cell reads
  `cron <expression> <time zone>`, or `<FREQUENCY> every <interval>` plus
  ` on <weekdays>` when weekdays are set, or `once`. `until` and `count` stay
  out of the column to keep it short; JSON and YAML carry them.

### Update schedules through the load test's schedule route

`gcx k6 schedules update <id>` and the Schedule adapter's update work in two
steps:
1. Read `GET /schedules/{id}` to learn the schedule's `load_test_id`.
2. Send the full request to `POST /load_tests/{load_test_id}/schedule`.

They return the schedule from the POST response. The commands and positional
arguments stay as they are.

The recurrence-field fix and this route fix ship in the same PR. On its own,
the route fix would let updates replace cron schedules with one-time
schedules, because today's types drop `cron` before sending.

*Alternative:* delete the schedule and then create a new one. Rejected
because it takes two writes where one exists, and a failure between them
loses the schedule.

### Delete schedules through the schedule's own route

`gcx k6 load-tests delete-schedule <load-test-id>` works in two steps:
1. Read `GET /load_tests/{id}/schedule`. A not-found answer means there is
   nothing to delete, so the command succeeds. This keeps today's idempotent
   behavior.
2. Otherwise, call `DELETE /schedules/{schedule-id}`.

The Schedule adapter's delete calls `DELETE /schedules/{id}` directly instead
of reading the schedule to find its load test. A not-found answer from the
correct route still counts as already deleted. Any other error fails the
command.

The `API` interface replaces `DeleteScheduleByLoadTest` with a delete by
schedule ID and a read by load test ID.

### Guard project changes once, for both update paths

The command and the LoadTest adapter's update both run one shared check
before writing:
1. Read the load test.
2. If the input sets a non-zero `project_id` that differs from the test's
   project, fail with an error naming `project_id`, before any write.

*Alternative:* move the test with `/move`. Rejected because moving is a new
operation that needs its own command and maintainer review.

### Test both clients against strict fakes

Run and schedule tests run one table against both clients. The fake server
serves each client's base path and answers anything outside the documented
method and path pairs with the unknown-route 404.

Fixtures move to the v6 shapes, anonymized:
- runs: `test_id`, `result`, `status: completed`, `result_details`
- schedules: cron and full recurrence rules

*Alternative:* check every path the clients call against a vendored OpenAPI
spec. Rejected as more machinery than the defect class needs. Strict fakes
catch the same mistakes.

## Risks / Trade-offs

- **The JSON rename breaks scripts that read `load_test_id` or
  `result_status`.** The old values were always 0, so nothing could have
  relied on them. The PR and changelog call out the rename.
- **The service-account path isn't verified live.** It shares the same
  routes, and the tests cover both clients. The PR states that only OAuth was
  exercised live.
- **A schedule's ID may change when an update replaces it.** The specs don't
  depend on the ID staying the same. `schedules update` reports the schedule
  the API returns. A push from a stale manifest still converges, because an
  update or create both replace the load test's schedule.
- **`--limit 0` on a test with thousands of runs makes several requests.**
  It's an explicit opt-in. The default `--limit 50` makes one request.
- **`delete-schedule` changes from doing nothing to deleting.** That is what
  the command always claimed to do. The changelog says so.

## Migration Plan

There is no persisted state to migrate. Ship in a regular release with a
changelog note for the run JSON field rename and the `delete-schedule`
behavior change. Rollback is a revert.

## Open Questions

- **Does k6 Cloud keep a schedule's ID when an update replaces the
  schedule?** The answer only changes which ID `schedules update` prints. Check
  it during implementation against a throwaway schedule.
