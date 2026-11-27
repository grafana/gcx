package dashboards

import (
	"bytes"
	"testing"

	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// renderSample runs the sample instance through the tree command's options
// pipeline and encodes it with the selected output format.
func renderSample(t *testing.T, args []string, folderArgs ...string) string {
	t.Helper()
	testutils.SetAgentMode(t, false)

	o := newTreeOptsForTest(t, args...)
	require.NoError(t, o.Validate())

	dashboards, folders := sampleTree()
	tree, err := assembleDashboardTree(dashboards, folders, folderArgs, o)
	require.NoError(t, err)
	tree.label = "https://example.grafana.net"

	var buf bytes.Buffer
	require.NoError(t, o.IO.Encode(&buf, tree))
	return buf.String()
}

func TestDashboardTreeGolden(t *testing.T) {
	tests := []struct {
		golden     string
		args       []string
		folderArgs []string
	}{
		{golden: "tree_text"},
		{golden: "tree_text_classify_uid_date", args: []string{"-F", "--uid", "-D"}},
		{golden: "tree_text_created_date", args: []string{"-cD", "--time-format", "2006-01-02"}},
		{golden: "tree_text_full_path_flat", args: []string{"-fi", "--skip-report"}},
		{golden: "tree_text_selected", folderArgs: []string{"ceph", "Applications"}},
		{golden: "tree_text_folders_only", args: []string{"-d", "-L", "2"}},
		{golden: "tree_text_files_first_file_limit", args: []string{"--files-first", "--file-limit", "2"}},
		{golden: "tree_text_pattern_prune", args: []string{"-P", "*overview*", "--ignore-case", "--prune"}},
		{golden: "tree_json", args: []string{"-o", "json"}},
		{golden: "tree_json_level", args: []string{"-o", "json", "-L", "1", "-d"}},
		{golden: "tree_yaml", args: []string{"-o", "yaml"}},
	}
	for _, tc := range tests {
		t.Run(tc.golden, func(t *testing.T) {
			testutils.Golden(t, tc.golden, renderSample(t, tc.args, tc.folderArgs...))
		})
	}
}

func TestDashboardTreeTextEmpty(t *testing.T) {
	testutils.SetAgentMode(t, false)

	var buf bytes.Buffer
	codec := &dashboardTreeTextCodec{opts: &treeRender{}}
	require.NoError(t, codec.Encode(&buf, &dashboardTree{label: "https://example.grafana.net"}))
	assert.Equal(t, "https://example.grafana.net\n\n0 folders, 0 dashboards\n", buf.String())
}

func TestDashboardTreeTextUnreadableFolder(t *testing.T) {
	testutils.SetAgentMode(t, false)

	root, _ := buildDashboardTree([]unstructured.Unstructured{dashboardFixture("d", "Hidden", "secret")}, nil)
	var buf bytes.Buffer
	codec := &dashboardTreeTextCodec{opts: &treeRender{}}
	require.NoError(t, codec.Encode(&buf, &dashboardTree{Folders: root.Folders, Dashboards: root.Dashboards}))
	assert.Equal(t, ".\n└── secret (folder not readable)\n    └── Hidden\n\n1 folder, 1 dashboard\n", buf.String())
}

func TestDashboardTreeTextColor(t *testing.T) {
	testutils.SetAgentMode(t, false)

	assert.Contains(t, renderSample(t, []string{"-C"}), "\x1b[", "-C forces color when output is not a terminal")
	assert.NotContains(t, renderSample(t, nil), "\x1b[", "no color when output is not a terminal")
	assert.NotContains(t, renderSample(t, []string{"-n"}), "\x1b[")
}

func TestDashboardTreeTextCodecRejectsOtherTypes(t *testing.T) {
	var buf bytes.Buffer
	require.Error(t, (&dashboardTreeTextCodec{}).Encode(&buf, &unstructured.UnstructuredList{}))
}
