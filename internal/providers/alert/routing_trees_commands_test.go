package alert_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/providers/alert"
	"github.com/grafana/gcx/internal/providers/native"
	"github.com/grafana/gcx/internal/resources"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/yaml"
)

const rtGroup = "notifications.alerting.grafana.app"

var rtGroupResource = schema.GroupResource{Group: rtGroup, Resource: "routingtrees"} //nolint:gochecknoglobals // Test fixture.

func rtDescriptor(version string) resources.Descriptor {
	return resources.Descriptor{
		GroupVersion: schema.GroupVersion{Group: rtGroup, Version: version},
		Kind:         "RoutingTree",
		Singular:     "routingtree",
		Plural:       "routingtrees",
	}
}

func rtObject(name string, spec map[string]any, annotations map[string]string) unstructured.Unstructured {
	obj := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": rtGroup + "/v1beta1",
		"kind":       "RoutingTree",
		"metadata": map[string]any{
			"name":            name,
			"namespace":       "default",
			"resourceVersion": "42",
		},
	}}
	if spec != nil {
		obj.Object["spec"] = spec
	}
	if annotations != nil {
		obj.SetAnnotations(annotations)
	}
	return obj
}

// fakeRoutingTreeClient implements native.Client and records every call.
type fakeRoutingTreeClient struct {
	items map[string]unstructured.Unstructured
	err   error

	calls   []string
	lastObj *unstructured.Unstructured
}

func (f *fakeRoutingTreeClient) record(call string) { f.calls = append(f.calls, call) }

func (f *fakeRoutingTreeClient) Create(_ context.Context, _ resources.Descriptor, obj *unstructured.Unstructured, _ metav1.CreateOptions) (*unstructured.Unstructured, error) {
	f.record("create " + obj.GetName())
	f.lastObj = obj
	if f.err != nil {
		return nil, f.err
	}
	return obj, nil
}

func (f *fakeRoutingTreeClient) Update(_ context.Context, _ resources.Descriptor, obj *unstructured.Unstructured, _ metav1.UpdateOptions) (*unstructured.Unstructured, error) {
	f.record("update " + obj.GetName())
	f.lastObj = obj
	if f.err != nil {
		return nil, f.err
	}
	return obj, nil
}

func (f *fakeRoutingTreeClient) Get(_ context.Context, _ resources.Descriptor, name string, _ metav1.GetOptions) (*unstructured.Unstructured, error) {
	f.record("get " + name)
	if f.err != nil {
		return nil, f.err
	}
	item, ok := f.items[name]
	if !ok {
		return nil, apierrors.NewNotFound(rtGroupResource, name)
	}
	return &item, nil
}

func (f *fakeRoutingTreeClient) GetMultiple(context.Context, resources.Descriptor, []string, metav1.GetOptions) ([]unstructured.Unstructured, error) {
	return nil, errors.New("not used")
}

func (f *fakeRoutingTreeClient) List(context.Context, resources.Descriptor, metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	f.record("list")
	if f.err != nil {
		return nil, f.err
	}
	list := &unstructured.UnstructuredList{}
	for _, name := range []string{"user-defined", "team-a", "team-b", "bare"} {
		if item, ok := f.items[name]; ok {
			list.Items = append(list.Items, item)
		}
	}
	return list, nil
}

func (f *fakeRoutingTreeClient) Delete(_ context.Context, _ resources.Descriptor, name string, _ metav1.DeleteOptions) error {
	f.record("delete " + name)
	return f.err
}

// recordingBinding returns a binding over client that records each Load.
func recordingBinding(client native.Client, loads *[]native.LoadOptions) native.Binding {
	return native.Func(func(_ context.Context, o native.LoadOptions) (native.Access, error) {
		*loads = append(*loads, o)
		version := "v1beta1"
		if o.APIVersion != "" {
			version, _ = native.ParseAPIVersion(rtGroup, o.APIVersion)
		}
		return native.Access{Client: client, Descriptor: rtDescriptor(version)}, nil
	})
}

type rtRun struct {
	stdout, stderr string
	err            error
}

func runRoutingTrees(t *testing.T, binding native.Binding, stdin string, args ...string) rtRun {
	t.Helper()
	cmd := alert.NewRoutingTreesCommandForTest(binding)
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(args)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	err := cmd.ExecuteContext(t.Context())
	return rtRun{stdout: stdout.String(), stderr: stderr.String(), err: err}
}

