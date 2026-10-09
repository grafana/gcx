package resources_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	cmdresources "github.com/grafana/gcx/cmd/gcx/resources"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/assistant/watcher"
	"github.com/grafana/gcx/internal/assistant/watchers"
	"github.com/grafana/gcx/internal/assistant/watchers/watcherstest"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	_ "github.com/grafana/gcx/internal/providers/assistant" // Register the real Assistant resource adapter.
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func watcherFixture(id, title string) watchers.Watcher {
	return watchers.Watcher{ID: id, Name: title, Prompt: "Monitor telemetry", TriggerIntervalSeconds: 300, Sensitivity: "balanced"}
}

func watcherResourceCLI(t *testing.T, fixture *watcherstest.Server) func(...string) ([]byte, error) {
	t.Helper()
	t.Setenv("GCX_DISCOVERY_CACHE_DIR", t.TempDir())
	t.Setenv("GCX_KEYCHAIN", "off")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_DIRS", t.TempDir())
	for _, name := range []string{"GRAFANA_SERVER", "GRAFANA_TOKEN", "GRAFANA_USER", "GRAFANA_PASSWORD", "GRAFANA_PROXY_ENDPOINT"} {
		t.Setenv(name, "")
	}
	agent.SetFlag(true)
	t.Cleanup(func() { agent.SetFlag(false) })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(metav1.APIVersions{TypeMeta: metav1.TypeMeta{Kind: "APIVersions", APIVersion: "v1"}, Versions: []string{}})
		case "/apis":
			w.Header().Set("Content-Type", "application/json")
			version := metav1.GroupVersionForDiscovery{GroupVersion: watcher.WatcherAPIGroup + "/v0alpha1", Version: "v0alpha1"}
			_ = json.NewEncoder(w).Encode(metav1.APIGroupList{
				TypeMeta: metav1.TypeMeta{Kind: "APIGroupList", APIVersion: "v1"},
				Groups:   []metav1.APIGroup{{Name: watcher.WatcherAPIGroup, Versions: []metav1.GroupVersionForDiscovery{version}, PreferredVersion: version}},
			})
		case "/apis/" + watcher.WatcherAPIGroup + "/v0alpha1":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(metav1.APIResourceList{
				TypeMeta: metav1.TypeMeta{Kind: "APIResourceList", APIVersion: "v1"}, GroupVersion: watcher.WatcherAPIGroup + "/v0alpha1",
				APIResources: []metav1.APIResource{{Name: "pages", SingularName: "page", Kind: "Page", Namespaced: true}},
			})
		default:
			fixture.ServeHTTP(w, r)
		}
	}))
	t.Cleanup(server.Close)
	contents := fmt.Sprintf("version: 1\nstacks:\n  test:\n    grafana:\n      server: %s\n      token: fixture-token\n      org-id: 1\ncontexts:\n  test:\n    stack: test\ncurrent-context: test\n", server.URL)
	t.Setenv("GRAFANA_SERVER", server.URL)
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
	return func(args ...string) ([]byte, error) {
		root := &cobra.Command{Use: "gcx", SilenceErrors: true, SilenceUsage: true}
		root.AddCommand(cmdresources.Command())
		var stdout, stderr bytes.Buffer
		root.SetOut(&stdout)
		root.SetErr(&stderr)
		root.SetArgs(append([]string{"resources", "--config", path, "--context", "test"}, args...))
		err := root.ExecuteContext(t.Context())
		return stdout.Bytes(), err
	}
}

func watcherOutputPath(root, name string) string {
	desc := watcher.WatcherDescriptor()
	return filepath.Join(root, desc.Plural+"."+desc.GroupVersion.Version+"."+desc.GroupVersion.Group, name+".json")
}

func decodeWatcherReceipt(t *testing.T, output []byte) cmdio.ArtifactReceipt {
	t.Helper()
	var receipt cmdio.ArtifactReceipt
	dec := json.NewDecoder(bytes.NewReader(output))
	require.NoError(t, dec.Decode(&receipt), string(output))
	var extra any
	require.ErrorIs(t, dec.Decode(&extra), io.EOF, "stdout must contain exactly one result")
	return receipt
}

