# k6 Cloud API coverage

This page maps the k6 Cloud API to `gcx` commands. The v6 list uses the
published OpenAPI document. The metrics list uses the published v5 metrics API.

The team also ran a create, read, update, and delete smoke test against a test
stack. The test sent a request to every listed v6 route. The test stack had no
private load zone. The two allowed-project requests therefore confirmed the
documented HTTP 400 response for a public zone. All other temporary-resource
operations succeeded. The test also covered the v5 metrics routes, logs, and
insights. The test removed all temporary resources.

## v6 resource API

| Operation | `gcx` coverage |
| --- | --- |
| `GET /cloud/v6/auth` | `gcx k6 auth validate` |
| `GET /cloud/v6/labels` | `gcx k6 label-keys list` |
| `POST /cloud/v6/labels` | `gcx k6 label-keys create` |
| `PATCH /cloud/v6/labels/{id}` | `gcx k6 label-keys update` |
| `DELETE /cloud/v6/labels/{id}` | `gcx k6 label-keys delete` |
| `GET /cloud/v6/load_tests` | `gcx k6 load-tests list` |
| `GET /cloud/v6/load_tests/{id}/schedule` | `gcx k6 load-tests get-schedule` |
| `POST /cloud/v6/load_tests/{id}/schedule` | `gcx k6 schedules create` |
| `GET /cloud/v6/load_tests/{id}/test_runs` | `gcx k6 runs list <load-test-id>` |
| `GET /cloud/v6/load_tests/{id}` | `gcx k6 load-tests get` |
| `PATCH /cloud/v6/load_tests/{id}` | `gcx k6 load-tests update` |
| `DELETE /cloud/v6/load_tests/{id}` | `gcx k6 load-tests delete` |
| `PUT /cloud/v6/load_tests/{id}/move` | `gcx k6 load-tests move` |
| `GET /cloud/v6/load_tests/{id}/script` | `gcx k6 load-tests get-script` |
| `PUT /cloud/v6/load_tests/{id}/script` | `gcx k6 load-tests update-script` |
| `POST /cloud/v6/load_tests/{id}/start` | `gcx k6 load-tests start` |
| `GET /cloud/v6/load_zones` | `gcx k6 load-zones list` |
| `GET /cloud/v6/load_zones/{id}/allowed_projects` | `gcx k6 load-zones list-allowed-projects` |
| `PUT /cloud/v6/load_zones/{id}/allowed_projects` | `gcx k6 load-zones update-allowed-projects` |
| `GET /cloud/v6/project-limits` | `gcx k6 project-limits list` |
| `GET /cloud/v6/projects` | `gcx k6 projects list` |
| `POST /cloud/v6/projects` | `gcx k6 projects create` |
| `GET /cloud/v6/projects/{id}/allowed_load_zones` | `gcx k6 projects list-allowed-load-zones` |
| `PUT /cloud/v6/projects/{id}/allowed_load_zones` | `gcx k6 projects update-allowed-load-zones` |
| `GET /cloud/v6/projects/{id}/limits` | `gcx k6 projects get-limits` |
| `PATCH /cloud/v6/projects/{id}/limits` | `gcx k6 projects update-limits` |
| `GET /cloud/v6/projects/{id}/load_tests` | `gcx k6 load-tests list --project-id` |
| `POST /cloud/v6/projects/{id}/load_tests` | `gcx k6 load-tests create` |
| `GET /cloud/v6/projects/{id}` | `gcx k6 projects get` |
| `PATCH /cloud/v6/projects/{id}` | `gcx k6 projects update` |
| `DELETE /cloud/v6/projects/{id}` | `gcx k6 projects delete` |
| `GET /cloud/v6/projects/{project_id}/labels` | `gcx k6 projects list-labels` |
| `PUT /cloud/v6/projects/{project_id}/labels` | `gcx k6 projects update-labels` |
| `DELETE /cloud/v6/projects/{project_id}/labels/{key}` | Use `gcx k6 projects update-labels` with the required replacement list. This keeps one clear replacement workflow. |
| `GET /cloud/v6/schedules` | `gcx k6 schedules list` |
| `GET /cloud/v6/schedules/{id}` | `gcx k6 schedules get` |
| `DELETE /cloud/v6/schedules/{id}` | `gcx k6 schedules delete` |
| `POST /cloud/v6/schedules/{id}/activate` | `gcx k6 schedules activate` |
| `POST /cloud/v6/schedules/{id}/deactivate` | `gcx k6 schedules deactivate` |
| `GET /cloud/v6/test_runs` | `gcx k6 runs list` without a load-test ID |
| `GET /cloud/v6/test_runs/{id}` | `gcx k6 runs get` |
| `PATCH /cloud/v6/test_runs/{id}` | `gcx k6 runs update` |
| `DELETE /cloud/v6/test_runs/{id}` | `gcx k6 runs delete` |
| `POST /cloud/v6/test_runs/{id}/abort` | `gcx k6 runs abort` |
| `GET /cloud/v6/test_runs/{id}/distribution` | `gcx k6 runs get-distribution` |
| `POST /cloud/v6/test_runs/{id}/save` | Intentionally omitted. The upstream API deprecates this operation and specifies `star` as its replacement. |
| `GET /cloud/v6/test_runs/{id}/script` | `gcx k6 runs get-script` |
| `POST /cloud/v6/test_runs/{id}/star` | `gcx k6 runs star` |
| `POST /cloud/v6/test_runs/{id}/unsave` | Intentionally omitted. The upstream API deprecates this operation and specifies `unstar` as its replacement. |
| `POST /cloud/v6/test_runs/{id}/unstar` | `gcx k6 runs unstar` |
| `POST /cloud/v6/validate_options` | `gcx k6 options validate` |

The existing `gcx k6 schedules update` command uses an API route that is not in
the v6 OpenAPI document. This page does not count that route as published v6
coverage.

## v5 metrics API

| Operation | `gcx` coverage |
| --- | --- |
| `GET /cloud/v5/test_runs/{id}/metrics` | `gcx k6 runs list-metrics` |
| `GET /cloud/v5/load_tests/{id}/metrics(...)` | `gcx k6 load-tests list-metrics` |
| `GET /cloud/v5/test_runs/{id}/series` | `gcx k6 runs list-series` |
| `GET /cloud/v5/test_runs/{id}/labels` | `gcx k6 runs list-labels` |
| `GET /cloud/v5/test_runs/{id}/label/{name}/values` | `gcx k6 runs list-labels --label` |
| `GET /cloud/v5/test_runs/{id}/query_range_k6(...)` | `gcx k6 runs query` |
| `GET /cloud/v5/test_runs/{id}/query_aggregate_k6(...)` | `gcx k6 runs query --aggregate` |
| `GET /cloud/v5/load_tests/{id}/query_aggregate_k6(...)` | `gcx k6 load-tests query` |

The `/ms` routes are aliases for metric-list routes. They do not need separate
commands.

## Run diagnostics

`gcx k6 runs list-logs` reads the selected run from the k6 Cloud logs API.
`gcx k6 runs get-insights` reads the latest insight execution and joins its
audit definitions with its results. `gcx k6 runs traces list` and
`gcx k6 runs traces get` inspect browser traces through the k6 Tempo route.
`gcx k6 runs artifacts list` and `gcx k6 runs artifacts download` discover and
download browser screenshots through the k6 files API. These trace and file
routes are internal k6 service routes. Their response contracts can change.

`gcx k6 runs wait` polls the published v6 run endpoint until execution and
metric processing finish. It returns a nonzero exit code when the final run
result does not pass.
