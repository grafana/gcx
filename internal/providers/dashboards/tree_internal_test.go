package dashboards

import (
	"bytes"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// fixtureTime returns a fixed UTC time on 2026-01-<day>.
func fixtureTime(day int) time.Time {
	return time.Date(2026, time.January, day, 12, 0, 0, 0, time.UTC)
}

func folderFixture(name, title, parent string) unstructured.Unstructured {
	meta := map[string]any{"name": name}
	if parent != "" {
		meta["annotations"] = map[string]any{folderAnnotation: parent}
	}
	return unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "folder.grafana.app/v1",
		"kind":       "Folder",
		"metadata":   meta,
		"spec":       map[string]any{"title": title},
	}}
}

func dashboardFixture(name, title, folder string) unstructured.Unstructured {
	meta := map[string]any{"name": name}
	if folder != "" {
		meta["annotations"] = map[string]any{folderAnnotation: folder}
	}
	spec := map[string]any{}
	if title != "" {
		spec["title"] = title
	}
	return unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "dashboard.grafana.app/v1",
		"kind":       "Dashboard",
		"metadata":   meta,
		"spec":       spec,
	}}
}

// withTimes sets the creation time and Grafana's updatedTimestamp annotation.
func withTimes(obj unstructured.Unstructured, created, updated time.Time) unstructured.Unstructured {
	obj.SetCreationTimestamp(metav1.NewTime(created))
	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[updatedTimestampAnnotation] = updated.Format(time.RFC3339)
	obj.SetAnnotations(annotations)
	return obj
}

// sampleTree is a small instance used across tests:
//
//	Applications/        Home Assistant, Static Pages / Overview
//	Infrastructure/      Tailscale
//	  Ceph/              Ceph - Cluster, OSD Overview
//	  Raspberry Pi/      Raspberry Pi Fleet
//	Performance Testing/ (empty)
//	Incident Insights    (no folder)
func sampleTree() ([]unstructured.Unstructured, []unstructured.Unstructured) {
	folders := []unstructured.Unstructured{
		withTimes(folderFixture("apps", "Applications", ""), fixtureTime(1), fixtureTime(1)),
		withTimes(folderFixture("infra", "Infrastructure", ""), fixtureTime(2), fixtureTime(2)),
		withTimes(folderFixture("ceph", "Ceph", "infra"), fixtureTime(3), fixtureTime(3)),
		withTimes(folderFixture("pi", "Raspberry Pi", "infra"), fixtureTime(4), fixtureTime(4)),
		withTimes(folderFixture("k6", "Performance Testing", ""), fixtureTime(5), fixtureTime(5)),
	}
	dashboards := []unstructured.Unstructured{
		withTimes(dashboardFixture("ha", "Home Assistant", "apps"), fixtureTime(6), fixtureTime(20)),
		withTimes(dashboardFixture("static", "Static Pages / Overview", "apps"), fixtureTime(7), fixtureTime(8)),
		withTimes(dashboardFixture("ceph-cluster", "Ceph - Cluster", "ceph"), fixtureTime(9), fixtureTime(9)),
		withTimes(dashboardFixture("osd", "OSD Overview", "ceph"), fixtureTime(10), fixtureTime(30)),
		withTimes(dashboardFixture("fleet", "Raspberry Pi Fleet", "pi"), fixtureTime(11), fixtureTime(11)),
		withTimes(dashboardFixture("tailscale", "Tailscale", "infra"), fixtureTime(12), fixtureTime(12)),
		withTimes(dashboardFixture("incidents", "Incident Insights", ""), fixtureTime(13), fixtureTime(13)),
	}
	return dashboards, folders
}

// titles flattens a tree into paths, folders marked with a trailing slash,
// in render order (folders before dashboards at each level).
func titles(node *treeFolder, prefix string) []string {
	out := make([]string, 0, len(node.Folders)+len(node.Dashboards))
	for _, f := range node.Folders {
		path := joinTreePath(prefix, f.Title)
		out = append(out, path+"/")
		out = append(out, titles(f, path)...)
	}
	for _, d := range node.Dashboards {
		out = append(out, joinTreePath(prefix, d.Title))
	}
	return out
}

