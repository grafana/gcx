package checks_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/grafana/gcx/internal/providers/synth/checks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests pin `checks create|update --dry-run`: the check is validated by
// the SM API (POST check/validate) and nothing is persisted.

var (
	//nolint:gochecknoglobals // Test fixtures shared across test functions.
	validValidateBody = map[string]any{"valid": true, "findings": []any{}}
	//nolint:gochecknoglobals // Test fixtures shared across test functions.
	invalidValidateBody = map[string]any{
		"valid": false,
		"findings": []map[string]string{
			{"severity": "error", "field": "", "msg": "invalid check timeout"},
			{"severity": "error", "field": "probes", "msg": "invalid probe identifier"},
		},
	}
)

// writeUnknownProbeManifest writes a manifest naming a probe the fake SM API
// does not know, so local validation rejects it.
func writeUnknownProbeManifest(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "check.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`apiVersion: syntheticmonitoring.ext.grafana.app/v1alpha1
kind: Check
metadata:
  name: web-check
spec:
  job: web-check
  target: https://example.com
  frequency: 60000
  timeout: 10000
  enabled: true
  probes:
    - Nowhere
  settings:
    http:
      method: GET
`), 0o600))
	return path
}

func TestChecksCreateDryRun(t *testing.T) {
	t.Run("valid check is validated and not created", func(t *testing.T) {
		st := &checkAPIState{probesOnline: true, validateStatus: 200, validateBody: validValidateBody}
		srv := newCheckServer(t, st)
		manifest := writeCheckManifest(t, t.TempDir())
		before, err := os.ReadFile(manifest)
		require.NoError(t, err)

		stdout, _, err := runChecks(t, srv.URL, false, "", "create", "-f", manifest, "--dry-run")
		require.NoError(t, err)

		assert.Equal(t, "✔ Check \"web-check\" is valid (dry-run: nothing was created)\n", stdout)
		assert.Equal(t, 1, st.validateCalls)
		assert.Equal(t, 0, st.writes, "dry-run must not create anything")
		assert.NotContains(t, st.lastValidate, "id", "a create has no check ID")
		assert.Equal(t, "web-check", st.lastValidate["job"])

		after, err := os.ReadFile(manifest)
		require.NoError(t, err)
		assert.Equal(t, string(before), string(after), "dry-run must not rewrite the manifest")
	})

	t.Run("agent mode emits one JSON document", func(t *testing.T) {
		st := &checkAPIState{probesOnline: true, validateStatus: 200, validateBody: validValidateBody}
		srv := newCheckServer(t, st)
		manifest := writeCheckManifest(t, t.TempDir())

		stdout, _, err := runChecks(t, srv.URL, true, "", "create", "-f", manifest, "--dry-run")
		require.NoError(t, err)

		doc, ok := decodeSingleJSONValue(t, stdout).(map[string]any)
		require.True(t, ok, "dry-run result must be a JSON object")
		assert.Equal(t, "gcx.synth.check_create", doc["type"])
		assert.Equal(t, "validated", doc["action"])
		assert.Equal(t, "web-check", doc["job"])
	})

	t.Run("invalid check fails with every finding and writes nothing", func(t *testing.T) {
		st := &checkAPIState{probesOnline: true, validateStatus: 422, validateBody: invalidValidateBody}
		srv := newCheckServer(t, st)
		manifest := writeCheckManifest(t, t.TempDir())

		stdout, _, err := runChecks(t, srv.URL, false, "", "create", "-f", manifest, "--dry-run")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid check timeout")
		assert.Contains(t, err.Error(), "probes: invalid probe identifier")
		assert.Empty(t, stdout, "stdout is reserved for the success document")
		assert.Equal(t, 0, st.writes)
	})

	t.Run("warnings go to stderr and do not fail", func(t *testing.T) {
		body := map[string]any{
			"valid":    true,
			"findings": []map[string]string{{"severity": "warning", "field": "frequency", "msg": "below the app minimum"}},
		}
		st := &checkAPIState{probesOnline: true, validateStatus: 200, validateBody: body}
		srv := newCheckServer(t, st)
		manifest := writeCheckManifest(t, t.TempDir())

		stdout, stderr, err := runChecks(t, srv.URL, false, "", "create", "-f", manifest, "--dry-run")
		require.NoError(t, err)
		assert.Contains(t, stderr, "frequency: below the app minimum")
		assert.NotContains(t, stdout, "below the app minimum")
	})

	t.Run("server without the endpoint is an error, not a false success", func(t *testing.T) {
		st := &checkAPIState{probesOnline: true} // validateStatus 0 => 404
		srv := newCheckServer(t, st)
		manifest := writeCheckManifest(t, t.TempDir())

		stdout, _, err := runChecks(t, srv.URL, false, "", "create", "-f", manifest, "--dry-run")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not support check validation")
		assert.Empty(t, stdout)
		assert.Equal(t, 0, st.writes)
	})

	t.Run("local validation still runs first", func(t *testing.T) {
		st := &checkAPIState{probesOnline: true, validateStatus: 200, validateBody: validValidateBody}
		srv := newCheckServer(t, st)
		manifest := writeUnknownProbeManifest(t, t.TempDir())

		_, _, err := runChecks(t, srv.URL, false, "", "create", "-f", manifest, "--dry-run")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "check validation failed")
		assert.Equal(t, 0, st.validateCalls, "a locally invalid check must not reach the server")
	})

	t.Run("cannot be combined with --show-status", func(t *testing.T) {
		st := &checkAPIState{probesOnline: true, validateStatus: 200, validateBody: validValidateBody}
		srv := newCheckServer(t, st)
		manifest := writeCheckManifest(t, t.TempDir())

		_, _, err := runChecks(t, srv.URL, false, "", "create", "-f", manifest, "--dry-run", "--show-status")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "--dry-run")
		assert.Equal(t, 0, st.validateCalls)
	})
}

// existingChecks seeds the fake SM API with check 1234, the ID in
// "web-check-1234". It returns a fresh map per call because the fake server
// mutates its check map on create.
func existingChecks() map[int64]checks.Check {
	return map[int64]checks.Check{1234: {ID: 1234}}
}

func TestChecksUpdateDryRun(t *testing.T) {
	t.Run("nonexistent check fails before any validation", func(t *testing.T) {
		st := &checkAPIState{probesOnline: true, validateStatus: 200, validateBody: validValidateBody}
		srv := newCheckServer(t, st)
		manifest := writeCheckManifest(t, t.TempDir())

		stdout, _, err := runChecks(t, srv.URL, false, "", "update", "web-check-1234", "-f", manifest, "--dry-run")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
		assert.Contains(t, err.Error(), "1234")
		assert.Empty(t, stdout, "a missing check must not report success")
		assert.Equal(t, 0, st.validateCalls, "a missing check must not reach the validate endpoint")
		assert.Equal(t, 0, st.writes)
	})

	t.Run("existence is checked before local validation", func(t *testing.T) {
		st := &checkAPIState{probesOnline: true, validateStatus: 200, validateBody: validValidateBody}
		srv := newCheckServer(t, st)
		manifest := writeUnknownProbeManifest(t, t.TempDir())

		_, _, err := runChecks(t, srv.URL, false, "", "update", "web-check-1234", "-f", manifest, "--dry-run")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
		assert.NotContains(t, err.Error(), "check validation failed", "the missing check outranks local findings")
	})

	t.Run("existing check with a locally invalid spec still fails local validation", func(t *testing.T) {
		st := &checkAPIState{checks: existingChecks(), probesOnline: true, validateStatus: 200, validateBody: validValidateBody}
		srv := newCheckServer(t, st)
		manifest := writeUnknownProbeManifest(t, t.TempDir())

		_, _, err := runChecks(t, srv.URL, false, "", "update", "web-check-1234", "-f", manifest, "--dry-run")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "check validation failed")
		assert.Equal(t, 0, st.validateCalls)
	})

	t.Run("validates against the check's own ID and does not update", func(t *testing.T) {
		st := &checkAPIState{checks: existingChecks(), probesOnline: true, validateStatus: 200, validateBody: validValidateBody}
		srv := newCheckServer(t, st)
		manifest := writeCheckManifest(t, t.TempDir())

		stdout, _, err := runChecks(t, srv.URL, false, "", "update", "web-check-1234", "-f", manifest, "--dry-run")
		require.NoError(t, err)

		assert.Equal(t, "✔ Check \"web-check\" (id=1234) is valid (dry-run: nothing was updated)\n", stdout)
		assert.InDelta(t, 1234, st.lastValidate["id"], 0)
		assert.Equal(t, 0, st.writes, "dry-run must not update anything")
	})

	t.Run("agent mode emits one JSON document", func(t *testing.T) {
		st := &checkAPIState{checks: existingChecks(), probesOnline: true, validateStatus: 200, validateBody: validValidateBody}
		srv := newCheckServer(t, st)
		manifest := writeCheckManifest(t, t.TempDir())

		stdout, _, err := runChecks(t, srv.URL, true, "", "update", "web-check-1234", "-f", manifest, "--dry-run")
		require.NoError(t, err)

		doc, ok := decodeSingleJSONValue(t, stdout).(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "gcx.synth.check_update", doc["type"])
		assert.Equal(t, "validated", doc["action"])
		assert.Equal(t, 1234, jsonInt(t, doc["id"]))
		assert.Equal(t, "web-check-1234", doc["name"])
	})

	t.Run("invalid check fails and writes nothing", func(t *testing.T) {
		st := &checkAPIState{checks: existingChecks(), probesOnline: true, validateStatus: 422, validateBody: invalidValidateBody}
		srv := newCheckServer(t, st)
		manifest := writeCheckManifest(t, t.TempDir())

		_, _, err := runChecks(t, srv.URL, false, "", "update", "web-check-1234", "-f", manifest, "--dry-run")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid check timeout")
		assert.Equal(t, 0, st.writes)
	})

	t.Run("cannot be combined with --show-status", func(t *testing.T) {
		st := &checkAPIState{probesOnline: true, validateStatus: 200, validateBody: validValidateBody}
		srv := newCheckServer(t, st)
		manifest := writeCheckManifest(t, t.TempDir())

		_, _, err := runChecks(t, srv.URL, false, "", "update", "web-check-1234", "-f", manifest, "--dry-run", "--show-status")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "--dry-run")
	})

	t.Run("failed existence lookup is reported as a lookup error", func(t *testing.T) {
		st := &checkAPIState{failGet: true, probesOnline: true, validateStatus: 200, validateBody: validValidateBody}
		srv := newCheckServer(t, st)
		manifest := writeCheckManifest(t, t.TempDir())

		stdout, _, err := runChecks(t, srv.URL, false, "", "update", "web-check-1234", "-f", manifest, "--dry-run")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "looking up check")
		assert.NotContains(t, err.Error(), "not found", "a server error is not a missing check")
		assert.Empty(t, stdout)
		assert.Equal(t, 0, st.validateCalls)
	})

	t.Run("server without the endpoint is an error, not a false success", func(t *testing.T) {
		st := &checkAPIState{checks: existingChecks(), probesOnline: true} // validateStatus 0 => 404
		srv := newCheckServer(t, st)
		manifest := writeCheckManifest(t, t.TempDir())

		stdout, _, err := runChecks(t, srv.URL, false, "", "update", "web-check-1234", "-f", manifest, "--dry-run")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not support check validation")
		assert.Empty(t, stdout)
		assert.Equal(t, 0, st.writes)
	})

	t.Run("warnings go to stderr and do not fail", func(t *testing.T) {
		body := map[string]any{
			"valid":    true,
			"findings": []map[string]string{{"severity": "warning", "field": "frequency", "msg": "below the app minimum"}},
		}
		st := &checkAPIState{checks: existingChecks(), probesOnline: true, validateStatus: 200, validateBody: body}
		srv := newCheckServer(t, st)
		manifest := writeCheckManifest(t, t.TempDir())

		stdout, stderr, err := runChecks(t, srv.URL, false, "", "update", "web-check-1234", "-f", manifest, "--dry-run")
		require.NoError(t, err)
		assert.Contains(t, stderr, "frequency: below the app minimum")
		assert.NotContains(t, stdout, "below the app minimum")
	})
}

// A plain create/update must never call validate: the server already runs the
// same checks on add/update, so a pre-flight would only add a round trip.
func TestChecksCreateUpdateDoNotCallValidate(t *testing.T) {
	st := &checkAPIState{probesOnline: true, validateStatus: 200, validateBody: validValidateBody}
	srv := newCheckServer(t, st)
	manifest := writeCheckManifest(t, t.TempDir())

	_, _, err := runChecks(t, srv.URL, false, "", "create", "-f", manifest)
	require.NoError(t, err)
	_, _, err = runChecks(t, srv.URL, false, "", "update", "web-check-1234", "-f", manifest)
	require.NoError(t, err)

	assert.Equal(t, 0, st.validateCalls)
	assert.Equal(t, 2, st.writes)
}
