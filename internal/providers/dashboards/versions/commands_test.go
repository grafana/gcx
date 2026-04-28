package versions_test

// External test package for the versions command group.
// Internal symbols (commandDeps, newListCommand, newRestoreCommand) are accessed
// through the export_test.go shim (TestCommandDeps, TestListCommand, TestRestoreCommand).

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/providers/dashboards/versions"
	"github.com/grafana/gcx/internal/resources"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// ---------------------------------------------------------------------------
// Fake client
// ---------------------------------------------------------------------------

// fakeVersionsClient is a test double that implements versions.DashboardVersionsClient
// without any real K8s connectivity. Tests configure expected responses and
// inspect captured call parameters.
type fakeVersionsClient struct {
	// Preconfigured responses.
	historyItems []unstructured.Unstructured
	historyErr   error
	currentItem  *unstructured.Unstructured
	currentErr   error
	updatedItem  *unstructured.Unstructured
	updateErr    error

	// Captured call details (verified by tests).
	listCalled       bool
	getCalled        bool
	updateCalled     bool
	capturedListOpts metav1.ListOptions
	capturedUpdate   *unstructured.Unstructured
}

func (f *fakeVersionsClient) List(
	_ context.Context, _ resources.Descriptor, opts metav1.ListOptions,
) (*unstructured.UnstructuredList, error) {
	f.listCalled = true
	f.capturedListOpts = opts

	if f.historyErr != nil {
		return nil, f.historyErr
	}

	return &unstructured.UnstructuredList{Items: f.historyItems}, nil
}

func (f *fakeVersionsClient) Get(
	_ context.Context, _ resources.Descriptor, _ string, _ metav1.GetOptions,
) (*unstructured.Unstructured, error) {
	f.getCalled = true
	if f.currentErr != nil {
		return nil, f.currentErr
	}

	return f.currentItem, nil
}

func (f *fakeVersionsClient) Update(
	_ context.Context, _ resources.Descriptor, obj *unstructured.Unstructured, _ metav1.UpdateOptions,
) (*unstructured.Unstructured, error) {
	f.updateCalled = true
	f.capturedUpdate = obj.DeepCopy()

	if f.updateErr != nil {
		return nil, f.updateErr
	}

	return f.updatedItem, nil
}

// testDesc returns a minimal Descriptor for dashboard resources used in tests.
func testDesc() resources.Descriptor {
	return resources.Descriptor{
		GroupVersion: schema.GroupVersion{Group: "dashboard.grafana.app", Version: "v1"},
		Kind:         "Dashboard",
		Singular:     "dashboard",
		Plural:       "dashboards",
	}
}

// runVersionsCmd builds a minimal cobra parent with list+restore children and
// executes with the given args. It returns stdout, stderr, and any error.
func runVersionsCmd(
	t *testing.T,
	fc *fakeVersionsClient,
	args []string,
	stdinContent string,
) (string, string, error) {
	t.Helper()

	deps := versions.NewTestCommandDeps(fc, testDesc())

	parent := &cobra.Command{
		Use:           "versions",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	parent.AddCommand(versions.NewTestListCommand(deps))
	parent.AddCommand(versions.NewTestRestoreCommand(deps))

	var outBuf, errBuf bytes.Buffer
	parent.SetOut(&outBuf)
	parent.SetErr(&errBuf)
	parent.SetIn(strings.NewReader(stdinContent))
	parent.SetContext(context.Background())
	parent.SetArgs(args)

	err := parent.Execute()
	return outBuf.String(), errBuf.String(), err
}

// ---------------------------------------------------------------------------
// Test helpers: build minimal unstructured objects
// ---------------------------------------------------------------------------

// historyItem builds a dashboard revision for the fake LIST-history result.
func historyItem(generation int64, timestamp, author, message string, spec map[string]any) unstructured.Unstructured {
	obj := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "dashboard.grafana.app/v1",
			"kind":       "Dashboard",
			"metadata": map[string]any{
				"name":      "foo",
				"namespace": "default",
			},
			"spec": spec,
		},
	}
	obj.SetGeneration(generation)

	ann := map[string]string{}
	if timestamp != "" {
		ann["grafana.app/updatedTimestamp"] = timestamp
	}
	if author != "" {
		ann["grafana.app/updatedBy"] = author
	}
	if message != "" {
		ann["grafana.app/message"] = message
	}
	if len(ann) > 0 {
		obj.SetAnnotations(ann)
	}

	return obj
}