func TestBuildDashboardTree(t *testing.T) {
	tests := []struct {
		name       string
		dashboards []unstructured.Unstructured
		folders    []unstructured.Unstructured
		want       []string
	}{
		{
			name: "empty",
			want: []string{},
		},
		{
			name:       "nested folders keep input order",
			folders:    []unstructured.Unstructured{folderFixture("b", "Beta", ""), folderFixture("a", "Alpha", ""), folderFixture("c", "Child", "a")},
			dashboards: []unstructured.Unstructured{dashboardFixture("d1", "In Child", "c"), dashboardFixture("d2", "Root", "")},
			want:       []string{"Beta/", "Alpha/", "Alpha/Child/", "Alpha/Child/In Child", "Root"},
		},
		{
			name:       "titles fall back to names",
			folders:    []unstructured.Unstructured{folderFixture("f1", "", "")},
			dashboards: []unstructured.Unstructured{dashboardFixture("uid-1", "", "f1")},
			want:       []string{"f1/", "f1/uid-1"},
		},
		{
			name:       "dashboard in unknown folder is kept under its folder uid",
			dashboards: []unstructured.Unstructured{dashboardFixture("d1", "Hidden", "secret-folder")},
			want:       []string{"secret-folder/", "secret-folder/Hidden"},
		},
		{
			name:    "folder with unknown parent attaches to root",
			folders: []unstructured.Unstructured{folderFixture("f1", "Orphan", "gone")},
			want:    []string{"Orphan/"},
		},
		{
			name:    "folder cycle attaches to root",
			folders: []unstructured.Unstructured{folderFixture("a", "A", "b"), folderFixture("b", "B", "a")},
			want:    []string{"A/", "B/"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root, _ := buildDashboardTree(tc.dashboards, tc.folders)
			assert.Equal(t, tc.want, titles(root, ""))
		})
	}
}

func TestBuildDashboardTreeMetadata(t *testing.T) {
	dashboards, folders := sampleTree()
	dashboards = append(dashboards, dashboardFixture("lost", "Lost", "missing"))
	root, index := buildDashboardTree(dashboards, folders)

	assert.Equal(t, "Infrastructure/Ceph", index["ceph"].path)
	assert.Equal(t, fixtureTime(3), index["ceph"].Created)
	assert.True(t, index["missing"].Unreadable)
	assert.False(t, index["ceph"].Unreadable)

	ha := root.Folders[0].Dashboards[0]
	assert.Equal(t, treeDashboard{Name: "ha", Title: "Home Assistant", Created: fixtureTime(6), Updated: fixtureTime(20)}, ha)
}

func TestObjectTimes(t *testing.T) {
	created := time.Date(2026, 3, 1, 10, 0, 0, 0, time.FixedZone("CET", 3600))
	managed := time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC)

	obj := dashboardFixture("d", "D", "")
	obj.SetCreationTimestamp(metav1.NewTime(created))
	obj.SetManagedFields([]metav1.ManagedFieldsEntry{{Time: &metav1.Time{Time: managed}}})

	gotCreated, gotUpdated := objectTimes(obj)
	assert.Equal(t, created.UTC(), gotCreated)
	assert.Equal(t, time.UTC, gotCreated.Location())
	assert.Equal(t, managed, gotUpdated, "newest managed-fields time is the update time")

	withAnnotation := withTimes(obj, created, fixtureTime(20))
	_, gotUpdated = objectTimes(withAnnotation)
	assert.Equal(t, fixtureTime(20), gotUpdated, "updatedTimestamp annotation wins")

	gotCreated, gotUpdated = objectTimes(dashboardFixture("d", "D", ""))
	assert.True(t, gotCreated.IsZero())
	assert.True(t, gotUpdated.IsZero())
}

func TestFindFolder(t *testing.T) {
	folders := []unstructured.Unstructured{
		folderFixture("infra", "Infrastructure", ""),
		folderFixture("ceph", "Ceph", "infra"),
		folderFixture("slash", "Static / Pages", ""),
		folderFixture("slash-child", "Child", "slash"),
		folderFixture("dup-1", "Shared", ""),
		folderFixture("dup-2", "Shared", "infra"),
		folderFixture("case", "Mixed Case", "infra"),
	}
	root, index := buildDashboardTree(nil, folders)

	tests := []struct {
		arg  string
		want string
	}{
		{arg: "ceph", want: "ceph"},
		{arg: "Infrastructure/Ceph", want: "ceph"},
		{arg: "/Infrastructure/Ceph/", want: "ceph"},
		{arg: "infrastructure/ceph", want: "ceph"},
		{arg: "Static / Pages/Child", want: "slash-child"},
		{arg: "Mixed Case", want: "case"},
		{arg: "mixed case", want: "case"},
		{arg: "Infrastructure/Shared", want: "dup-2"},
		{arg: "Shared", want: "dup-1"},
		{arg: "Child", want: "slash-child"},
		{arg: "Nope"},
		{arg: "/"},
	}
	for _, tc := range tests {
		t.Run(tc.arg, func(t *testing.T) {
			node, ambiguous := findFolder(root, index, tc.arg)
			assert.Empty(t, ambiguous)
			if tc.want == "" {
				assert.Nil(t, node)
				return
			}
			require.NotNil(t, node)
			assert.Equal(t, tc.want, node.Name)
		})
	}
}

