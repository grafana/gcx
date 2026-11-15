package resources_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/cmd/gcx/resources"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/resources/observation"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const fullPermissions = `{"folders:read":["folders:*"],"dashboards:read":["dashboards:*"],"datasources:read":["datasources:*"],"dashboards.permissions:read":["dashboards:*"],"folders.permissions:read":["folders:*"]}`

// defaultACL is the permission list the fake serves unless acl overrides it.
const defaultACL = `[{"userLogin":"sa-admin","permissionName":"Admin"},{"role":"Editor","permissionName":"Edit"},{"role":"Viewer","permissionName":"View"}]`

// fakeGrafana serves API discovery, the caller endpoints and the folder and
// dashboard lists. Datasources are not served: their adapter is registered
// by a provider package that this test binary does not import, so the
// datasources type is unavailable here. The package tests cover it.
type fakeGrafana struct {
	userStatus  int
	permissions string
	folders     []string
	dashboards  []string
	// trash is served for the dashboards list with the trash label selector.
	trash []string
	// acl overrides the permission list served for a dashboard or folder uid.
	acl map[string]string

	mu         sync.Mutex
	unexpected []string
}

func (f *fakeGrafana) serve(t *testing.T) *httptest.Server {
	t.Helper()
	group := func(name string) string {
		return fmt.Sprintf(`{"name":%q,"versions":[{"groupVersion":"%s/v1beta1","version":"v1beta1"}],"preferredVersion":{"groupVersion":"%s/v1beta1","version":"v1beta1"}}`, name, name, name)
	}
	resourceList := func(gv, plural, singular, kind string) string {
		return fmt.Sprintf(`{"kind":"APIResourceList","groupVersion":%q,"resources":[{"name":%q,"singularName":%q,"namespaced":true,"kind":%q,"verbs":["get","list"]}]}`, gv, plural, singular, kind)
	}
	list := func(kind string, items []string) string {
		return fmt.Sprintf(`{"kind":%q,"apiVersion":"v1","metadata":{"resourceVersion":"1"},"items":[%s]}`, kind, strings.Join(items, ","))
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		aclPath := (strings.HasPrefix(r.URL.Path, "/api/dashboards/uid/") || strings.HasPrefix(r.URL.Path, "/api/folders/")) &&
			strings.HasSuffix(r.URL.Path, "/permissions")
		if aclPath {
			uid := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/api/dashboards/uid/"), "/api/folders/"), "/permissions")
			if acl, set := f.acl[uid]; set {
				_, _ = fmt.Fprint(w, acl)
				return
			}
			_, _ = fmt.Fprint(w, defaultACL)
			return
		}
		switch r.URL.Path {
		case "/bootdata":
			// Config loading probes it; a plain Grafana server may not serve it.
			http.NotFound(w, r)
		case "/api":
			_, _ = fmt.Fprint(w, `{"kind":"APIVersions","versions":[]}`)
		case "/apis":
			_, _ = fmt.Fprintf(w, `{"kind":"APIGroupList","groups":[%s,%s]}`, group("folder.grafana.app"), group("dashboard.grafana.app"))
		case "/apis/folder.grafana.app/v1beta1":
			_, _ = fmt.Fprint(w, resourceList("folder.grafana.app/v1beta1", "folders", "folder", "Folder"))
		case "/apis/dashboard.grafana.app/v1beta1":
			_, _ = fmt.Fprint(w, resourceList("dashboard.grafana.app/v1beta1", "dashboards", "dashboard", "Dashboard"))
		case "/apis/folder.grafana.app/v1beta1/namespaces/default/folders":
			_, _ = fmt.Fprint(w, list("FolderList", f.folders))
		case "/apis/dashboard.grafana.app/v1beta1/namespaces/default/dashboards":
			if r.URL.Query().Get("labelSelector") == "grafana.app/get-trash=true" {
				_, _ = fmt.Fprint(w, list("DashboardList", f.trash))
				return
			}
			_, _ = fmt.Fprint(w, list("DashboardList", f.dashboards))
		case "/api/user":
			if f.userStatus != 0 && f.userStatus != http.StatusOK {
				w.WriteHeader(f.userStatus)
				_, _ = fmt.Fprint(w, `{"message":"invalid API key"}`)
				return
			}
			_, _ = fmt.Fprint(w, `{"uid":"service-account:1","login":"sa-admin"}`)
		case "/api/access-control/user/permissions":
			_, _ = fmt.Fprint(w, f.permissions)
		default:
			f.mu.Lock()
			f.unexpected = append(f.unexpected, r.Method+" "+r.URL.Path)
			f.mu.Unlock()
			w.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprint(w, `{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"NotFound","code":404}`)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func folderJSON(name, title string) string {
	return fmt.Sprintf(`{"apiVersion":"folder.grafana.app/v1beta1","kind":"Folder","metadata":{"name":%q,"namespace":"default","resourceVersion":"1"},"spec":{"title":%q}}`, name, title)
}

func dashboardJSON(name, title string) string {
	return fmt.Sprintf(`{"apiVersion":"dashboard.grafana.app/v1beta1","kind":"Dashboard","metadata":{"name":%q,"namespace":"default","resourceVersion":"1","annotations":{"grafana.app/folder":"f1"}},"spec":{"title":%q,"version":3}}`, name, title)
}

// captureEnv isolates config discovery, points a config with the context
// "selected" at the fake server, and returns the config path. The working
// directory is an empty temporary directory.
func captureEnv(t *testing.T, fake *fakeGrafana) string {
	t.Helper()
	testutils.SandboxConfigEnv(t)
	t.Setenv("GCX_DISCOVERY_CACHE_DIR", t.TempDir())
	srv := fake.serve(t)
	t.Cleanup(func() {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		assert.Empty(t, fake.unexpected, "unexpected requests")
	})
	cfg := filepath.Join(t.TempDir(), "config.yaml")
	content := fmt.Sprintf("version: 1\ncredentials:\n  keychain: off\nstacks:\n  local:\n    grafana:\n      server: %s\n      org-id: 1\ncontexts:\n  selected:\n    stack: local\ncurrent-context: selected\n", srv.URL)
	require.NoError(t, os.WriteFile(cfg, []byte(content), 0o600))
	return cfg
}

func runResources(t *testing.T, cfg string, args ...string) (string, string, error) {
	t.Helper()
	root := &cobra.Command{Use: "gcx", SilenceErrors: true, SilenceUsage: true}
	root.AddCommand(resources.Command())
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(append([]string{"resources", "--config", cfg}, args...))
	err := root.ExecuteContext(t.Context())
	return stdout.String(), stderr.String(), err
}

func exitCode(t *testing.T, err error) int {
	t.Helper()
	require.Error(t, err)
	if emitted, ok := err.(*gcxerrors.EmittedError); ok { //nolint:errorlint // the command returns it unwrapped.
		return emitted.Code
	}
	detailed := fail.ErrorToDetailedError(err)
	require.NotNil(t, detailed)
	require.NotNil(t, detailed.ExitCode, "error without an exit code: %v", err)
	return *detailed.ExitCode
}

func defaultFake() *fakeGrafana {
	return &fakeGrafana{
		permissions: fullPermissions,
		folders:     []string{folderJSON("f1", "F1")},
		dashboards:  []string{dashboardJSON("d1", "D1"), dashboardJSON("d2", "D2")},
	}
}

type captureReceipt struct {
	Action string `json:"action"`
	Dir    string `json:"dir"`
	Files  []struct {
		Path  string `json:"path"`
		Kind  string `json:"kind"`
		Count int    `json:"count"`
	} `json:"files"`
	Summary struct {
		Succeeded int `json:"succeeded"`
		Failed    int `json:"failed"`
	} `json:"summary"`
	Failures []struct {
		Target struct {
			Kind string `json:"kind"`
		} `json:"target"`
		Error string `json:"error"`
	} `json:"failures"`
}

func TestCaptureCmd_Receipt(t *testing.T) {
	cfg := captureEnv(t, defaultFake())
	testutils.SetAgentMode(t, true)

	stdout, _, err := runResources(t, cfg, "capture", "dashboards", "folders", "-p", "out")
	require.NoError(t, err)

	var receipt captureReceipt
	require.NoError(t, json.Unmarshal([]byte(stdout), &receipt))
	assert.Equal(t, "captured", receipt.Action)
	assert.Equal(t, "out", receipt.Dir)
	// The summary counts resource types and their trash and permission views;
	// files count resources.
	assert.Equal(t, 5, receipt.Summary.Succeeded)
	assert.Equal(t, 0, receipt.Summary.Failed)
	assert.Empty(t, receipt.Failures)
	counts := map[string]int{}
	for _, f := range receipt.Files {
		counts[f.Path] = f.Count
	}
	assert.Equal(t, map[string]int{
		filepath.Join("out", observation.ManifestFile):             0,
		filepath.Join("out", observation.TypeDashboards):           2,
		filepath.Join("out", observation.TypeFolders):              1,
		filepath.Join("out", observation.TypeDashboardPermissions): 2,
		filepath.Join("out", observation.TypeFolderPermissions):    1,
	}, counts)

	capture, err := observation.Load("out")
	require.NoError(t, err)
	m := capture.Manifest
	assert.Equal(t, "selected", m.Stack.Context)
	assert.Equal(t, "default", m.Stack.Namespace)
	require.NotNil(t, m.Caller)
	assert.Equal(t, observation.CallerIdentity{UID: "service-account:1", Login: "sa-admin"}, *m.Caller)
	assert.Equal(t, []string{"dashboards", "folders"}, m.Selectors)
	// Dashboards bring the trash and permission views, folders a permission view.
	require.Len(t, m.Types, 5)
	for _, cov := range m.Types {
		assert.Equal(t, observation.CoverageComplete, cov.Status, cov.Type)
		assert.Equal(t, observation.ReadScopeAll, cov.ReadScope, cov.Type)
	}
	// The dashboard save counter is normalized away.
	spec, ok := capture.Objects[observation.TypeDashboards]["d1"]["spec"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, map[string]any{"title": "D1"}, spec)
}

func TestCaptureCmd_TextReceipt(t *testing.T) {
	cfg := captureEnv(t, defaultFake())

	stdout, stderr, err := runResources(t, cfg, "capture", "dashboards", "folders", "-p", "out")
	require.NoError(t, err)
	assert.Contains(t, stdout, "6 resources captured across 5 types to out")
	assert.Empty(t, stderr)
}

func TestCaptureCmd_Incomplete(t *testing.T) {
	t.Run("restricted read scope", func(t *testing.T) {
		fake := defaultFake()
		fake.permissions = `{"folders:read":["folders:*"],"dashboards:read":["dashboards:uid:d1"]}`
		cfg := captureEnv(t, fake)

		stdout, stderr, err := runResources(t, cfg, "capture", "dashboards", "folders", "-p", "out")
		assert.Equal(t, gcxerrors.ExitPartialFailure, exitCode(t, err))
		// The trash is read with the dashboards permission, so it is incomplete too.
		assert.Contains(t, stderr, "warn: 4 of 5 resource type(s) captured incompletely")
		assert.Contains(t, stdout, observation.TypeDashboards+" is incomplete")
		assert.Contains(t, stdout, "dashboards:read is not granted")

		// The capture is still written.
		capture, err := observation.Load("out")
		require.NoError(t, err)
		// Types are sorted: dashboards, folders, the two permission views, trash.
		require.Len(t, capture.Manifest.Types, 5)
		assert.Equal(t, observation.ReadScopeRestricted, capture.Manifest.Types[0].ReadScope)
		assert.Equal(t, observation.CoverageComplete, capture.Manifest.Types[1].Status)
		for _, cov := range capture.Manifest.Types[2:] {
			assert.Equal(t, observation.CoverageIncomplete, cov.Status, cov.Type)
		}
	})

	t.Run("receipt counts the incomplete type as failed", func(t *testing.T) {
		cfg := captureEnv(t, defaultFake())
		testutils.SetAgentMode(t, true)

		// datasources is supported but not served by the fake stack.
		stdout, _, err := runResources(t, cfg, "capture", "folders", "datasources", "-p", "out")
		assert.Equal(t, gcxerrors.ExitPartialFailure, exitCode(t, err))

		var receipt captureReceipt
		require.NoError(t, json.NewDecoder(strings.NewReader(stdout)).Decode(&receipt))
		// folders and its permission view succeed.
		assert.Equal(t, 2, receipt.Summary.Succeeded)
		assert.Equal(t, 1, receipt.Summary.Failed)
		require.Len(t, receipt.Failures, 1)
		assert.Equal(t, observation.TypeDatasources, receipt.Failures[0].Target.Kind)
		assert.True(t, strings.HasPrefix(receipt.Failures[0].Error, observation.CoverageUnavailable+": "), receipt.Failures[0].Error)
	})
}

func TestCaptureCmd_RejectedBeforeWriting(t *testing.T) {
	tests := []struct {
		name     string
		user     int
		args     []string
		wantCode int
		wantErr  string
	}{
		{"unknown type", 0, []string{"bogus"}, gcxerrors.ExitUsageError, "unknown resource type"},
		{"named selector", 0, []string{"dashboards/d1"}, gcxerrors.ExitUsageError, "remove the resource names"},
		{"bad credentials", http.StatusUnauthorized, []string{"dashboards"}, gcxerrors.ExitAuthFailure, "status 401"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fake := defaultFake()
			fake.userStatus = tc.user
			cfg := captureEnv(t, fake)

			stdout, _, err := runResources(t, cfg, append([]string{"capture", "-p", "out"}, tc.args...)...)
			assert.Equal(t, tc.wantCode, exitCode(t, err))
			require.ErrorContains(t, err, tc.wantErr)
			assert.Empty(t, stdout)
			_, statErr := os.Stat("out")
			require.ErrorIs(t, statErr, os.ErrNotExist)
			entries, err := os.ReadDir(".")
			require.NoError(t, err)
			assert.Empty(t, entries, "nothing is written next to the destination")
		})
	}
}

// A before capture with full access and an after capture by the same caller
// with a restricted scope: the text table lists the partial type and the
// reason for the indeterminate dashboard.
func TestDiffCmd_TextShowsIncompleteTypes(t *testing.T) {
	fake := defaultFake()
	cfg := captureEnv(t, fake)
	_, _, err := runResources(t, cfg, "capture", "dashboards", "folders", "-p", "before")
	require.NoError(t, err)

	fake.permissions = `{"folders:read":["folders:*"],"folders.permissions:read":["folders:*"],"dashboards:read":["dashboards:uid:d1"]}`
	fake.dashboards = fake.dashboards[:1]
	fake.folders = append(fake.folders, folderJSON("f2", "F2"))
	_, _, err = runResources(t, cfg, "capture", "dashboards", "folders", "-p", "after")
	require.Equal(t, gcxerrors.ExitPartialFailure, exitCode(t, err))

	stdout, _, err := runResources(t, cfg, "diff", "before", "after", "-o", "text")
	require.NoError(t, err)
	// Folders and their permission view are compared; dashboards, their
	// permission view and the trash are partial.
	assert.Contains(t, stdout, "2 added, 0 modified, 0 deleted, 4 unchanged, 2 indeterminate")
	assert.Contains(t, stdout, "The comparison is incomplete")

	tables := strings.SplitN(stdout, "\n\n", 3)
	require.Len(t, tables, 3, stdout)
	gaps, changes := tables[1], tables[2]
	assert.Contains(t, gaps, "REASON")
	assert.Contains(t, gaps, observation.TypeDashboards)
	assert.Contains(t, gaps, observation.TypePartial)
	assert.Contains(t, gaps, "after capture is incomplete")
	assert.NotContains(t, gaps, "\n"+observation.TypeFolders+" ", "a compared type is not a gap")
	assert.NotContains(t, gaps, observation.TypeFolderPermissions, "a compared view is not a gap")

	assert.Contains(t, changes, "DETAIL")
	for line := range strings.SplitSeq(changes, "\n") {
		switch {
		case strings.Contains(line, " d2 "):
			assert.Contains(t, line, observation.ChangeIndeterminate)
			assert.Contains(t, line, "deletion cannot be established")
		case strings.Contains(line, " f2 "):
			assert.Contains(t, line, observation.ChangeAdded)
		}
	}
	assert.Contains(t, changes, " d2 ")
	assert.Contains(t, changes, " f2 ")
}

func trashChanges(t *testing.T, stdout string) map[string]string {
	t.Helper()
	var res struct {
		Changes []struct {
			Type   string `json:"type"`
			Name   string `json:"name"`
			Change string `json:"change"`
		} `json:"changes"`
		Complete bool `json:"complete"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &res), stdout)
	require.True(t, res.Complete, stdout)
	out := map[string]string{}
	for _, c := range res.Changes {
		out[c.Type+"/"+c.Name] = c.Change
	}
	return out
}

// A dashboard created and deleted between two captures leaves no change in
// the dashboards type, but it does leave an entry in the trash view.
func TestDiffCmd_TrashShowsCreateAndDeleteWithinWindow(t *testing.T) {
	fake := defaultFake()
	cfg := captureEnv(t, fake)
	_, _, err := runResources(t, cfg, "capture", "dashboards", "-p", "before")
	require.NoError(t, err)

	fake.trash = []string{dashboardJSON("d3", "D3")}
	_, _, err = runResources(t, cfg, "capture", "dashboards", "-p", "after")
	require.NoError(t, err)

	stdout, _, err := runResources(t, cfg, "diff", "before", "after", "-o", "json")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{observation.TypeDashboardTrash + "/d3": observation.ChangeAdded}, trashChanges(t, stdout))
}

// Creating a dashboard with the name of a trashed one removes the trash entry.
func TestDiffCmd_TrashShowsConsumedName(t *testing.T) {
	fake := defaultFake()
	fake.trash = []string{dashboardJSON("d3", "D3 old")}
	cfg := captureEnv(t, fake)
	_, _, err := runResources(t, cfg, "capture", "dashboards", "-p", "before")
	require.NoError(t, err)

	fake.trash = nil
	fake.dashboards = append(fake.dashboards, dashboardJSON("d3", "D3 new"))
	_, _, err = runResources(t, cfg, "capture", "dashboards", "-p", "after")
	require.NoError(t, err)

	stdout, _, err := runResources(t, cfg, "diff", "before", "after", "-o", "json")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		observation.TypeDashboards + "/d3":           observation.ChangeAdded,
		observation.TypeDashboardPermissions + "/d3": observation.ChangeAdded,
		observation.TypeDashboardTrash + "/d3":       observation.ChangeDeleted,
	}, trashChanges(t, stdout))
}

// Narrowing a dashboard's permissions is a change in the permission view,
// while the dashboard itself is unchanged.
func TestDiffCmd_PermissionsShowNarrowedACL(t *testing.T) {
	fake := defaultFake()
	cfg := captureEnv(t, fake)
	_, _, err := runResources(t, cfg, "capture", "dashboards", "-p", "before")
	require.NoError(t, err)

	fake.acl = map[string]string{"d1": `[{"userLogin":"sa-admin","permissionName":"Admin"}]`}
	_, _, err = runResources(t, cfg, "capture", "dashboards", "-p", "after")
	require.NoError(t, err)

	stdout, _, err := runResources(t, cfg, "diff", "before", "after", "-o", "json")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{observation.TypeDashboardPermissions + "/d1": observation.ChangeModified}, trashChanges(t, stdout))
}