// exitCodeOf maps an error to the exit code the CLI would use.
func exitCodeOf(err error) int {
	if err == nil {
		return gcxerrors.ExitSuccess
	}
	d := fail.ErrorToDetailedError(err)
	if d == nil || d.ExitCode == nil {
		return gcxerrors.ExitGeneralError
	}
	return *d.ExitCode
}

func defaultRoutingTrees() map[string]unstructured.Unstructured {
	return map[string]unstructured.Unstructured{
		"user-defined": rtObject("user-defined", map[string]any{
			"defaults": map[string]any{"receiver": "grafana-default-email"},
			"routes":   []any{map[string]any{"receiver": "a"}, map[string]any{"receiver": "b"}},
		}, map[string]string{"grafana.com/provenance": "api"}),
		"team-a": rtObject("team-a", map[string]any{
			"defaults": map[string]any{"receiver": "team-a-email"},
			"routes":   []any{},
		}, nil),
		"bare": rtObject("bare", nil, nil),
	}
}

func TestRoutingTreesList(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantLines  []string
		wantAbsent []string
		wantLoad   string
	}{
		{
			name:       "table",
			args:       []string{"list"},
			wantLines:  []string{"NAME", "RECEIVER", "ROUTES", "user-defined", "grafana-default-email", "team-a", "team-a-email", "bare"},
			wantAbsent: []string{"PROVENANCE"},
		},
		{
			name:      "wide adds provenance",
			args:      []string{"list", "-o", "wide"},
			wantLines: []string{"PROVENANCE", "api"},
		},
		{
			name:     "api-version passes through to Load",
			args:     []string{"list", "--api-version", rtGroup + "/v0alpha1"},
			wantLoad: rtGroup + "/v0alpha1",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setAgentMode(t, false)
			client := &fakeRoutingTreeClient{items: defaultRoutingTrees()}
			var loads []native.LoadOptions

			res := runRoutingTrees(t, recordingBinding(client, &loads), "", tc.args...)
			require.NoError(t, res.err)
			for _, s := range tc.wantLines {
				assert.Contains(t, res.stdout, s)
			}
			for _, s := range tc.wantAbsent {
				assert.NotContains(t, res.stdout, s)
			}
			require.Len(t, loads, 1)
			assert.Equal(t, tc.wantLoad, loads[0].APIVersion)
		})
	}
}

func TestRoutingTreesList_TableCells(t *testing.T) {
	setAgentMode(t, false)
	client := &fakeRoutingTreeClient{items: defaultRoutingTrees()}
	var loads []native.LoadOptions

	res := runRoutingTrees(t, recordingBinding(client, &loads), "", "list")
	require.NoError(t, res.err)

	rows := map[string][]string{}
	for _, line := range strings.Split(strings.TrimSpace(res.stdout), "\n")[1:] {
		fields := strings.Fields(line)
		rows[fields[0]] = fields
	}
	assert.Equal(t, []string{"user-defined", "grafana-default-email", "2"}, rows["user-defined"])
	assert.Equal(t, []string{"team-a", "team-a-email", "0"}, rows["team-a"])
	// Missing spec fields render empty instead of failing.
	assert.Equal(t, []string{"bare"}, rows["bare"])
}

func TestRoutingTreesList_JSON(t *testing.T) {
	setAgentMode(t, false)
	client := &fakeRoutingTreeClient{items: defaultRoutingTrees()}
	var loads []native.LoadOptions

	res := runRoutingTrees(t, recordingBinding(client, &loads), "", "list", "-o", "json")
	require.NoError(t, res.err)

	var doc struct {
		Items []map[string]any `json:"items"`
	}
	require.NoError(t, json.Unmarshal([]byte(res.stdout), &doc))
	require.Len(t, doc.Items, 3)
	first := doc.Items[0]
	assert.Equal(t, rtGroup+"/v1beta1", first["apiVersion"])
	assert.Equal(t, "RoutingTree", first["kind"])
	meta, ok := first["metadata"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "42", meta["resourceVersion"])
	assert.Equal(t, map[string]any{"grafana.com/provenance": "api"}, meta["annotations"])
	assert.Contains(t, first, "spec")
}