func TestFindFolderAmbiguousTitle(t *testing.T) {
	folders := []unstructured.Unstructured{
		folderFixture("a", "Team A", ""),
		folderFixture("b", "Team B", ""),
		folderFixture("a-dash", "Dashboards", "a"),
		folderFixture("b-dash", "Dashboards", "b"),
	}
	root, index := buildDashboardTree(nil, folders)

	node, ambiguous := findFolder(root, index, "dashboards")
	assert.Nil(t, node)
	assert.Equal(t, []string{"Team A/Dashboards", "Team B/Dashboards"}, ambiguous)
}

func mustGlob(t *testing.T, pattern string, ignoreCase bool) *globMatcher {
	t.Helper()
	m, err := newGlobMatcher(pattern, ignoreCase)
	require.NoError(t, err)
	return m
}

func TestTreeSelectionFilter(t *testing.T) {
	dashboards, folders := sampleTree()
	root, _ := buildDashboardTree(dashboards, folders)

	tests := []struct {
		name string
		sel  treeSelection
		want []string
	}{
		{
			name: "no options keeps everything",
			want: []string{
				"Applications/", "Applications/Home Assistant", "Applications/Static Pages / Overview",
				"Infrastructure/", "Infrastructure/Ceph/", "Infrastructure/Ceph/Ceph - Cluster", "Infrastructure/Ceph/OSD Overview",
				"Infrastructure/Raspberry Pi/", "Infrastructure/Raspberry Pi/Raspberry Pi Fleet", "Infrastructure/Tailscale",
				"Performance Testing/", "Incident Insights",
			},
		},
		{
			name: "folders only",
			sel:  treeSelection{foldersOnly: true},
			want: []string{"Applications/", "Infrastructure/", "Infrastructure/Ceph/", "Infrastructure/Raspberry Pi/", "Performance Testing/"},
		},
		{
			name: "level 1",
			sel:  treeSelection{level: 1},
			want: []string{"Applications/", "Infrastructure/", "Performance Testing/", "Incident Insights"},
		},
		{
			name: "level 2 with folders only",
			sel:  treeSelection{level: 2, foldersOnly: true},
			want: []string{"Applications/", "Infrastructure/", "Infrastructure/Ceph/", "Infrastructure/Raspberry Pi/", "Performance Testing/"},
		},
		{
			name: "include keeps folders",
			sel:  treeSelection{include: mustGlob(t, "*Overview", false)},
			want: []string{
				"Applications/", "Applications/Static Pages / Overview",
				"Infrastructure/", "Infrastructure/Ceph/", "Infrastructure/Ceph/OSD Overview", "Infrastructure/Raspberry Pi/",
				"Performance Testing/",
			},
		},
		{
			name: "include with prune",
			sel:  treeSelection{include: mustGlob(t, "*Overview", false), prune: true},
			want: []string{"Applications/", "Applications/Static Pages / Overview", "Infrastructure/", "Infrastructure/Ceph/", "Infrastructure/Ceph/OSD Overview"},
		},
		{
			name: "exclude",
			sel:  treeSelection{exclude: mustGlob(t, "Ceph*|OSD*|Raspberry*", false), prune: true},
			want: []string{"Applications/", "Applications/Home Assistant", "Applications/Static Pages / Overview", "Infrastructure/", "Infrastructure/Tailscale", "Incident Insights"},
		},
		{
			name: "match folders includes a matching folder's contents",
			sel:  treeSelection{include: mustGlob(t, "ceph", true), matchFolders: true, prune: true},
			want: []string{"Infrastructure/", "Infrastructure/Ceph/", "Infrastructure/Ceph/Ceph - Cluster", "Infrastructure/Ceph/OSD Overview"},
		},
		{
			name: "match folders excludes a matching folder",
			sel:  treeSelection{exclude: mustGlob(t, "Infrastructure", false), matchFolders: true},
			want: []string{"Applications/", "Applications/Home Assistant", "Applications/Static Pages / Overview", "Performance Testing/", "Incident Insights"},
		},
		{
			name: "prune keeps folders cut off by level",
			sel:  treeSelection{level: 1, prune: true},
			want: []string{"Applications/", "Infrastructure/", "Incident Insights"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, titles(tc.sel.filter(root, 1, false), ""))
		})
	}
}