func requireWatcherPartialFailure(t *testing.T, err error) {
	t.Helper()
	var emitted *gcxerrors.EmittedError
	require.ErrorAs(t, err, &emitted)
	assert.Equal(t, gcxerrors.ExitPartialFailure, emitted.Code)
}

func TestWatcherGenericPullRejectsEveryCollisionBeforeWriting(t *testing.T) {
	fixture := &watcherstest.Server{
		Current: [][]watchers.Watcher{
			{watcherFixture("current-1", "Same title"), watcherFixture("cross-current", "Archive title"), watcherFixture("unique", "Unique")},
			{},
			{watcherFixture("current-2", "Same-title")},
		},
		Archived: [][]watchers.Watcher{{watcherFixture("cross-archived", "Archive-title")}},
		// Collision indexing must not request supplementary configuration for
		// an archived candidate that is outside the selected export partition.
		EnrollmentStatus: map[string]int{"cross-archived": http.StatusForbidden},
	}
	execute := watcherResourceCLI(t, fixture)
	dest := t.TempDir()
	for _, name := range []string{"same-title", "archive-title"} {
		path := watcherOutputPath(dest, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte("existing file"), 0o600))
	}
	output, err := execute("pull", "watchers", "--path", dest)
	requireWatcherPartialFailure(t, err)
	receipt := decodeWatcherReceipt(t, output)
	assert.Equal(t, 1, receipt.Summary.Succeeded)
	assert.Equal(t, 3, receipt.Summary.Failed)
	assert.Zero(t, receipt.Summary.Skipped)
	require.Len(t, receipt.Failures, 3)
	for _, failure := range receipt.Failures {
		assert.Equal(t, watcher.WatcherKind, failure.Target.Kind)
		assert.Contains(t, failure.Error, "ambiguous")
		if failure.Target.Name == "same-title" {
			assert.Contains(t, failure.Error, "current-1")
			assert.Contains(t, failure.Error, "current-2")
		} else {
			assert.Contains(t, failure.Error, "cross-current")
			assert.Contains(t, failure.Error, "cross-archived")
		}
	}
	for _, name := range []string{"same-title", "archive-title"} {
		contents, err := os.ReadFile(watcherOutputPath(dest, name))
		require.NoError(t, err)
		assert.Equal(t, "existing file", string(contents))
	}
	contents, err := os.ReadFile(watcherOutputPath(dest, "unique"))
	require.NoError(t, err)
	var manifest unstructured.Unstructured
	require.NoError(t, json.Unmarshal(contents, &manifest))
	assert.Equal(t, "unique", manifest.GetAnnotations()[watcher.WatcherIDAnnotation])
	assert.Zero(t, fixture.EnrollmentReads("cross-archived"))
	assert.Zero(t, fixture.MutationCalls())
}

func TestWatcherGenericPullOnErrorModesAndExplicitID(t *testing.T) {
	for _, mode := range []string{"ignore", "abort"} {
		t.Run(mode, func(t *testing.T) {
			fixture := &watcherstest.Server{Current: [][]watchers.Watcher{{watcherFixture("one", "Same title"), watcherFixture("two", "Same-title"), watcherFixture("unique", "Unique")}}}
			execute := watcherResourceCLI(t, fixture)
			dest := t.TempDir()
			output, err := execute("pull", "watcher", "--path", dest, "--on-error", mode)
			if mode == "abort" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "ambiguous")
				assert.Empty(t, output)
				_, err := os.Stat(watcherOutputPath(dest, "unique"))
				assert.ErrorIs(t, err, os.ErrNotExist)
			} else {
				require.NoError(t, err)
				receipt := decodeWatcherReceipt(t, output)
				assert.Equal(t, 1, receipt.Summary.Succeeded)
				assert.Equal(t, 2, receipt.Summary.Failed)
			}
		})
	}
	t.Run("explicit archived ID", func(t *testing.T) {
		fixture := &watcherstest.Server{
			Current:    [][]watchers.Watcher{{watcherFixture("current", "Same title")}},
			Archived:   [][]watchers.Watcher{{watcherFixture("archived", "Same-title")}},
			Enrollment: map[string]bool{"archived": true},
		}
		execute := watcherResourceCLI(t, fixture)
		dest := t.TempDir()
		output, err := execute("pull", "watchers/archived", "--path", dest)
		require.NoError(t, err)
		receipt := decodeWatcherReceipt(t, output)
		assert.Equal(t, 1, receipt.Summary.Succeeded)
		assert.Zero(t, receipt.Summary.Failed)
		contents, err := os.ReadFile(watcherOutputPath(dest, "same-title"))
		require.NoError(t, err)
		assert.Contains(t, string(contents), "archived")
	})
}