// currentDashboard builds the "current" dashboard object returned by GET.
func currentDashboard(generation int64, resourceVersion string, spec map[string]any) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "dashboard.grafana.app/v1",
			"kind":       "Dashboard",
			"metadata": map[string]any{
				"name":      "foo",
				"namespace": "default",
			},
			"spec": spec,
		},
	}
	obj.SetGeneration(generation)
	obj.SetResourceVersion(resourceVersion)

	return obj
}

// updatedDashboard builds the response returned by the fake Update (PUT).
func updatedDashboard(newGeneration int64) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "dashboard.grafana.app/v1",
			"kind":       "Dashboard",
			"metadata": map[string]any{
				"name":      "foo",
				"namespace": "default",
			},
		},
	}
	obj.SetGeneration(newGeneration)

	return obj
}

// ---------------------------------------------------------------------------
// Tests: versions list
// ---------------------------------------------------------------------------

func TestVersionsList_Selectors(t *testing.T) {
	// Scenario A: LIST must issue the exact magic selectors.
	fc := &fakeVersionsClient{
		historyItems: []unstructured.Unstructured{
			historyItem(1, "2024-01-01T00:00:00Z", "alice", "initial", map[string]any{"title": "v1"}),
			historyItem(2, "2024-01-02T00:00:00Z", "bob", "update", map[string]any{"title": "v2"}),
		},
	}

	_, _, err := runVersionsCmd(t, fc, []string{"list", "foo"}, "")
	require.NoError(t, err)

	assert.True(t, fc.listCalled, "List must be called")
	assert.Equal(t, "grafana.app/get-history=true", fc.capturedListOpts.LabelSelector,
		"list must use LabelSelector=grafana.app/get-history=true")
	assert.Equal(t, "metadata.name=foo", fc.capturedListOpts.FieldSelector,
		"list must use FieldSelector=metadata.name=<name>")
}

func TestVersionsList_Selectors_DifferentName(t *testing.T) {
	// Verify the field selector encodes the exact name argument from the CLI.
	fc := &fakeVersionsClient{
		historyItems: nil,
	}

	_, _, err := runVersionsCmd(t, fc, []string{"list", "my-dashboard"}, "")
	require.NoError(t, err)

	assert.Equal(t, "metadata.name=my-dashboard", fc.capturedListOpts.FieldSelector,
		"field selector must use the dashboard name from args")
}

func TestVersionsList_OutputColumns(t *testing.T) {
	// Scenario: list renders VERSION TIMESTAMP AUTHOR MESSAGE columns in descending order.
	fc := &fakeVersionsClient{
		historyItems: []unstructured.Unstructured{
			// Return items in ascending order; list must sort descending.
			historyItem(1, "2024-01-01T10:00:00Z", "alice", "first commit", map[string]any{"title": "v1"}),
			historyItem(3, "2024-01-03T12:00:00Z", "carol", "third commit", map[string]any{"title": "v3"}),
			historyItem(2, "2024-01-02T11:00:00Z", "bob", "second commit", map[string]any{"title": "v2"}),
		},
	}

	// Pass -o table explicitly: agent-mode env (CI/Claude Code) defaults to JSON,
	// but this test is specifically verifying table rendering.
	out, _, err := runVersionsCmd(t, fc, []string{"list", "-o", "table", "foo"}, "")
	require.NoError(t, err)

	// Required columns must appear.
	assert.Contains(t, out, "VERSION", "output must contain VERSION header")
	assert.Contains(t, out, "TIMESTAMP", "output must contain TIMESTAMP header")
	assert.Contains(t, out, "AUTHOR", "output must contain AUTHOR header")
	assert.Contains(t, out, "MESSAGE", "output must contain MESSAGE header")

	// Content from annotations must appear.
	assert.Contains(t, out, "alice")
	assert.Contains(t, out, "bob")
	assert.Contains(t, out, "carol")
	assert.Contains(t, out, "first commit")
	assert.Contains(t, out, "second commit")
	assert.Contains(t, out, "third commit")

	// Descending order: 3 must appear before 1.
	pos3 := strings.Index(out, "3")
	pos1 := strings.LastIndex(out, "1")
	assert.Less(t, pos3, pos1, "generation 3 must appear before generation 1 in descending order")
}