func TestRoutingTreesGet(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantErr  bool
		wantCall string
		check    func(t *testing.T, stdout string)
	}{
		{
			name:     "found defaults to yaml",
			args:     []string{"get", "team-a"},
			wantCall: "get team-a",
			check: func(t *testing.T, stdout string) {
				t.Helper()
				var got map[string]any
				require.NoError(t, yaml.Unmarshal([]byte(stdout), &got))
				want := defaultRoutingTrees()["team-a"]
				assert.Equal(t, want.Object, got)
			},
		},
		{
			name:     "not found surfaces server error",
			args:     []string{"get", "team-z"},
			wantErr:  true,
			wantCall: "get team-z",
		},
		{
			name:     "default alias passed through without client-side aliasing",
			args:     []string{"get", "default"},
			wantErr:  true,
			wantCall: "get default",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setAgentMode(t, false)
			client := &fakeRoutingTreeClient{items: defaultRoutingTrees()}
			var loads []native.LoadOptions

			res := runRoutingTrees(t, recordingBinding(client, &loads), "", tc.args...)
			assert.Equal(t, []string{tc.wantCall}, client.calls)
			if tc.wantErr {
				require.Error(t, res.err)
				assert.True(t, apierrors.IsNotFound(res.err))
				assert.NotEqual(t, gcxerrors.ExitSuccess, exitCodeOf(res.err))
				return
			}
			require.NoError(t, res.err)
			tc.check(t, res.stdout)
		})
	}
}

const newTreeYAML = `apiVersion: notifications.alerting.grafana.app/v1beta1
kind: RoutingTree
metadata:
  name: team-c
spec:
  defaults:
    receiver: grafana-default-email
  routes: []
`

func manifestYAML(apiVersion, kind, name, resourceVersion string) string {
	var b strings.Builder
	b.WriteString("apiVersion: " + apiVersion + "\n")
	b.WriteString("kind: " + kind + "\n")
	b.WriteString("metadata:\n  name: " + name + "\n")
	if resourceVersion != "" {
		b.WriteString("  resourceVersion: \"" + resourceVersion + "\"\n")
	}
	b.WriteString("spec:\n  defaults:\n    receiver: grafana-default-email\n")
	return b.String()
}

func TestRoutingTreesCreate(t *testing.T) {
	tests := []struct {
		name      string
		stdin     string
		args      []string
		clientErr error
		wantCode  int
		wantErr   string
		wantLoad  string
		wantCalls []string
	}{
		{
			name:      "success from stdin",
			stdin:     newTreeYAML,
			args:      []string{"create", "-f", "-"},
			wantLoad:  "v1beta1",
			wantCalls: []string{"create team-c"},
		},
		{
			name:      "manifest version selects v0alpha1",
			stdin:     manifestYAML(rtGroup+"/v0alpha1", "RoutingTree", "team-c", ""),
			args:      []string{"create", "-f", "-", "--api-version", "v0alpha1"},
			wantLoad:  "v0alpha1",
			wantCalls: []string{"create team-c"},
		},
		{
			name:      "conflict passed through",
			stdin:     newTreeYAML,
			args:      []string{"create", "-f", "-"},
			clientErr: apierrors.NewAlreadyExists(rtGroupResource, "team-c"),
			wantCode:  gcxerrors.ExitGeneralError,
			wantErr:   "already exists",
			wantLoad:  "v1beta1",
			wantCalls: []string{"create team-c"},
		},
		{
			name:     "missing filename",
			args:     []string{"create"},
			wantCode: gcxerrors.ExitUsageError,
		},
		{
			name:     "wrong group",
			stdin:    manifestYAML("dashboard.grafana.app/v1", "RoutingTree", "team-c", ""),
			args:     []string{"create", "-f", "-"},
			wantCode: gcxerrors.ExitUsageError,
			wantErr:  "must be notifications.alerting.grafana.app/<version>",
		},
		{
			name:     "wrong kind",
			stdin:    manifestYAML(rtGroup+"/v1beta1", "Receiver", "team-c", ""),
			args:     []string{"create", "-f", "-"},
			wantCode: gcxerrors.ExitUsageError,
			wantErr:  `kind "Receiver" must be RoutingTree`,
		},
		{
			name:     "flag disagrees with manifest",
			stdin:    newTreeYAML,
			args:     []string{"create", "-f", "-", "--api-version", rtGroup + "/v0alpha1"},
			wantCode: gcxerrors.ExitUsageError,
			wantErr:  "conflicts with the manifest's apiVersion",
		},
		{
			name:     "flag in wrong group",
			stdin:    newTreeYAML,
			args:     []string{"create", "-f", "-", "--api-version", "dashboard.grafana.app/v1"},
			wantCode: gcxerrors.ExitUsageError,
			wantErr:  "--api-version",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setAgentMode(t, false)
			client := &fakeRoutingTreeClient{err: tc.clientErr}
			var loads []native.LoadOptions

			res := runRoutingTrees(t, recordingBinding(client, &loads), tc.stdin, tc.args...)
			assert.Equal(t, tc.wantCalls, client.calls)
			assert.Equal(t, tc.wantCode, exitCodeOf(res.err))
			if tc.wantErr != "" {
				require.ErrorContains(t, res.err, tc.wantErr)
			}
			if tc.wantLoad == "" {
				assert.Empty(t, loads, "Load must not run when validation fails")
				return
			}
			require.Len(t, loads, 1)
			assert.Equal(t, tc.wantLoad, loads[0].APIVersion)
			if tc.wantCode == gcxerrors.ExitSuccess {
				assert.Contains(t, res.stdout, `routing tree "team-c" created`)
			}
		})
	}
}