func TestWatcherGenericPullUnavailableIndexIsAnExplicitFailure(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusForbidden, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			fixture := &watcherstest.Server{Current: [][]watchers.Watcher{{watcherFixture("one", "Unique")}}, ArchivedStatus: status}
			execute := watcherResourceCLI(t, fixture)
			dest := t.TempDir()
			output, err := execute("pull", "watchers", "--path", dest)
			requireWatcherPartialFailure(t, err)
			receipt := decodeWatcherReceipt(t, output)
			assert.Zero(t, receipt.Summary.Succeeded)
			assert.Equal(t, 1, receipt.Summary.Failed)
			assert.Zero(t, receipt.Summary.Skipped)
			require.Len(t, receipt.Failures, 1)
			assert.Contains(t, receipt.Failures[0].Error, "enumerate Watcher identities")
			_, err = os.Stat(watcherOutputPath(dest, "unique"))
			assert.ErrorIs(t, err, os.ErrNotExist)
		})
	}
	t.Run("collection unavailable is not skipped", func(t *testing.T) {
		fixture := &watcherstest.Server{CurrentStatus: http.StatusNotFound}
		execute := watcherResourceCLI(t, fixture)
		output, err := execute("pull", "watchers", "--path", t.TempDir())
		requireWatcherPartialFailure(t, err)
		receipt := decodeWatcherReceipt(t, output)
		assert.Equal(t, 1, receipt.Summary.Failed)
		assert.Zero(t, receipt.Summary.Skipped)
		assert.Contains(t, receipt.Failures[0].Error, "unavailable")
	})
}

func TestWatcherGenericPullMultipleIDsPreserveUniqueFiles(t *testing.T) {
	for _, selectors := range [][]string{
		{"watchers/archived,unique"},
		{"watchers/current,archived"},
		{"watcher/current", "watcher/archived"},
	} {
		t.Run(strings.Join(selectors, "/"), func(t *testing.T) {
			fixture := &watcherstest.Server{
				Current:  [][]watchers.Watcher{{watcherFixture("current", "Same title"), watcherFixture("unique", "Unique")}},
				Archived: [][]watchers.Watcher{{watcherFixture("archived", "Same-title")}},
			}
			execute := watcherResourceCLI(t, fixture)
			dest := t.TempDir()
			args := append([]string{"pull", "--path", dest}, selectors...)
			output, err := execute(args...)
			if selectors[0] == "watchers/archived,unique" {
				require.NoError(t, err)
				receipt := decodeWatcherReceipt(t, output)
				assert.Equal(t, 2, receipt.Summary.Succeeded)
				assert.Zero(t, receipt.Summary.Failed)
				return
			}
			requireWatcherPartialFailure(t, err)
			receipt := decodeWatcherReceipt(t, output)
			assert.Zero(t, receipt.Summary.Succeeded)
			assert.Equal(t, 2, receipt.Summary.Failed)
			_, err = os.Stat(watcherOutputPath(dest, "same-title"))
			assert.ErrorIs(t, err, os.ErrNotExist)
		})
	}
}

