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

// wantBatchOnly are the fields set only for a batch resource operation that ran
// to a finalized count. They travel together: present for such an operation,
// absent for every other invocation. Their presence says the operation
// completed, not that the user was shown a summary.
func wantBatchOnly() []string {
	return []string{
		"batch_succeeded_bucket", "batch_failed_bucket", "batch_skipped_bucket", "dry_run",
	}
}

// wantErrorSignalsOnly are the failure-depth fields, set only when the
// surfaced error carried a transport HTTP status or a Kubernetes reason, and
// suppressed for exit codes 4 and 5. Unlike the batch block they do not
// travel together: most failures carry exactly one of the two shapes.
func wantErrorSignalsOnly() []string {
	return []string{"http_status", "k8s_reason"}
}

// wantGrafanaAuthOnly is the auth-method field, present whenever a Grafana
// auth selection was decided — on any outcome, including success and
// canceled — and absent when none was, so it is not part of the error group.
func wantGrafanaAuthOnly() []string {
	return []string{"grafana_auth_method"}
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

		HTTPStatus:        403,
		K8sReason:         "NotFound",
		GrafanaAuthMethod: "token",
	}

	got := marshalKeys(t, full)
	want := append(wantAlwaysPresent(), wantParseErrorOnly()...)
	want = append(want, wantBatchOnly()...)
	want = append(want, wantErrorSignalsOnly()...)
	want = append(want, wantGrafanaAuthOnly()...)
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

// Absence is meaningful for all three new optional fields: no status found,
// no Kubernetes reason found, no auth selection decided. Zero values must
// vanish rather than travel as 0 or "".
func TestEventOmitsErrorSignalAndAuthFieldsWhenUnset(t *testing.T) {
	got := marshalKeys(t, telemetry.Event{Outcome: telemetry.OutcomeOK})
	for _, field := range append(wantErrorSignalsOnly(), wantGrafanaAuthOnly()...) {
		assert.NotContains(t, got, field, "events without the signal must omit the field entirely")
	}
}

func TestK8sReasonLabelClampsToVocabulary(t *testing.T) {
	assert.Empty(t, telemetry.K8sReasonLabel(""), "no reason found means the field is omitted, not sent as unknown")
	assert.Equal(t, telemetry.K8sReasonOther, telemetry.K8sReasonLabel("SomeFutureReason"),
		"a StatusReason is a server-controlled string and must never travel verbatim")
	assert.Equal(t, "StorageReadError", telemetry.K8sReasonLabel("StorageReadError"),
		"the vocabulary holds wire values: the Go identifier is StatusReasonStoreReadError")

	for _, label := range telemetry.K8sReasonLabels() {
		if label == telemetry.K8sReasonOther {
			continue
		}
		assert.Equal(t, label, telemetry.K8sReasonLabel(label), "every listed reason passes through unchanged")
	}
	assert.Len(t, telemetry.K8sReasonLabels(), 20, "19 reasons plus the other sentinel is the receiver-gated contract")
}

func TestGrafanaAuthMethodLabelClampsToVocabulary(t *testing.T) {
	assert.Empty(t, telemetry.GrafanaAuthMethodLabel(""), "no decision means the field is omitted")
	assert.Equal(t, telemetry.AuthMethodUnknown, telemetry.GrafanaAuthMethodLabel("Bearer xyz"),
		"an out-of-contract value evidences a decision but must not travel verbatim")

	for _, label := range telemetry.GrafanaAuthMethodLabels() {
		assert.Equal(t, label, telemetry.GrafanaAuthMethodLabel(label), "every listed method passes through unchanged")
	}
	assert.Len(t, telemetry.GrafanaAuthMethodLabels(), 6, "six values is the receiver-gated contract")
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