func TestVersionsList_MissingAnnotationsRenderEmpty(t *testing.T) {
	// Items with no annotations must render empty strings, not "<nil>".
	fc := &fakeVersionsClient{
		historyItems: []unstructured.Unstructured{
			// No annotations set.
			historyItem(1, "", "", "", map[string]any{"title": "v1"}),
		},
	}

	// -o table: checking for <nil> vs empty is a table rendering concern.
	out, _, err := runVersionsCmd(t, fc, []string{"list", "-o", "table", "foo"}, "")
	require.NoError(t, err)

	assert.NotContains(t, out, "<nil>", "nil annotations must not render as <nil>")
}

func TestVersionsList_TimestampNotFromCreationTimestamp(t *testing.T) {
	// TIMESTAMP must come from the annotation, not metadata.creationTimestamp.
	// We set creationTimestamp to a known value and the annotation to a different value.
	item := historyItem(1, "2024-06-15T09:00:00Z", "alice", "check timestamp", map[string]any{})
	// Set creationTimestamp to a distinctly different value.
	ts, err := time.Parse(time.RFC3339, "2023-01-01T00:00:00Z")
	require.NoError(t, err)
	item.SetCreationTimestamp(metav1.NewTime(ts))

	fc := &fakeVersionsClient{
		historyItems: []unstructured.Unstructured{item},
	}

	// Pass -o table explicitly so the annotation value (not creationTimestamp) appears in the
	// TIMESTAMP column — agent-mode env defaults to JSON which would include both timestamps.
	out, _, listErr := runVersionsCmd(t, fc, []string{"list", "-o", "table", "foo"}, "")
	require.NoError(t, listErr)

	// The annotation timestamp must appear.
	assert.Contains(t, out, "2024-06-15T09:00:00Z",
		"TIMESTAMP must come from grafana.app/updatedTimestamp annotation")
	// The creationTimestamp value must NOT appear.
	assert.NotContains(t, out, "2023-01-01T00:00:00Z",
		"TIMESTAMP must NOT come from metadata.creationTimestamp")
}

// ---------------------------------------------------------------------------
// Tests: versions restore
// ---------------------------------------------------------------------------

func TestVersionsRestore_HappyPath(t *testing.T) {
	// Scenario B: happy path — LIST + GET + PUT with correct spec, resourceVersion,
	// and annotation.
	historicalSpec := map[string]any{
		"title": "Historical Title",
		"panels": []any{
			map[string]any{"id": float64(1)},
		},
	}
	currentSpec := map[string]any{
		"title": "Current Title",
	}

	fc := &fakeVersionsClient{
		historyItems: []unstructured.Unstructured{
			historyItem(1, "2024-01-01T00:00:00Z", "alice", "v1 commit", historicalSpec),
			historyItem(2, "2024-01-02T00:00:00Z", "bob", "v2 commit", currentSpec),
		},
		currentItem: currentDashboard(2, "rv-abc123", currentSpec),
		updatedItem: updatedDashboard(3),
	}

	_, errOut, err := runVersionsCmd(t, fc, []string{"restore", "foo", "1", "--yes"}, "")
	require.NoError(t, err, "restore to a valid version must succeed")

	// Must have called LIST, GET, and UPDATE.
	assert.True(t, fc.listCalled, "must call List for history")
	assert.True(t, fc.getCalled, "must call Get for current")
	assert.True(t, fc.updateCalled, "must call Update (PUT)")

	// Verify the update object has the correct resourceVersion.
	require.NotNil(t, fc.capturedUpdate)
	assert.Equal(t, "rv-abc123", fc.capturedUpdate.GetResourceVersion(),
		"PUT must carry resourceVersion from the GET response")

	// Verify the spec is the historical one (not current).
	gotTitle, _, _ := unstructured.NestedString(fc.capturedUpdate.Object, "spec", "title")
	assert.Equal(t, "Historical Title", gotTitle,
		"PUT must carry the historical spec")

	// Verify annotation.
	ann := fc.capturedUpdate.GetAnnotations()
	require.NotNil(t, ann, "update object must have annotations")
	assert.Equal(t, "Restored from version 1", ann["grafana.app/message"],
		"default restore message must be set")

	// cmdio.Success must be written to stderr.
	assert.Contains(t, errOut, "restored to version 1",
		"cmdio.Success must appear on stderr")
}