func TestWatcherGenericPullMixedReferencesKeepUnaffectedSelections(t *testing.T) {
	for _, reference := range []string{"same-title", "missing", "same-title,missing"} {
		for _, mode := range []string{"fail", "ignore", "abort"} {
			t.Run(reference+"/"+mode, func(t *testing.T) {
				fixture := &watcherstest.Server{
					Current: [][]watchers.Watcher{{watcherFixture("one", "Same title"), watcherFixture("two", "Same-title"), watcherFixture("unique-id", "Unique")}},
				}
				execute := watcherResourceCLI(t, fixture)
				dest := t.TempDir()
				collisionPath := watcherOutputPath(dest, "same-title")
				require.NoError(t, os.MkdirAll(filepath.Dir(collisionPath), 0o755))
				require.NoError(t, os.WriteFile(collisionPath, []byte("existing file"), 0o600))
				output, err := execute("pull", "watchers/unique,"+reference, "--path", dest, "--on-error", mode)
				if mode == "abort" {
					require.Error(t, err)
					assert.Empty(t, output)
					_, err = os.Stat(watcherOutputPath(dest, "unique"))
					assert.ErrorIs(t, err, os.ErrNotExist)
					return
				}
				if mode == "fail" {
					requireWatcherPartialFailure(t, err)
				} else {
					require.NoError(t, err)
				}
				receipt := decodeWatcherReceipt(t, output)
				assert.Equal(t, 1, receipt.Summary.Succeeded)
				assert.Equal(t, strings.Count(reference, ",")+1, receipt.Summary.Failed)
				assert.Zero(t, receipt.Summary.Skipped)
				contents, err := os.ReadFile(watcherOutputPath(dest, "unique"))
				require.NoError(t, err)
				assert.Contains(t, string(contents), "unique-id")
				contents, err = os.ReadFile(collisionPath)
				require.NoError(t, err)
				assert.Equal(t, "existing file", string(contents))
				for _, failure := range receipt.Failures {
					assert.Contains(t, failure.Error, "pull Watcher reference")
					if strings.Contains(failure.Error, "same-title") {
						assert.Contains(t, failure.Error, "one")
						assert.Contains(t, failure.Error, "two")
					}
				}
				assert.Zero(t, fixture.MutationCalls())
			})
		}
	}
}

func TestWatcherGenericGetCollisionIsAnExplicitPartialFailure(t *testing.T) {
	fixture := &watcherstest.Server{Current: [][]watchers.Watcher{{watcherFixture("one", "Same title"), watcherFixture("two", "Same-title"), watcherFixture("unique", "Unique")}}}
	execute := watcherResourceCLI(t, fixture)
	output, err := execute("get", "watchers", "--output", "json")
	requireWatcherPartialFailure(t, err)
	var result struct {
		Type  string                      `json:"type"`
		Items []unstructured.Unstructured `json:"items"`
		Error struct {
			Summary  string `json:"summary"`
			ExitCode int    `json:"exitCode"`
		} `json:"error"`
	}
	dec := json.NewDecoder(bytes.NewReader(output))
	require.NoError(t, dec.Decode(&result), string(output))
	assert.Equal(t, "gcx.partial_result", result.Type)
	require.Len(t, result.Items, 1)
	assert.Equal(t, "unique", result.Items[0].GetName())
	assert.Contains(t, result.Error.Summary, "2 resource(s) failed")
	assert.Equal(t, gcxerrors.ExitPartialFailure, result.Error.ExitCode)
	var extra any
	require.ErrorIs(t, dec.Decode(&extra), io.EOF)
}