func TestRoutingTreesCreate_FromFile(t *testing.T) {
	setAgentMode(t, false)
	path := filepath.Join(t.TempDir(), "tree.yaml")
	require.NoError(t, os.WriteFile(path, []byte(newTreeYAML), 0o600))
	client := &fakeRoutingTreeClient{}
	var loads []native.LoadOptions

	res := runRoutingTrees(t, recordingBinding(client, &loads), "", "create", "-f", path, "-o", "json")
	require.NoError(t, res.err)
	assert.Equal(t, []string{"create team-c"}, client.calls)

	var doc map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.stdout), &doc))
	assert.Equal(t, "created", doc["action"])
}

func TestRoutingTreesUpdate(t *testing.T) {
	valid := manifestYAML(rtGroup+"/v1beta1", "RoutingTree", "team-a", "42")

	tests := []struct {
		name      string
		stdin     string
		args      []string
		clientErr error
		wantCode  int
		wantErr   string
		wantLoad  string
		wantCalls []string
	}{
		{
			name:      "success",
			stdin:     valid,
			args:      []string{"update", "team-a", "-f", "-"},
			wantLoad:  "v1beta1",
			wantCalls: []string{"update team-a"},
		},
		{
			name:      "manifest version selects v0alpha1",
			stdin:     manifestYAML(rtGroup+"/v0alpha1", "RoutingTree", "team-a", "42"),
			args:      []string{"update", "team-a", "-f", "-"},
			wantLoad:  "v0alpha1",
			wantCalls: []string{"update team-a"},
		},
		{
			name:      "stale version conflict passed through",
			stdin:     valid,
			args:      []string{"update", "team-a", "-f", "-"},
			clientErr: apierrors.NewConflict(rtGroupResource, "team-a", errors.New("the object has been modified")),
			wantCode:  gcxerrors.ExitGeneralError,
			wantErr:   "the object has been modified",
			wantLoad:  "v1beta1",
			wantCalls: []string{"update team-a"},
		},
		{
			name:     "missing resource version",
			stdin:    manifestYAML(rtGroup+"/v1beta1", "RoutingTree", "team-a", ""),
			args:     []string{"update", "team-a", "-f", "-"},
			wantCode: gcxerrors.ExitUsageError,
			wantErr:  "no metadata.resourceVersion",
		},
		{
			name:     "name mismatch",
			stdin:    valid,
			args:     []string{"update", "team-b", "-f", "-"},
			wantCode: gcxerrors.ExitUsageError,
			wantErr:  `name argument "team-b" does not match`,
		},
		{
			name:     "wrong kind",
			stdin:    manifestYAML(rtGroup+"/v1beta1", "Receiver", "team-a", "42"),
			args:     []string{"update", "team-a", "-f", "-"},
			wantCode: gcxerrors.ExitUsageError,
		},
		{
			name:     "flag disagrees with manifest",
			stdin:    valid,
			args:     []string{"update", "team-a", "-f", "-", "--api-version", "v0alpha1"},
			wantCode: gcxerrors.ExitUsageError,
			wantErr:  "conflicts with the manifest's apiVersion",
		},
		{
			name:     "missing filename",
			args:     []string{"update", "team-a"},
			wantCode: gcxerrors.ExitUsageError,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setAgentMode(t, false)
			client := &fakeRoutingTreeClient{err: tc.clientErr}
			var loads []native.LoadOptions

			res := runRoutingTrees(t, recordingBinding(client, &loads), tc.stdin, tc.args...)
			assert.Equal(t, tc.wantCalls, client.calls)
			assert.Equal(t, tc.wantCode, exitCodeOf(res.err))
			if tc.wantErr != "" {
				require.ErrorContains(t, res.err, tc.wantErr)
			}
			if tc.wantLoad == "" {
				assert.Empty(t, loads, "Load must not run when validation fails")
				return
			}
			require.Len(t, loads, 1)
			assert.Equal(t, tc.wantLoad, loads[0].APIVersion)
			if tc.wantCode == gcxerrors.ExitSuccess {
				assert.Equal(t, "42", client.lastObj.GetResourceVersion())
				assert.Contains(t, res.stdout, `routing tree "team-a" updated`)
			}
		})
	}
}