func TestVersionsRestore_MessageOverride(t *testing.T) {
	// Scenario C: --message MSG overrides the default annotation value.
	fc := &fakeVersionsClient{
		historyItems: []unstructured.Unstructured{
			historyItem(1, "", "", "", map[string]any{"title": "v1"}),
		},
		currentItem: currentDashboard(3, "rv-xyz", map[string]any{"title": "current"}),
		updatedItem: updatedDashboard(4),
	}

	_, _, err := runVersionsCmd(t, fc,
		[]string{"restore", "foo", "1", "--yes", "--message", "rolling back last week's change"},
		"")
	require.NoError(t, err)
	require.True(t, fc.updateCalled)

	ann := fc.capturedUpdate.GetAnnotations()
	require.NotNil(t, ann)
	assert.Equal(t, "rolling back last week's change", ann["grafana.app/message"],
		"--message flag must override the default annotation value")
}

func TestVersionsRestore_409Conflict(t *testing.T) {
	// Scenario D: 409 on PUT must surface as a non-zero exit.
	conflictErr := apierrors.NewConflict(
		schema.GroupResource{Group: "dashboard.grafana.app", Resource: "dashboards"},
		"foo",
		errors.New("resource version mismatch"),
	)

	fc := &fakeVersionsClient{
		historyItems: []unstructured.Unstructured{
			historyItem(1, "", "", "", map[string]any{"title": "v1"}),
		},
		currentItem: currentDashboard(3, "rv-stale", map[string]any{"title": "current"}),
		updateErr:   conflictErr,
	}

	_, _, err := runVersionsCmd(t, fc, []string{"restore", "foo", "1", "--yes"}, "")
	require.Error(t, err, "409 conflict must result in a non-zero exit")

	assert.True(t, fc.updateCalled, "Update must be called before the 409 is observed")
}

func TestVersionsRestore_NoOpWhenAlreadyAtTarget(t *testing.T) {
	// Scenario E: target generation == current generation → exit 0 without PUT.
	fc := &fakeVersionsClient{
		historyItems: []unstructured.Unstructured{
			historyItem(3, "2024-01-03T00:00:00Z", "alice", "v3", map[string]any{"title": "v3"}),
		},
		currentItem: currentDashboard(3, "rv-current", map[string]any{"title": "v3"}),
	}

	_, errOut, err := runVersionsCmd(t, fc, []string{"restore", "foo", "3", "--yes"}, "")
	require.NoError(t, err, "no-op restore must exit 0")

	assert.False(t, fc.updateCalled, "no PUT must be issued when already at target version")
	assert.Contains(t, errOut, "already at version 3",
		"cmdio.Success must be emitted on stderr when already at target")
}

func TestVersionsRestore_TargetNotFound(t *testing.T) {
	// Scenario F: target version not in history → non-zero exit with clear message.
	fc := &fakeVersionsClient{
		historyItems: []unstructured.Unstructured{
			historyItem(1, "", "", "", map[string]any{}),
			historyItem(2, "", "", "", map[string]any{}),
		},
		currentItem: currentDashboard(2, "rv-x", map[string]any{}),
	}

	_, _, err := runVersionsCmd(t, fc, []string{"restore", "foo", "99", "--yes"}, "")
	require.Error(t, err, "missing target version must be a non-zero exit")
	assert.Contains(t, err.Error(), "99",
		"error must mention the requested version number")

	assert.False(t, fc.updateCalled, "no PUT must be issued for a missing version")
}

func TestVersionsRestore_NonIntegerVersionFails(t *testing.T) {
	// Scenario H: non-integer <version> must fail with parse error before any HTTP.
	fc := &fakeVersionsClient{}

	_, _, err := runVersionsCmd(t, fc, []string{"restore", "foo", "notaninteger", "--yes"}, "")
	require.Error(t, err, "non-integer version must cause parse error")

	assert.False(t, fc.listCalled,
		"no HTTP call must be issued when version cannot be parsed")
	assert.False(t, fc.getCalled,
		"no HTTP call must be issued when version cannot be parsed")
	assert.False(t, fc.updateCalled,
		"no HTTP call must be issued when version cannot be parsed")
}

