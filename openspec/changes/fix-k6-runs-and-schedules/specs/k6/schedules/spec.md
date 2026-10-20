# Spec Delta

## Purpose

Lets users read and change the schedules that run k6 Cloud load tests without losing any recurrence setting, whether the schedule uses a cron expression or a recurrence rule.

## ADDED Requirements

### Requirement: Show every recurrence setting of a schedule
`gcx k6 schedules list`, `gcx k6 schedules get`, and Schedule reads through `gcx resources get` and `gcx resources pull` SHALL include the schedule's `cron` (`schedule` and `time_zone`) and its recurrence rule's `frequency`, `interval`, `byday`, `until` and `count`, as k6 Cloud returns them. `frequency` SHALL accept `HOURLY`, `DAILY`, `WEEKLY`, `MONTHLY` and `YEARLY`.

#### Scenario: Cron schedule
- **WHEN** a schedule runs on cron expression `20 13 * * *` in time zone `Europe/London`
- **THEN** `gcx k6 schedules get <id> -o yaml` shows `cron.schedule: 20 13 * * *` and `cron.time_zone: Europe/London`

#### Scenario: Weekly recurrence rule
- **WHEN** a schedule recurs weekly on Mondays and Fridays until a fixed date
- **THEN** the output shows `frequency: WEEKLY`, `byday: [MO, FR]` and that `until` date

### Requirement: Show each schedule's recurrence in the schedules table
The table output of `gcx k6 schedules list` SHALL include a RECURRENCE column. It SHALL show `cron` with the expression and time zone for a cron schedule, the frequency, interval and any weekdays for a recurrence rule, and `once` for a schedule with neither.

#### Scenario: Cron schedule in the table
- **WHEN** a schedule runs on cron expression `20 13 * * *` in time zone `Europe/London`
- **THEN** its RECURRENCE cell reads `cron 20 13 * * * Europe/London`

#### Scenario: Recurrence rule in the table
- **WHEN** a schedule recurs every 2 weeks on Mondays and Fridays
- **THEN** its RECURRENCE cell reads `WEEKLY every 2 on MO,FR`

#### Scenario: One-time schedule in the table
- **WHEN** a schedule has neither a cron expression nor a recurrence rule
- **THEN** its RECURRENCE cell reads `once`

### Requirement: Write schedules with every recurrence setting
`gcx k6 schedules create`, `gcx k6 schedules update`, and Schedule creates and updates through `gcx resources push` SHALL send `starts` together with the input's `cron` or recurrence rule, including every recurrence field the input sets. An update SHALL replace the load test's existing schedule.

#### Scenario: Update to a cron schedule
- **WHEN** `gcx k6 schedules update <id> -f schedule.yaml` supplies cron expression `0 10 * * *` in time zone `UTC`
- **THEN** the command succeeds and a following `gcx k6 schedules list` shows that load test's schedule with that cron expression and time zone

#### Scenario: Pull and push round trip
- **WHEN** a cron schedule is pulled with `gcx resources pull` and pushed back unchanged with `gcx resources push`
- **THEN** the schedule's cron expression and time zone are unchanged

### Requirement: Delete a load test's schedule
`gcx k6 load-tests delete-schedule <load-test-id>` SHALL delete that load test's schedule, and Schedule deletes through `gcx resources delete` SHALL delete the addressed schedule. They SHALL report success only after the schedule is deleted, or when the load test has no schedule.

#### Scenario: Scheduled load test
- **WHEN** a load test has a schedule and `gcx k6 load-tests delete-schedule <load-test-id>` runs
- **THEN** the command reports success and the schedule no longer appears in `gcx k6 schedules list`

#### Scenario: Load test without a schedule
- **WHEN** a load test has no schedule and `gcx k6 load-tests delete-schedule <load-test-id>` runs
- **THEN** the command succeeds and nothing changes

#### Scenario: Delete fails upstream
- **WHEN** k6 Cloud rejects the delete request with an error other than not-found
- **THEN** the command exits non-zero and reports the error instead of success