func TestTreeSelectionFilterMarksTruncated(t *testing.T) {
	dashboards, folders := sampleTree()
	root, _ := buildDashboardTree(dashboards, folders)

	filtered := treeSelection{level: 1}.filter(root, 1, false)
	byTitle := map[string]*treeFolder{}
	for _, f := range filtered.Folders {
		byTitle[f.Title] = f
	}
	assert.True(t, byTitle["Infrastructure"].Truncated)
	assert.False(t, byTitle["Performance Testing"].Truncated, "an empty folder is not truncated")
	assert.Len(t, root.Folders[1].Folders, 2, "filter must not modify the source tree")
}

func TestTreeSelectionFilterTruncatedFoldersOnly(t *testing.T) {
	dashboards, folders := sampleTree()
	root, _ := buildDashboardTree(dashboards, folders)

	filtered := treeSelection{level: 1, foldersOnly: true}.filter(root, 1, false)
	byTitle := map[string]*treeFolder{}
	for _, f := range filtered.Folders {
		byTitle[f.Title] = f
	}
	assert.False(t, byTitle["Applications"].Truncated, "with -d, a folder holding only dashboards hides nothing")
	assert.True(t, byTitle["Infrastructure"].Truncated)
}

func TestSortTree(t *testing.T) {
	folders := []unstructured.Unstructured{
		withTimes(folderFixture("f10", "Folder 10", ""), fixtureTime(1), fixtureTime(9)),
		withTimes(folderFixture("f9", "folder 9", ""), fixtureTime(2), fixtureTime(8)),
	}
	dashboards := []unstructured.Unstructured{
		withTimes(dashboardFixture("b", "beta", ""), fixtureTime(3), fixtureTime(3)),
		withTimes(dashboardFixture("a", "Alpha", ""), fixtureTime(4), fixtureTime(1)),
		withTimes(dashboardFixture("c", "Gamma", ""), fixtureTime(1), fixtureTime(5)),
	}

	tests := []struct {
		key     treeSortKey
		reverse bool
		want    []string
	}{
		{key: sortByName, want: []string{"Folder 10/", "folder 9/", "Alpha", "beta", "Gamma"}},
		{key: sortByVersion, want: []string{"folder 9/", "Folder 10/", "Alpha", "beta", "Gamma"}},
		{key: sortByMtime, want: []string{"folder 9/", "Folder 10/", "Alpha", "beta", "Gamma"}},
		{key: sortByCtime, want: []string{"Folder 10/", "folder 9/", "Gamma", "beta", "Alpha"}},
		{key: sortByNone, want: []string{"Folder 10/", "folder 9/", "beta", "Alpha", "Gamma"}},
		{key: sortByName, reverse: true, want: []string{"folder 9/", "Folder 10/", "Gamma", "beta", "Alpha"}},
	}
	for _, tc := range tests {
		name := string(tc.key)
		if tc.reverse {
			name += " reversed"
		}
		t.Run(name, func(t *testing.T) {
			root, _ := buildDashboardTree(dashboards, folders)
			sortTree(root, tc.key, tc.reverse)
			assert.Equal(t, tc.want, titles(root, ""))
		})
	}
}

func newTreeOptsForTest(t *testing.T, args ...string) *treeOpts {
	t.Helper()
	o := &treeOpts{}
	flags := pflag.NewFlagSet("tree", pflag.ContinueOnError)
	o.setup(flags)
	require.NoError(t, flags.Parse(args))
	return o
}

