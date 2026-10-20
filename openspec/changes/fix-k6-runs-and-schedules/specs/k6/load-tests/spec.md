# Spec Delta

## Purpose

Lets users manage k6 Cloud load tests from manifests and commands, and tells them plainly when a requested change cannot be applied instead of reporting success.

## ADDED Requirements

### Requirement: Reject load test updates that change the project
`gcx k6 load-tests update -f` and LoadTest updates through `gcx resources push` SHALL fail before changing anything when the input sets `project_id` to a value other than the load test's current project. The error SHALL name `project_id`. When `project_id` is omitted or matches the current project, the update SHALL proceed.

#### Scenario: Different project
- **WHEN** a load test belongs to project 1 and `gcx k6 load-tests update <id> -f test.yaml` supplies `project_id: 2` and a new name
- **THEN** the command exits non-zero with an error naming `project_id`, and the test's name, script and project are unchanged

#### Scenario: Same project
- **WHEN** the input's `project_id` matches the load test's project and the input changes its name
- **THEN** the update succeeds and the load test has the new name

#### Scenario: Project omitted
- **WHEN** the input omits `project_id` and changes the load test's name
- **THEN** the update succeeds and the load test has the new name
