package telemetry_test

import (
	"encoding/json"
	"testing"

	"github.com/grafana/gcx/internal/telemetry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The exact JSON field inventory of the event schema. Any change here changes
// the public usage-stats contract (BigQuery columns and the documented field
// list) and must be reflected in the usage-stats flattener and the docs.
func wantAlwaysPresent() []string {
	return []string{
		"service", "version", "os", "arch",
		"device_id", "device_id_persisted",
		"command", "flags", "provider", "outcome", "exit_code", "error_kind", "duration_ms",
		"is_tty", "is_ci", "ci_provider", "is_agent", "agent", "target_kind", "output_format",
	}
}

func wantParseErrorOnly() []string {
	return []string{
		"parse_error_kind", "parse_error_parent", "parse_error_token",
		"attempted_command", "parse_error_flags", "parse_error_nearest", "parse_error_distance",
	}
}

// wantBatchOnly are the fields set only for a batch resource operation that
// emitted a final result document. They travel together: present for such an
// operation, absent for every other invocation.
func wantBatchOnly() []string {
	return []string{
		"batch_succeeded_bucket", "batch_failed_bucket", "batch_skipped_bucket", "dry_run",
	}
}

func marshalKeys(t *testing.T, ev telemetry.Event) map[string]any {
	t.Helper()
	data, err := json.Marshal(ev)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(data, &m))
	return m
}

func TestEventFieldInventory(t *testing.T) {
	appliedRun := false
	full := telemetry.Event{
		Service:            telemetry.ServiceName,
		Version:            "0.4.1",
		OS:                 "linux",
		Arch:               "arm64",
		DeviceID:           "00000000-0000-4000-8000-000000000000",
		DeviceIDPersisted:  true,
		Command:            "dashboards push",
		Flags:              "dry-run,folder",
		Provider:           "dashboards",
		Outcome:            telemetry.OutcomeParseError,
		ExitCode:           2,
		ErrorKind:          "usage_error",
		DurationMS:         1234,
		IsTTY:              true,
		IsCI:               true,
		CIProvider:         "github_actions",
		IsAgent:            true,
		Agent:              "claude-code",
		TargetKind:         "cloud",
		OutputFormat:       "json",
		ParseErrorKind:     "unknown_command",
		ParseErrorParent:   "dashboards",
		ParseErrorToken:    "serch",
		AttemptedCommand:   "dashboards serch",
		ParseErrorFlags:    "verbsoe",
		ParseErrorNearest:  "search",
		ParseErrorDistance: 2,

		BatchSucceededBucket: strPtr(telemetry.BucketToHundred),
		BatchFailedBucket:    strPtr(telemetry.BucketZero),
		BatchSkippedBucket:   strPtr(telemetry.BucketZero),
		DryRun:               &appliedRun,
	}

	got := marshalKeys(t, full)
	want := append(wantAlwaysPresent(), wantParseErrorOnly()...)
	want = append(want, wantBatchOnly()...)
	assert.ElementsMatch(t, want, keys(got), "full event must emit exactly the documented field set")
}

func TestEventOmitsParseFieldsWhenUnset(t *testing.T) {
	got := marshalKeys(t, telemetry.Event{Outcome: telemetry.OutcomeOK})
	assert.ElementsMatch(t, wantAlwaysPresent(), keys(got),
		"non-parse-error events must omit parse_error_* and keep all other fields, even zero-valued")
}

// A non-batch invocation must carry no batch fields at all: absence is what
// distinguishes "not a batch operation" from "a batch that matched nothing".
func TestEventOmitsBatchFieldsWhenUnset(t *testing.T) {
	got := marshalKeys(t, telemetry.Event{Outcome: telemetry.OutcomeOK})
	for _, field := range wantBatchOnly() {
		assert.NotContains(t, got, field, "non-batch events must omit every batch field")
	}
}

// The zero bucket and a false dry-run are real answers, so omitempty must not
// drop them. This is the case that makes the fields pointers.
func TestEventKeepsZeroBatchValues(t *testing.T) {
	appliedRun := false
	got := marshalKeys(t, telemetry.Event{
		Outcome:              telemetry.OutcomeOK,
		BatchSucceededBucket: strPtr(telemetry.BucketZero),
		BatchFailedBucket:    strPtr(telemetry.BucketZero),
		BatchSkippedBucket:   strPtr(telemetry.BucketZero),
		DryRun:               &appliedRun,
	})

	assert.Equal(t, telemetry.BucketZero, got["batch_succeeded_bucket"],
		"a batch that matched nothing must report bucket 0, not vanish")
	assert.Equal(t, false, got["dry_run"], "dry_run=false must survive omitempty")
}

func strPtr(s string) *string { return &s } //nolint:modernize // new(string) gives *"", not a pointer to the given value.

func TestEventNoNearMatchDistanceSurvives(t *testing.T) {
	// -1 (novel guess, no near match) must not be dropped by omitempty.
	got := marshalKeys(t, telemetry.Event{Outcome: telemetry.OutcomeParseError, ParseErrorDistance: -1})
	assert.InDelta(t, float64(-1), got["parse_error_distance"], 0)
}

func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