func TestTreeOptsValidate(t *testing.T) {
	testutils.SetAgentMode(t, false)

	tests := []struct {
		name    string
		args    []string
		wantErr string
		check   func(t *testing.T, o *treeOpts)
	}{
		{name: "defaults", check: func(t *testing.T, o *treeOpts) {
			t.Helper()
			assert.Equal(t, sortByName, o.sortKey)
			assert.Equal(t, "text", o.IO.OutputFormat)
			assert.Equal(t, defaultTreeTimeFormat, o.render.timeFormat)
		}},
		{name: "sort version", args: []string{"--sort", "version"}, check: func(t *testing.T, o *treeOpts) {
			t.Helper()
			assert.Equal(t, sortByVersion, o.sortKey)
		}},
		{name: "-t sorts by mtime", args: []string{"-t"}, check: func(t *testing.T, o *treeOpts) {
			t.Helper()
			assert.Equal(t, sortByMtime, o.sortKey)
			assert.False(t, o.render.dateCreated)
		}},
		{name: "-c sorts by ctime and dates show creation", args: []string{"-cD"}, check: func(t *testing.T, o *treeOpts) {
			t.Helper()
			assert.Equal(t, sortByCtime, o.sortKey)
			assert.True(t, o.render.dateCreated)
			assert.True(t, o.render.showDate)
		}},
		{name: "-U keeps server order", args: []string{"-U"}, check: func(t *testing.T, o *treeOpts) {
			t.Helper()
			assert.Equal(t, sortByNone, o.sortKey)
		}},
		{name: "folders only reaches the report", args: []string{"-d"}, check: func(t *testing.T, o *treeOpts) {
			t.Helper()
			assert.True(t, o.render.foldersOnly)
			assert.True(t, o.selection.foldersOnly)
		}},
		{name: "patterns compile", args: []string{"-P", "a*", "-I", "b?", "--ignore-case"}, check: func(t *testing.T, o *treeOpts) {
			t.Helper()
			assert.True(t, o.selection.include.matches("ABC"))
			assert.True(t, o.selection.exclude.matches("BX"))
		}},
		{name: "negative level", args: []string{"-L", "-1"}, wantErr: "--level must be >= 0"},
		{name: "negative file limit", args: []string{"--file-limit", "-1"}, wantErr: "--file-limit must be >= 0"},
		{name: "unknown sort", args: []string{"--sort", "size"}, wantErr: "--sort must be one of"},
		{name: "empty time format", args: []string{"--time-format", ""}, wantErr: "--time-format must not be empty"},
		{name: "bad pattern", args: []string{"-P", "[abc"}, wantErr: "--pattern: invalid pattern"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := newTreeOptsForTest(t, tc.args...)
			err := o.Validate()
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			tc.check(t, o)
		})
	}
}

func TestTreeCommandMutuallyExclusiveFlags(t *testing.T) {
	testutils.SetAgentMode(t, false)

	for _, args := range [][]string{
		{"-t", "-c"},
		{"--sort", "name", "-U"},
		{"-C", "-n"},
	} {
		cmd := newTreeCommand(nil)
		cmd.SetArgs(args)
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		err := cmd.Execute()
		require.ErrorContains(t, err, "were all set", "args %v", args)
	}
}

func TestAssembleDashboardTree(t *testing.T) {
	testutils.SetAgentMode(t, false)
	dashboards, folders := sampleTree()

	t.Run("whole instance", func(t *testing.T) {
		o := newTreeOptsForTest(t, "-L", "1")
		require.NoError(t, o.Validate())
		tree, err := assembleDashboardTree(dashboards, folders, nil, o)
		require.NoError(t, err)
		assert.False(t, tree.selected)
		assert.Len(t, tree.Folders, 3)
		assert.Len(t, tree.Dashboards, 1)
	})

	t.Run("selected folders keep argument order", func(t *testing.T) {
		o := newTreeOptsForTest(t)
		require.NoError(t, o.Validate())
		tree, err := assembleDashboardTree(dashboards, folders, []string{"ceph", "Applications"}, o)
		require.NoError(t, err)
		assert.True(t, tree.selected)
		require.Len(t, tree.Folders, 2)
		assert.Equal(t, "Infrastructure/Ceph", tree.Folders[0].path)
		assert.Equal(t, "Applications", tree.Folders[1].path)
		assert.Empty(t, tree.Dashboards)
	})

	t.Run("unknown folder", func(t *testing.T) {
		o := newTreeOptsForTest(t)
		require.NoError(t, o.Validate())
		_, err := assembleDashboardTree(dashboards, folders, []string{"infra"}, o)
		require.NoError(t, err, "a UID resolves")

		_, err = assembleDashboardTree(dashboards, folders, []string{"structure"}, o)
		var detailed gcxerrors.DetailedError
		require.ErrorAs(t, err, &detailed)
		assert.Equal(t, `Folder "structure" not found`, detailed.Summary)
		assert.Contains(t, detailed.Suggestions, `gcx dashboards tree "Infrastructure"`)
	})

	t.Run("ambiguous folder title", func(t *testing.T) {
		o := newTreeOptsForTest(t)
		require.NoError(t, o.Validate())
		dup := append(append([]unstructured.Unstructured{}, folders...), folderFixture("ceph-2", "Ceph", "apps"))
		_, err := assembleDashboardTree(dashboards, dup, []string{"Ceph"}, o)
		var detailed gcxerrors.DetailedError
		require.ErrorAs(t, err, &detailed)
		assert.Contains(t, detailed.Summary, "matches 2 folders")
		assert.Equal(t, []string{`gcx dashboards tree "Applications/Ceph"`, `gcx dashboards tree "Infrastructure/Ceph"`}, detailed.Suggestions)
	})
}
