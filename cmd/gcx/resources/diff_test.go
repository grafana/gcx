package resources_test

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/cmd/gcx/resources"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/resources/observation"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeTestCapture(t *testing.T, dir, namespace string, dashboards map[string]string) {
	t.Helper()
	objs := map[string]map[string]any{}
	for name, title := range dashboards {
		objs[name] = map[string]any{
			"apiVersion": "dashboard.grafana.app/v1beta1",
			"kind":       "Dashboard",
			"metadata":   map[string]any{"name": name},
			"spec":       map[string]any{"title": title},
		}
	}
	c := &observation.Capture{
		Manifest: observation.Manifest{
			Type:                 observation.ManifestType,
			SchemaVersion:        observation.SchemaVersion,
			NormalizationVersion: observation.NormalizationVersion,
			Stack:                observation.StackIdentity{Server: "https://example.grafana.net", Namespace: namespace},
			Selectors:            []string{"dashboards"},
			Types: []observation.TypeCoverage{{
				Type: observation.TypeDashboards, Selector: "dashboards", Version: "v1beta1", Kind: "Dashboard",
				Status: observation.CoverageComplete, Count: len(objs),
			}},
		},
		Objects: map[string]map[string]map[string]any{observation.TypeDashboards: objs},
	}
	require.NoError(t, observation.Write(c, dir))
}

// newTestRoot mounts the resources command under a parent, as the real CLI
// does: its PersistentPreRun calls the root's hook.
func newTestRoot(out *bytes.Buffer, args ...string) *cobra.Command {
	root := &cobra.Command{Use: "gcx", SilenceErrors: true, SilenceUsage: true}
	root.AddCommand(resources.Command())
	root.SetOut(out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs(append([]string{"resources"}, args...))
	return root
}

func runDiff(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := newTestRoot(&out, append([]string{"diff"}, args...)...).Execute()
	return out.String(), err
}

func TestDiffCmd(t *testing.T) {
	root := t.TempDir()
	before := filepath.Join(root, "before")
	after := filepath.Join(root, "after")
	other := filepath.Join(root, "other")
	writeTestCapture(t, before, "stacks-1", map[string]string{"a": "A", "b": "B"})
	writeTestCapture(t, after, "stacks-1", map[string]string{"a": "A2", "c": "C"})
	writeTestCapture(t, other, "stacks-2", map[string]string{"a": "A"})

	t.Run("json result", func(t *testing.T) {
		out, err := runDiff(t, before, after, "-o", "json")
		require.NoError(t, err)
		var res observation.DiffResult
		require.NoError(t, json.Unmarshal([]byte(out), &res))
		assert.Equal(t, observation.DiffType, res.Type)
		assert.True(t, res.Complete)
		assert.Equal(t, observation.DiffSummary{Added: 1, Modified: 1, Deleted: 1}, res.Summary)
	})

	t.Run("text result", func(t *testing.T) {
		out, err := runDiff(t, before, after, "-o", "text")
		require.NoError(t, err)
		assert.Contains(t, out, "1 added, 1 modified, 1 deleted, 0 unchanged, 0 indeterminate")
		assert.Contains(t, out, "/spec/title")
	})

	t.Run("different stacks are rejected", func(t *testing.T) {
		_, err := runDiff(t, before, other)
		var detailed gcxerrors.DetailedError
		require.ErrorAs(t, err, &detailed)
		assert.Equal(t, "Captures are not comparable", detailed.Summary)
	})

	t.Run("unfinished capture is rejected", func(t *testing.T) {
		_, err := runDiff(t, before, t.TempDir())
		var detailed gcxerrors.DetailedError
		require.ErrorAs(t, err, &detailed)
		assert.Equal(t, "Invalid capture", detailed.Summary)
	})
}

func TestCaptureCmd_RequiresPath(t *testing.T) {
	err := newTestRoot(&bytes.Buffer{}, "capture", "dashboards").Execute()
	var usage *fail.UsageError
	require.ErrorAs(t, err, &usage)
}
