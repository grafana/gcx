package checks_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/providers/synth/checks"
	"github.com/grafana/gcx/internal/resources"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Exercise the real file/command path and the resource adapter used by pull/push.
// Raw maps keep the assertions independent of the Check struct's JSON tags.
func TestChecksFolderUIDRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name  string
		field string
		want  any
	}{
		{name: "omitted"},
		{name: "assigned", field: "  folderUid: production-folder\n", want: "production-folder"},
		{name: "cleared", field: "  folderUid: \"\"\n", want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			check := stubCheckList[0]
			check.ID = 1234
			check.Probes = []int64{1}
			if value, ok := tc.want.(string); ok {
				check.FolderUID = &value
			}
			t.Run("commands", func(t *testing.T) {
				st := &checkAPIState{probesOnline: true}
				srv := newCheckServer(t, st)
				manifest := writeCheckManifest(t, t.TempDir())
				data, err := os.ReadFile(manifest)
				require.NoError(t, err)
				data = append(data, []byte(tc.field)...)
				require.NoError(t, os.WriteFile(manifest, data, 0o600))

				_, _, err = runChecks(t, srv.URL, false, "", "create", "-f", manifest)
				require.NoError(t, err)
				st.mu.Lock()
				created := st.checks[1234]
				st.mu.Unlock()
				assertCheckFolderJSON(t, created, tc.want)

				for _, args := range [][]string{{"get", "1234", "-o", "yaml"}, {"list", "-o", "json"}} {
					stdout, _, readErr := runChecks(t, srv.URL, false, "", args...)
					require.NoError(t, readErr)
					if args[0] == "get" {
						var obj unstructured.Unstructured
						require.NoError(t, format.NewYAMLCodec().Decode(strings.NewReader(stdout), &obj))
						assertFolderSpec(t, obj.Object, tc.want)
						require.NoError(t, os.WriteFile(manifest, []byte(stdout), 0o600))
					} else {
						var items []unstructured.Unstructured
						require.NoError(t, json.Unmarshal([]byte(stdout), &items))
						require.Len(t, items, 1)
						assertFolderSpec(t, items[0].Object, tc.want)
					}
				}
				_, _, err = runChecks(t, srv.URL, false, "", "update", "1234", "-f", manifest)
				require.NoError(t, err)
				st.mu.Lock()
				updated := st.lastUpdated
				st.mu.Unlock()
				assertCheckFolderJSON(t, updated, tc.want)
			})
			t.Run("resource adapter", func(t *testing.T) {
				st := &checkAPIState{probesOnline: true, checks: map[int64]checks.Check{1234: check}}
				srv := newCheckServer(t, st)

				a, err := checks.NewAdapterFactory(&fakeLoader{baseURL: srv.URL, token: "test-token", namespace: "default"})(context.Background())
				require.NoError(t, err)
				exported, err := a.Get(context.Background(), "web-check-1234", metav1.GetOptions{})
				require.NoError(t, err)
				assertFolderSpec(t, exported.Object, tc.want)
				_, err = a.Update(context.Background(), exported, metav1.UpdateOptions{})
				require.NoError(t, err)
				st.mu.Lock()
				updated := st.lastUpdated
				st.mu.Unlock()
				assertCheckFolderJSON(t, updated, tc.want)
			})
			t.Run("legacy conversion", func(t *testing.T) {
				// Cover the separate legacy resource conversion as well, including YAML.
				res, err := checks.ToResource(check, "default", map[int64]string{1: "Oregon"})
				require.NoError(t, err)
				var encoded bytes.Buffer
				require.NoError(t, format.NewYAMLCodec().Encode(&encoded, res.Object.Object))
				var obj unstructured.Unstructured
				require.NoError(t, format.NewYAMLCodec().Decode(&encoded, &obj))
				res, err = resources.FromUnstructured(&obj)
				require.NoError(t, err)
				spec, id, err := checks.FromResource(res)
				require.NoError(t, err)
				assertCheckFolderJSON(t, checks.SpecToCheck(spec, id, check.TenantID, check.Probes), tc.want)
			})
		})
	}
}

func assertCheckFolderJSON(t *testing.T, check checks.Check, want any) {
	t.Helper()
	data, err := json.Marshal(check)
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, json.Unmarshal(data, &body))
	got, present := body["folderUid"]
	assert.Equal(t, want != nil, present)
	assert.Equal(t, want, got)
}

func assertFolderSpec(t *testing.T, obj map[string]any, want any) {
	t.Helper()
	got, present, err := unstructured.NestedFieldNoCopy(obj, "spec", "folderUid")
	require.NoError(t, err)
	assert.Equal(t, want != nil, present)
	assert.Equal(t, want, got)
}

// The server already holds a folder assignment and the manifest omits
// folderUid. gcx must send no folderUid key so the backend preserves it; it
// must not backfill from the existing check or invent a default (the SM app's
// default-folder UID bug was a client inventing a folder identity).
func TestChecksFolderUIDOmittedDoesNotOverwriteExisting(t *testing.T) {
	existing := stubCheckList[0]
	existing.ID = 1234
	existing.Probes = []int64{1}
	folder := "existing-folder"
	existing.FolderUID = &folder

	t.Run("update command", func(t *testing.T) {
		st := &checkAPIState{probesOnline: true, checks: map[int64]checks.Check{1234: existing}}
		srv := newCheckServer(t, st)
		manifest := writeCheckManifest(t, t.TempDir())

		_, _, err := runChecks(t, srv.URL, false, "", "update", "1234", "-f", manifest)
		require.NoError(t, err)

		st.mu.Lock()
		updated := st.lastUpdated
		st.mu.Unlock()
		assertCheckFolderJSON(t, updated, nil)
	})

	t.Run("resource adapter", func(t *testing.T) {
		st := &checkAPIState{probesOnline: true, checks: map[int64]checks.Check{1234: existing}}
		srv := newCheckServer(t, st)

		a, err := checks.NewAdapterFactory(&fakeLoader{baseURL: srv.URL, token: "test-token", namespace: "default"})(context.Background())
		require.NoError(t, err)
		exported, err := a.Get(context.Background(), "web-check-1234", metav1.GetOptions{})
		require.NoError(t, err)
		assertFolderSpec(t, exported.Object, folder)

		// Simulate the user deleting the line from a pulled manifest.
		unstructured.RemoveNestedField(exported.Object, "spec", "folderUid")
		_, err = a.Update(context.Background(), exported, metav1.UpdateOptions{})
		require.NoError(t, err)

		st.mu.Lock()
		updated := st.lastUpdated
		st.mu.Unlock()
		assertCheckFolderJSON(t, updated, nil)
	})
}