func TestRoutingTreesDelete(t *testing.T) {
	tests := []struct {
		name      string
		agentMode bool
		stdin     string
		args      []string
		wantCode  int
		wantErr   string
		wantOut   string
		wantCalls []string
	}{
		{
			name:      "forced delete of named tree",
			args:      []string{"delete", "team-a", "--force"},
			wantOut:   `routing tree "team-a" deleted`,
			wantCalls: []string{"delete team-a"},
		},
		{
			name:      "default tree reports reset",
			args:      []string{"delete", "user-defined", "--force"},
			wantOut:   `default routing tree "user-defined" reset`,
			wantCalls: []string{"delete user-defined"},
		},
		{
			name:      "default alias reports reset",
			args:      []string{"delete", "default", "--force"},
			wantOut:   `default routing tree "default" reset`,
			wantCalls: []string{"delete default"},
		},
		{
			name:      "confirmed prompt",
			stdin:     "y\n",
			args:      []string{"delete", "team-a"},
			wantOut:   `routing tree "team-a" deleted`,
			wantCalls: []string{"delete team-a"},
		},
		{
			name:     "declined prompt is cancelled",
			stdin:    "n\n",
			args:     []string{"delete", "team-a"},
			wantCode: gcxerrors.ExitCancelled,
			wantErr:  "delete cancelled",
		},
		{
			name:     "closed stdin names --force",
			stdin:    "",
			args:     []string{"delete", "team-a"},
			wantCode: gcxerrors.ExitGeneralError,
			wantErr:  "--force",
		},
		{
			name:      "agent mode without force names --force",
			agentMode: true,
			args:      []string{"delete", "team-a"},
			wantCode:  gcxerrors.ExitGeneralError,
			wantErr:   "--force",
		},
		{
			name:     "api-version in wrong group",
			args:     []string{"delete", "team-a", "--force", "--api-version", "dashboard.grafana.app/v1"},
			wantCode: gcxerrors.ExitUsageError,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setAgentMode(t, tc.agentMode)
			client := &fakeRoutingTreeClient{}
			var loads []native.LoadOptions

			res := runRoutingTrees(t, recordingBinding(client, &loads), tc.stdin, tc.args...)
			assert.Equal(t, tc.wantCalls, client.calls)
			assert.Equal(t, tc.wantCode, exitCodeOf(res.err))
			if tc.wantErr != "" {
				require.ErrorContains(t, res.err, tc.wantErr)
			}
			if tc.wantCalls == nil {
				assert.Empty(t, loads, "Load must not run before confirmation")
				return
			}
			assert.Contains(t, res.stdout, tc.wantOut)
		})
	}
}

func TestRoutingTreesDelete_ReceiptJSON(t *testing.T) {
	setAgentMode(t, false)
	client := &fakeRoutingTreeClient{}
	var loads []native.LoadOptions

	res := runRoutingTrees(t, recordingBinding(client, &loads), "", "delete", "user-defined", "--force", "-o", "json")
	require.NoError(t, res.err)

	var doc map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.stdout), &doc))
	assert.Equal(t, "reset", doc["action"])
	target, ok := doc["target"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "user-defined", target["name"])
}

func TestRoutingTreesCommand_Verbs(t *testing.T) {
	cmd := alert.NewRoutingTreesCommandForTest(native.Fixed(native.Access{}))
	names := make([]string, 0, len(cmd.Commands()))
	for _, c := range cmd.Commands() {
		names = append(names, c.Name())
	}
	assert.ElementsMatch(t, []string{"list", "get", "create", "update", "delete"}, names)
	assert.Contains(t, cmd.Aliases, "routing-tree")
}