func TestVersionsRestore_NoLegacyRestoreEndpoint(t *testing.T) {
	// Scenario G: the legacy POST /api/dashboards/uid/{uid}/restore must never be called.
	//
	// Our implementation routes through DashboardVersionsClient (the fakeVersionsClient
	// here). Any call to the legacy REST endpoint would bypass this fake entirely,
	// which means it would fail to compile or panic (no real server is wired).
	// The only Update method observed here is the K8s PUT path.
	fc := &fakeVersionsClient{
		historyItems: []unstructured.Unstructured{
			historyItem(1, "", "", "", map[string]any{"title": "v1"}),
		},
		currentItem: currentDashboard(3, "rv-ok", map[string]any{"title": "current"}),
		updatedItem: updatedDashboard(4),
	}

	_, _, err := runVersionsCmd(t, fc, []string{"restore", "foo", "1", "--yes"}, "")
	require.NoError(t, err)

	// The only update call must go through our fake's Update method.
	assert.True(t, fc.updateCalled,
		"restore must call Update on the K8s client (not a legacy HTTP endpoint)")
}

func TestVersionsRestore_RestoreListSelectors(t *testing.T) {
	// Verify that restore also uses the magic selectors for its history LIST.
	fc := &fakeVersionsClient{
		historyItems: []unstructured.Unstructured{
			historyItem(1, "", "", "", map[string]any{}),
		},
		currentItem: currentDashboard(3, "rv-z", map[string]any{}),
		updatedItem: updatedDashboard(4),
	}

	_, _, err := runVersionsCmd(t, fc, []string{"restore", "foo", "1", "--yes"}, "")
	require.NoError(t, err)

	assert.Equal(t, "grafana.app/get-history=true", fc.capturedListOpts.LabelSelector,
		"restore LIST must use the magic LabelSelector")
	assert.Equal(t, "metadata.name=foo", fc.capturedListOpts.FieldSelector,
		"restore LIST must filter by metadata.name")
}

func TestVersionsRestore_ConfirmPromptAbort(t *testing.T) {
	// When --yes is NOT set, restore must prompt on stderr and abort on "n".
	fc := &fakeVersionsClient{
		historyItems: []unstructured.Unstructured{
			historyItem(1, "", "", "", map[string]any{"title": "v1"}),
		},
		currentItem: currentDashboard(3, "rv-q", map[string]any{"title": "current"}),
	}

	// Answer "n" on stdin.
	_, errOut, err := runVersionsCmd(t, fc, []string{"restore", "foo", "1"}, "n\n")
	require.NoError(t, err, "aborting the prompt must exit 0")

	assert.False(t, fc.updateCalled, "no PUT must be issued when user says no")
	assert.Contains(t, errOut, "Restore dashboard",
		"prompt message must appear on stderr")
}

func TestVersionsRestore_ConfirmPromptProceed(t *testing.T) {
	// When --yes is NOT set and user types "y", restore must proceed.
	fc := &fakeVersionsClient{
		historyItems: []unstructured.Unstructured{
			historyItem(1, "", "", "", map[string]any{"title": "v1"}),
		},
		currentItem: currentDashboard(3, "rv-q", map[string]any{"title": "current"}),
		updatedItem: updatedDashboard(4),
	}

	_, _, err := runVersionsCmd(t, fc, []string{"restore", "foo", "1"}, "y\n")
	require.NoError(t, err)
	assert.True(t, fc.updateCalled, "PUT must be issued when user confirms")
}

// ---------------------------------------------------------------------------
// Tests: versions list codec
// ---------------------------------------------------------------------------

func TestVersionsTableCodec_EmptyList(t *testing.T) {
	// An empty revision list must still render the column headers.
	fc := &fakeVersionsClient{
		historyItems: nil,
	}

	// -o table: agent-mode env defaults to JSON; this test specifically checks table headers.
	out, _, err := runVersionsCmd(t, fc, []string{"list", "-o", "table", "foo"}, "")
	require.NoError(t, err)

	assert.Contains(t, out, "VERSION")
	assert.Contains(t, out, "TIMESTAMP")
	assert.Contains(t, out, "AUTHOR")
	assert.Contains(t, out, "MESSAGE")
}

func TestVersionsTableCodec_AnnotationValues(t *testing.T) {
	fc := &fakeVersionsClient{
		historyItems: []unstructured.Unstructured{
			historyItem(5, "2024-06-01T10:00:00Z", "grafana-bot", "auto-save", map[string]any{}),
		},
	}

	out, _, err := runVersionsCmd(t, fc, []string{"list", "foo"}, "")
	require.NoError(t, err)

	assert.Contains(t, out, "5")
	assert.Contains(t, out, "2024-06-01T10:00:00Z")
	assert.Contains(t, out, "grafana-bot")
	assert.Contains(t, out, "auto-save")
}