func TestWatcherGenericBulkPullRetainsUnaffectedReads(t *testing.T) {
	for _, policy := range []string{"fail", "ignore", "abort"} {
		t.Run(policy, func(t *testing.T) {
			fixture := &watcherstest.Server{
				Current:          [][]watchers.Watcher{{watcherFixture("good", "Good"), watcherFixture("gone", "Gone"), watcherFixture("denied", "Denied"), watcherFixture("unavailable", "Unavailable")}},
				DetailStatus:     map[string]int{"gone": http.StatusNotFound},
				EnrollmentStatus: map[string]int{"denied": http.StatusForbidden, "unavailable": http.StatusNotFound},
			}
			execute := watcherResourceCLI(t, fixture)
			dest := t.TempDir()
			for _, name := range []string{"denied", "unavailable", "gone"} {
				path := watcherOutputPath(dest, name)
				require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
				require.NoError(t, os.WriteFile(path, []byte("existing file"), 0o600))
			}
			output, err := execute("pull", "watchers", "--path", dest, "--on-error", policy)
			if policy == "abort" {
				require.Error(t, err)
				assert.Empty(t, output)
				_, statErr := os.Stat(watcherOutputPath(dest, "good"))
				require.ErrorIs(t, statErr, os.ErrNotExist)
			} else {
				if policy == "ignore" {
					require.NoError(t, err)
				} else {
					requireWatcherPartialFailure(t, err)
				}
				receipt := decodeWatcherReceipt(t, output)
				assert.Equal(t, 1, receipt.Summary.Succeeded)
				assert.Equal(t, 2, receipt.Summary.Failed)
				assert.Equal(t, 1, receipt.Summary.Skipped)
				require.Len(t, receipt.Failures, 2)
				assert.Equal(t, "Watcher", receipt.Failures[0].Target.Kind)
				assert.Equal(t, "denied", receipt.Failures[0].Target.Name)
				assert.Contains(t, receipt.Failures[0].Error, "denied")
				assert.Equal(t, "unavailable", receipt.Failures[1].Target.Name)
				_, statErr := os.Stat(watcherOutputPath(dest, "good"))
				require.NoError(t, statErr)
			}
			for _, name := range []string{"denied", "unavailable", "gone"} {
				data, readErr := os.ReadFile(watcherOutputPath(dest, name))
				require.NoError(t, readErr)
				assert.Equal(t, "existing file", string(data))
			}
			assert.Zero(t, fixture.MutationCalls())
		})
	}
}

func TestWatcherGenericGetRetainsUnaffectedReads(t *testing.T) {
	for _, policy := range []string{"fail", "ignore"} {
		t.Run(policy, func(t *testing.T) {
			fixture := &watcherstest.Server{
				Current:          [][]watchers.Watcher{{watcherFixture("good", "Good"), watcherFixture("denied", "Denied")}},
				EnrollmentStatus: map[string]int{"denied": http.StatusForbidden},
			}
			execute := watcherResourceCLI(t, fixture)
			output, err := execute("get", "watchers", "--output", "json", "--on-error", policy)
			if policy == "ignore" {
				require.NoError(t, err)
			} else {
				requireWatcherPartialFailure(t, err)
			}
			var result struct {
				Type  string                      `json:"type"`
				Items []unstructured.Unstructured `json:"items"`
				Error struct {
					Summary  string `json:"summary"`
					ExitCode int    `json:"exitCode"`
				} `json:"error"`
			}
			dec := json.NewDecoder(bytes.NewReader(output))
			require.NoError(t, dec.Decode(&result), string(output))
			require.Len(t, result.Items, 1)
			assert.Equal(t, "good", result.Items[0].GetName())
			if policy == "fail" {
				assert.Equal(t, "gcx.partial_result", result.Type)
				assert.Contains(t, result.Error.Summary, "1 resource(s) failed")
				assert.Equal(t, gcxerrors.ExitPartialFailure, result.Error.ExitCode)
			} else {
				assert.Empty(t, result.Type)
			}
			var extra any
			require.ErrorIs(t, dec.Decode(&extra), io.EOF)
		})
	}
}

func TestWatcherGenericGetByNameAndIDMatchesPulledManifest(t *testing.T) {
	fixture := &watcherstest.Server{Current: [][]watchers.Watcher{{watcherFixture("server-id", "Unique title")}}, Enrollment: map[string]bool{"server-id": true}}
	execute := watcherResourceCLI(t, fixture)
	var byName, byID unstructured.Unstructured
	for _, selector := range []string{
		"watcher/unique-title", "watchers/server-id",
		"watchers." + watcher.WatcherAPIGroup + "/server-id",
		"watchers." + watcher.WatcherVersion + "." + watcher.WatcherAPIGroup + "/server-id",
	} {
		output, err := execute("get", selector, "--output", "json")
		require.NoError(t, err)
		var object unstructured.Unstructured
		require.NoError(t, json.Unmarshal(output, &object))
		assert.Equal(t, watcher.WatcherAPIVersion, object.GetAPIVersion())
		assert.Equal(t, "unique-title", object.GetName())
		assert.Equal(t, "server-id", object.GetAnnotations()[watcher.WatcherIDAnnotation])
		if strings.HasSuffix(selector, "server-id") {
			byID = object
		} else {
			byName = object
		}
	}
	assert.Equal(t, byName.Object, byID.Object)
	dest := t.TempDir()
	_, err := execute("pull", "watcher/server-id", "--path", dest)
	require.NoError(t, err)
	contents, err := os.ReadFile(watcherOutputPath(dest, "unique-title"))
	require.NoError(t, err)
	var pulled unstructured.Unstructured
	require.NoError(t, json.Unmarshal(contents, &pulled))
	assert.Equal(t, byID.Object["spec"], pulled.Object["spec"])
	assert.Equal(t, byID.GetName(), pulled.GetName())
	assert.Equal(t, byID.GetAnnotations(), pulled.GetAnnotations())
}

func TestWatcherGenericPushAndDeleteAreUnsupportedWithoutMutationRequests(t *testing.T) {
	for _, operation := range []string{"push", "delete"} {
		for _, dryRun := range []bool{false, true} {
			t.Run(operation+"/dryRun="+strconv.FormatBool(dryRun), func(t *testing.T) {
				fixture := &watcherstest.Server{Current: [][]watchers.Watcher{{watcherFixture("server-id", "Unique")}}}
				execute := watcherResourceCLI(t, fixture)
				dest := t.TempDir()
				_, err := execute("pull", "watcher/server-id", "--path", dest)
				require.NoError(t, err)
				args := []string{operation, "--path", dest, "--output", "json"}
				if operation == "delete" {
					args = append(args, "--yes")
				}
				if dryRun {
					args = append(args, "--dry-run")
				}
				output, err := execute(args...)
				requireWatcherPartialFailure(t, err)
				var result cmdio.BatchMutation
				require.NoError(t, json.Unmarshal(output, &result), string(output))
				assert.Zero(t, result.Summary.Succeeded)
				assert.Equal(t, 1, result.Summary.Failed)
				assert.Zero(t, result.Summary.Skipped)
				require.Len(t, result.Failures, 1)
				assert.Contains(t, result.Failures[0].Error, "Watcher "+operation+" is not supported yet")
				assert.Contains(t, result.Failures[0].Error, "Grafana")
				assert.Zero(t, fixture.MutationCalls())
			})
		}
	}
}

func TestWatcherGenericDeleteSelectorsRefuseBeforeConfigurationReads(t *testing.T) {
	for _, fixture := range []*watcherstest.Server{
		{},
		{CurrentStatus: http.StatusForbidden},
		{Current: [][]watchers.Watcher{{watcherFixture("one", "Same title"), watcherFixture("two", "Same-title")}}},
		{Current: [][]watchers.Watcher{{watcherFixture("one", "Unique")}}, EnrollmentStatus: map[string]int{"one": http.StatusForbidden}},
	} {
		execute := watcherResourceCLI(t, fixture)
		for _, selector := range []string{"watchers", "watcher/one", "watchers." + watcher.WatcherVersion + "." + watcher.WatcherAPIGroup + "/one"} {
			for _, dryRun := range []bool{false, true} {
				args := []string{"delete", selector, "--yes", "--output", "json"}
				if dryRun {
					args = append(args, "--dry-run")
				}
				_, err := execute(args...)
				require.ErrorIs(t, err, errors.ErrUnsupported)
				assert.Contains(t, err.Error(), "Watcher delete is not supported yet")
				assert.Zero(t, fixture.CollectionReads(false))
				assert.Zero(t, fixture.CollectionReads(true))
				assert.Zero(t, fixture.EnrollmentReads("one"))
				assert.Zero(t, fixture.MutationCalls())
			}
		}
	}
}
