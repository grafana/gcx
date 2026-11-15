package observation_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/discovery"
	"github.com/grafana/gcx/internal/resources/observation"
	"github.com/grafana/gcx/internal/resources/remote"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func dashboardDesc() resources.Descriptor {
	return resources.Descriptor{
		GroupVersion: schema.GroupVersion{Group: "dashboard.grafana.app", Version: "v1beta1"},
		Kind:         "Dashboard", Singular: "dashboard", Plural: "dashboards",
	}
}

func datasourceDesc() resources.Descriptor {
	return resources.Descriptor{
		GroupVersion: schema.GroupVersion{Group: "datasource.grafana.app", Version: "v0alpha1"},
		Kind:         "DataSource", Singular: "datasource", Plural: "datasources",
	}
}

func dashboard(name, title string, version int, panels ...any) map[string]any {
	if panels == nil {
		panels = []any{}
	}
	return map[string]any{
		"apiVersion": "dashboard.grafana.app/v1beta1",
		"kind":       "Dashboard",
		"metadata": map[string]any{
			"name":              name,
			"namespace":         "stacks-1",
			"uid":               "uid-" + name,
			"resourceVersion":   "123",
			"generation":        float64(version),
			"creationTimestamp": "2026-01-01T00:00:00Z",
			"annotations": map[string]any{
				"grafana.app/updatedTimestamp": "2026-01-02T00:00:00Z",
				"grafana.app/updatedBy":        "user:1",
				"grafana.app/folder":           "team",
			},
		},
		"spec": map[string]any{
			"title":   title,
			"version": float64(version),
			"id":      float64(7),
			"panels":  panels,
		},
		"status": map[string]any{"conversion": map[string]any{"failed": false}},
	}
}

func TestNormalize(t *testing.T) {
	tests := []struct {
		name    string
		typeKey string
		in      map[string]any
		want    map[string]any
	}{
		{
			name:    "dashboard drops incidental metadata, revision fields and status",
			typeKey: observation.TypeDashboards,
			in:      dashboard("a", "A", 3),
			want: map[string]any{
				"apiVersion": "dashboard.grafana.app/v1beta1",
				"kind":       "Dashboard",
				"metadata": map[string]any{
					"name":        "a",
					"annotations": map[string]any{"grafana.app/folder": "team"},
				},
				"spec": map[string]any{"title": "A", "panels": []any{}},
			},
		},
		{
			name:    "datasource secure values are redacted and names kept",
			typeKey: observation.TypeDatasources,
			in: map[string]any{
				"apiVersion": "prometheus.datasource.grafana.app/v0alpha1",
				"kind":       "DataSource",
				"metadata":   map[string]any{"name": "prom", "labels": map[string]any{"grafana.app/deprecatedInternalID": "4"}},
				"secure": map[string]any{
					"basicAuthPassword": map[string]any{"name": "ref-1"},
					"token":             map[string]any{"create": "s3cr3t"},
				},
				"spec": map[string]any{"title": "Prom", "version": float64(2)},
			},
			want: map[string]any{
				"apiVersion": "prometheus.datasource.grafana.app/v0alpha1",
				"kind":       "DataSource",
				"metadata":   map[string]any{"name": "prom"},
				"secure": map[string]any{
					"basicAuthPassword": map[string]any{"redacted": true},
					"token":             map[string]any{"redacted": true},
				},
				// version is only incidental for dashboards.
				"spec": map[string]any{"title": "Prom", "version": float64(2)},
			},
		},
		{
			name:    "folder keeps its parent and drops the derived path annotations",
			typeKey: observation.TypeFolders,
			in: map[string]any{
				"apiVersion": "folder.grafana.app/v1beta1",
				"kind":       "Folder",
				"metadata": map[string]any{"name": "child", "annotations": map[string]any{
					"grafana.app/folder":       "parent",
					"grafana.app/fullpath":     "Parent/Child",
					"grafana.app/fullpathUIDs": "parent/child",
				}},
				"spec": map[string]any{"title": "Child"},
			},
			want: map[string]any{
				"apiVersion": "folder.grafana.app/v1beta1",
				"kind":       "Folder",
				"metadata":   map[string]any{"name": "child", "annotations": map[string]any{"grafana.app/folder": "parent"}},
				"spec":       map[string]any{"title": "Child"},
			},
		},
		{
			name:    "unexpected secure shape is redacted as a whole",
			typeKey: observation.TypeDatasources,
			in:      map[string]any{"metadata": map[string]any{"name": "x"}, "secure": "plain"},
			want:    map[string]any{"metadata": map[string]any{"name": "x"}, "secure": map[string]any{"redacted": true}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := observation.Normalize(tc.typeKey, tc.in)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestNormalize_DoesNotMutateInput(t *testing.T) {
	in := dashboard("a", "A", 1)
	_, err := observation.Normalize(observation.TypeDashboards, in)
	require.NoError(t, err)
	assert.Contains(t, in["spec"], "version")
	assert.Contains(t, in, "status")
}

// fakePuller returns canned results per type key.
// A list request returns items; a request for named resources returns the
// matching entries of got, or of items when got has no entry for the type.
type fakePuller struct {
	items     map[string][]map[string]any
	got       map[string][]map[string]any
	getErr    map[string]error
	failures  map[string]error
	truncated map[string]bool
	err       map[string]error

	mu       sync.Mutex
	requests []resources.FilterType
}

func (f *fakePuller) Pull(_ context.Context, req remote.PullRequest) (*remote.OperationSummary, error) {
	summary := &remote.OperationSummary{}
	filter := req.Filters[0]
	key := observation.TypeKey(filter.Descriptor)
	f.mu.Lock()
	f.requests = append(f.requests, filter.Type)
	f.mu.Unlock()

	if filter.Type == resources.FilterTypeMultiple {
		if err := f.getErr[key]; err != nil {
			summary.RecordFailure(nil, err)
			return summary, nil
		}
		source, ok := f.got[key]
		if !ok {
			source = f.items[key]
		}
		req.Resources.Clear()
		for _, item := range source {
			name, _ := item["metadata"].(map[string]any)["name"].(string)
			if !slices.Contains(filter.ResourceUIDs, name) {
				continue
			}
			u := unstructured.Unstructured{Object: item}
			res, err := resources.FromUnstructured(&u)
			if err != nil {
				return nil, err
			}
			req.Resources.Add(res)
			summary.RecordSuccess()
		}
		return summary, nil
	}

	if err := f.err[key]; err != nil {
		return summary, err
	}
	req.Resources.Clear()
	for _, item := range f.items[key] {
		u := unstructured.Unstructured{Object: item}
		res, err := resources.FromUnstructured(&u)
		if err != nil {
			return nil, err
		}
		req.Resources.Add(res)
		summary.RecordSuccess()
	}
	if err := f.failures[key]; err != nil {
		summary.RecordFailure(nil, err)
	}
	if f.truncated[key] {
		summary.RecordTruncated()
	}
	return summary, nil
}

func selections() []observation.Selection {
	return []observation.Selection{
		{Selector: "dashboards", Type: observation.TypeDashboards, Filter: resources.Filter{Type: resources.FilterTypeAll, Descriptor: dashboardDesc()}},
	}
}

func collect(t *testing.T, p *fakePuller, sels []observation.Selection) *observation.Capture {
	t.Helper()
	c, err := observation.Collect(t.Context(), observation.CollectRequest{
		Stack:      observation.StackIdentity{Server: "https://example.grafana.net", Namespace: "stacks-1"},
		Selectors:  []string{"dashboards"},
		Selections: sels,
		Puller:     p,
		Now:        func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) },
	})
	require.NoError(t, err)
	return c
}

func TestCollect_Coverage(t *testing.T) {
	tests := []struct {
		name        string
		puller      *fakePuller
		sels        []observation.Selection
		wantStatus  string
		wantCount   int
		wantReasons int
	}{
		{
			name:       "complete read",
			puller:     &fakePuller{items: map[string][]map[string]any{observation.TypeDashboards: {dashboard("a", "A", 1)}}},
			sels:       selections(),
			wantStatus: observation.CoverageComplete,
			wantCount:  1,
		},
		{
			name: "failed item read is incomplete",
			puller: &fakePuller{
				items:    map[string][]map[string]any{observation.TypeDashboards: {dashboard("a", "A", 1)}},
				failures: map[string]error{observation.TypeDashboards: errors.New("forbidden")},
			},
			sels:        selections(),
			wantStatus:  observation.CoverageIncomplete,
			wantCount:   1,
			wantReasons: 1,
		},
		{
			name:        "truncated list is incomplete",
			puller:      &fakePuller{truncated: map[string]bool{observation.TypeDashboards: true}},
			sels:        selections(),
			wantStatus:  observation.CoverageIncomplete,
			wantReasons: 1,
		},
		{
			name:        "pull error is incomplete and keeps no objects",
			puller:      &fakePuller{err: map[string]error{observation.TypeDashboards: errors.New("403 forbidden")}},
			sels:        selections(),
			wantStatus:  observation.CoverageIncomplete,
			wantReasons: 1,
		},
		{
			name:        "unresolved type is unavailable",
			puller:      &fakePuller{},
			sels:        []observation.Selection{{Selector: "folders", Type: "folders", Err: errors.New("not found")}},
			wantStatus:  observation.CoverageUnavailable,
			wantReasons: 1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := collect(t, tc.puller, tc.sels)
			require.Len(t, c.Manifest.Types, 1)
			cov := c.Manifest.Types[0]
			assert.Equal(t, tc.wantStatus, cov.Status)
			assert.Equal(t, tc.wantCount, cov.Count)
			assert.Len(t, cov.Reasons, tc.wantReasons)
		})
	}
}

func TestCollect_CancelledWritesNothing(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := observation.Collect(ctx, observation.CollectRequest{Selections: selections(), Puller: &fakePuller{}})
	require.ErrorIs(t, err, context.Canceled)
}

func TestWriteLoad_RoundTrip(t *testing.T) {
	c := collect(t, &fakePuller{items: map[string][]map[string]any{observation.TypeDashboards: {dashboard("a", "A", 1), dashboard("b/c", "B", 1)}}}, selections())
	dest := filepath.Join(t.TempDir(), "before")
	require.NoError(t, observation.Write(c, dest))
	require.Len(t, c.Manifest.Inventory, 2)

	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(dest, observation.ManifestFile))
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}

	loaded, err := observation.Load(dest)
	require.NoError(t, err)
	assert.Equal(t, c.Objects, loaded.Objects)
	assert.Equal(t, c.Manifest.Types, loaded.Manifest.Types)

	// No temporary directory is left next to the destination.
	entries, err := os.ReadDir(filepath.Dir(dest))
	require.NoError(t, err)
	assert.Len(t, entries, 1)
}

func TestWrite_RejectsNonEmptyDestination(t *testing.T) {
	dest := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dest, "stale.json"), []byte("{}"), 0o600))
	err := observation.Write(collect(t, &fakePuller{}, selections()), dest)
	require.ErrorIs(t, err, observation.ErrDestinationNotEmpty)
}

func TestWrite_EmptyDestinationIsReplaced(t *testing.T) {
	dest := t.TempDir()
	require.NoError(t, observation.Write(collect(t, &fakePuller{}, selections()), dest))
	_, err := observation.Load(dest)
	require.NoError(t, err)
}

func TestLoad_RejectsUnfinishedOrAlteredCaptures(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, dir string)
	}{
		{
			name: "missing manifest",
			mutate: func(t *testing.T, dir string) {
				t.Helper()
				require.NoError(t, os.Remove(filepath.Join(dir, observation.ManifestFile)))
			},
		},
		{
			name: "stale extra file",
			mutate: func(t *testing.T, dir string) {
				t.Helper()
				require.NoError(t, os.WriteFile(filepath.Join(dir, observation.TypeDashboards, "old.json"), []byte("{}"), 0o600))
			},
		},
		{
			name: "modified file",
			mutate: func(t *testing.T, dir string) {
				t.Helper()
				require.NoError(t, os.WriteFile(filepath.Join(dir, observation.TypeDashboards, "a.json"), []byte("{}\n"), 0o600))
			},
		},
		{
			name: "missing file",
			mutate: func(t *testing.T, dir string) {
				t.Helper()
				require.NoError(t, os.Remove(filepath.Join(dir, observation.TypeDashboards, "a.json")))
			},
		},
		{
			name: "unknown schema version",
			mutate: func(t *testing.T, dir string) {
				t.Helper()
				p := filepath.Join(dir, observation.ManifestFile)
				data, err := os.ReadFile(p)
				require.NoError(t, err)
				var m map[string]any
				require.NoError(t, json.Unmarshal(data, &m))
				m["schema_version"] = "99"
				data, err = json.Marshal(m)
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(p, data, 0o600))
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "c")
			c := collect(t, &fakePuller{items: map[string][]map[string]any{observation.TypeDashboards: {dashboard("a", "A", 1)}}}, selections())
			require.NoError(t, observation.Write(c, dir))
			tc.mutate(t, dir)
			_, err := observation.Load(dir)
			var invalid *observation.InvalidCaptureError
			require.ErrorAs(t, err, &invalid)
		})
	}
}

func items(objs ...map[string]any) *fakePuller {
	return &fakePuller{items: map[string][]map[string]any{observation.TypeDashboards: objs}}
}

func compare(t *testing.T, before, after *observation.Capture) *observation.DiffResult {
	t.Helper()
	res, err := observation.Compare(before, after, "before", "after")
	require.NoError(t, err)
	return res
}

func TestCompare_Scenarios(t *testing.T) {
	panel := func(expr string) any {
		return map[string]any{"type": "timeseries", "targets": []any{map[string]any{"expr": expr}}}
	}

	t.Run("dashboard creation ignores incidental revision fields", func(t *testing.T) {
		before := collect(t, items(dashboard("a", "A", 1)), selections())
		after := collect(t, items(dashboard("a", "A", 5), dashboard("new", "New", 1)), selections())
		res := compare(t, before, after)
		assert.True(t, res.Complete)
		assert.Equal(t, observation.DiffSummary{Added: 1, Unchanged: 1}, res.Summary)
		require.Len(t, res.Changes, 1)
		assert.Equal(t, observation.ChangeAdded, res.Changes[0].Change)
		assert.Equal(t, "new", res.Changes[0].Name)
		spec, ok := res.Changes[0].Object["spec"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "New", spec["title"])
	})

	t.Run("dashboard query modification is reported at its path", func(t *testing.T) {
		before := collect(t, items(dashboard("a", "A", 1, panel("up"))), selections())
		after := collect(t, items(dashboard("a", "A", 2, panel("rate(x[5m])"))), selections())
		res := compare(t, before, after)
		require.Len(t, res.Changes, 1)
		change := res.Changes[0]
		assert.Equal(t, observation.ChangeModified, change.Change)
		require.Len(t, change.Fields, 1)
		assert.Equal(t, "/spec/panels/0/targets/0/expr", change.Fields[0].Path)
		assert.Equal(t, observation.FieldChanged, change.Fields[0].Op)
		assert.JSONEq(t, `"up"`, string(change.Fields[0].Before))
		assert.JSONEq(t, `"rate(x[5m])"`, string(change.Fields[0].After))
	})

	t.Run("deletion is reported on complete reads", func(t *testing.T) {
		res := compare(t, collect(t, items(dashboard("a", "A", 1)), selections()), collect(t, items(), selections()))
		assert.Equal(t, observation.DiffSummary{Deleted: 1}, res.Summary)
		assert.True(t, res.Complete)
	})

	t.Run("incomplete after capture makes deletion indeterminate", func(t *testing.T) {
		before := collect(t, items(dashboard("a", "A", 1), dashboard("b", "B", 1)), selections())
		failing := items(dashboard("b", "B", 1))
		failing.failures = map[string]error{observation.TypeDashboards: errors.New("403")}
		res := compare(t, before, collect(t, failing, selections()))
		assert.False(t, res.Complete)
		assert.Equal(t, observation.DiffSummary{Unchanged: 1, Indeterminate: 1}, res.Summary)
		assert.Equal(t, observation.ChangeIndeterminate, res.Changes[0].Change)
		assert.Equal(t, observation.TypePartial, res.Types[0].Status)
	})

	t.Run("incomplete before capture makes addition indeterminate", func(t *testing.T) {
		trunc := items()
		trunc.truncated = map[string]bool{observation.TypeDashboards: true}
		res := compare(t, collect(t, trunc, selections()), collect(t, items(dashboard("a", "A", 1)), selections()))
		assert.Equal(t, observation.DiffSummary{Indeterminate: 1}, res.Summary)
	})

	t.Run("no observed change", func(t *testing.T) {
		res := compare(t, collect(t, items(dashboard("a", "A", 1)), selections()), collect(t, items(dashboard("a", "A", 9)), selections()))
		assert.Equal(t, observation.DiffSummary{Unchanged: 1}, res.Summary)
		assert.Empty(t, res.Changes)
		assert.True(t, res.Complete)
	})

	t.Run("type outside one scope is indeterminate", func(t *testing.T) {
		dsSel := []observation.Selection{{Selector: "datasources", Type: observation.TypeDatasources, Filter: resources.Filter{Type: resources.FilterTypeAll, Descriptor: datasourceDesc()}}}
		res := compare(t, collect(t, items(), selections()), collect(t, &fakePuller{}, dsSel))
		assert.False(t, res.Complete)
		require.Len(t, res.Types, 2)
		for _, tc := range res.Types {
			assert.Equal(t, observation.TypeIndeterminate, tc.Status)
		}
	})
}

func TestCompare_DatasourceSecretIsRedacted(t *testing.T) {
	ds := func(secure map[string]any) map[string]any {
		obj := map[string]any{
			"apiVersion": "prometheus.datasource.grafana.app/v0alpha1",
			"kind":       "DataSource",
			"metadata":   map[string]any{"name": "prom"},
			"spec":       map[string]any{"title": "Prom", "type": "prometheus"},
		}
		if secure != nil {
			obj["secure"] = secure
		}
		return obj
	}
	sels := dsSelections()
	before := collect(t, &fakePuller{}, sels)
	// The list returns a summary without the secure block, as the legacy
	// datasource list does; only the item read carries it.
	after := collect(t, &fakePuller{
		items: map[string][]map[string]any{observation.TypeDatasources: {ds(nil)}},
		got:   map[string][]map[string]any{observation.TypeDatasources: {ds(map[string]any{"basicAuthPassword": map[string]any{"name": "ref"}})}},
	}, sels)

	res := compare(t, before, after)
	require.Len(t, res.Changes, 1)
	assert.Equal(t, map[string]any{"basicAuthPassword": map[string]any{"redacted": true}}, res.Changes[0].Object["secure"])

	data, err := json.Marshal(res)
	require.NoError(t, err)
	assert.NotContains(t, string(data), `"ref"`)
	assert.Contains(t, string(data), "secure values are redacted")
}

func dsSelections() []observation.Selection {
	return []observation.Selection{{Selector: "datasources", Type: observation.TypeDatasources, Filter: resources.Filter{Type: resources.FilterTypeAll, Descriptor: datasourceDesc()}}}
}

func TestCollect_DatasourcesAreReadIndividually(t *testing.T) {
	summary := map[string]any{
		"apiVersion": "prometheus.datasource.grafana.app/v0alpha1", "kind": "DataSource",
		"metadata": map[string]any{"name": "prom"},
		"spec":     map[string]any{"title": "Prom", "type": "prometheus"},
	}
	full := map[string]any{
		"apiVersion": "prometheus.datasource.grafana.app/v0alpha1", "kind": "DataSource",
		"metadata": map[string]any{"name": "prom"},
		"spec":     map[string]any{"title": "Prom", "type": "prometheus", "basicAuthUser": "bob"},
	}

	t.Run("item reads replace the list summaries", func(t *testing.T) {
		p := &fakePuller{
			items: map[string][]map[string]any{observation.TypeDatasources: {summary}},
			got:   map[string][]map[string]any{observation.TypeDatasources: {full}},
		}
		c := collect(t, p, dsSelections())
		assert.Equal(t, []resources.FilterType{resources.FilterTypeAll, resources.FilterTypeMultiple}, p.requests)
		assert.Equal(t, observation.CoverageComplete, c.Manifest.Types[0].Status)
		spec, ok := c.Objects[observation.TypeDatasources]["prom"]["spec"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "bob", spec["basicAuthUser"])
	})

	t.Run("a failed item read makes the type incomplete", func(t *testing.T) {
		p := &fakePuller{
			items:  map[string][]map[string]any{observation.TypeDatasources: {summary}},
			getErr: map[string]error{observation.TypeDatasources: errors.New("prom: not found")},
		}
		c := collect(t, p, dsSelections())
		cov := c.Manifest.Types[0]
		assert.Equal(t, observation.CoverageIncomplete, cov.Status)
		assert.Equal(t, 0, cov.Count)
		assert.Contains(t, cov.Reasons[0], "not found")
	})

	t.Run("dashboards are not read again", func(t *testing.T) {
		p := items(dashboard("a", "A", 1))
		collect(t, p, selections())
		assert.Equal(t, []resources.FilterType{resources.FilterTypeAll}, p.requests)
	})
}

func TestCompare_Incompatible(t *testing.T) {
	a := collect(t, items(), selections())
	b := collect(t, items(), selections())
	b.Manifest.Stack.Namespace = "stacks-2"
	_, err := observation.Compare(a, b, "a", "b")
	var incompatible *observation.IncompatibleError
	require.ErrorAs(t, err, &incompatible)

	c := collect(t, items(), selections())
	c.Manifest.NormalizationVersion = "0"
	_, err = observation.Compare(a, c, "a", "c")
	require.ErrorAs(t, err, &incompatible)
}

func TestCompare_NullIsDistinctFromAbsent(t *testing.T) {
	withNull := dashboard("a", "A", 1)
	spec, ok := withNull["spec"].(map[string]any)
	require.True(t, ok)
	spec["description"] = nil
	res := compare(t, collect(t, items(dashboard("a", "A", 1)), selections()), collect(t, items(withNull), selections()))
	require.Len(t, res.Changes, 1)
	field := res.Changes[0].Fields[0]
	assert.Equal(t, observation.FieldAdded, field.Op)
	assert.Equal(t, "/spec/description", field.Path)
	assert.Equal(t, "null", string(field.After))
	assert.Nil(t, field.Before)
}

func TestServerIdentity(t *testing.T) {
	tests := []struct{ in, want string }{
		{"https://user:pw@example.grafana.net/", "https://example.grafana.net"},
		{"https://example.grafana.net/grafana?x=1", "https://example.grafana.net/grafana"},
		{"localhost:3000", "localhost:3000"},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.want, observation.ServerIdentity(tc.in))
	}
}

type fakeMaker struct {
	filters map[string]resources.Filters
}

func (f fakeMaker) MakeFilters(opts discovery.MakeFiltersOptions) (resources.Filters, error) {
	key := opts.Selectors[0].GroupVersionKind.Resource
	if fs, ok := f.filters[key]; ok {
		return fs, nil
	}
	return nil, errors.New("unknown resource type")
}

func TestResolveSelections(t *testing.T) {
	maker := fakeMaker{filters: map[string]resources.Filters{
		"dashboards":    {{Type: resources.FilterTypeAll, Descriptor: dashboardDesc()}},
		"dashboard":     {{Type: resources.FilterTypeAll, Descriptor: dashboardDesc()}},
		"alertrules":    {{Type: resources.FilterTypeAll, Descriptor: resources.Descriptor{GroupVersion: schema.GroupVersion{Group: "rules.alerting.grafana.app", Version: "v0alpha1"}, Kind: "AlertRule", Plural: "alertrules"}}},
		"datasources":   {{Type: resources.FilterTypeAll, Descriptor: datasourceDesc()}},
		"notavailable!": nil,
	}}

	t.Run("deduplicates and records unresolved supported types", func(t *testing.T) {
		got, err := observation.ResolveSelections(maker, []string{"dashboards", "dashboard", "folders", "folder.folder"}, nil)
		require.NoError(t, err)
		require.Len(t, got, 2)
		assert.Equal(t, observation.TypeDashboards, got[0].Type)
		assert.Equal(t, observation.TypeFolders, got[1].Type)
		assert.Equal(t, "folders", got[1].Selector)
		assert.Error(t, got[1].Err)
	})

	t.Run("rejects unknown types", func(t *testing.T) {
		for _, sel := range []string{"bogus", "folders.example.com"} {
			_, err := observation.ResolveSelections(maker, []string{sel}, nil)
			var unsupported *observation.UnsupportedSelectorError
			require.ErrorAs(t, err, &unsupported, sel)
			assert.Contains(t, unsupported.Reason, "unknown resource type")
		}
	})

	t.Run("rejects unsupported types", func(t *testing.T) {
		_, err := observation.ResolveSelections(maker, []string{"alertrules"}, nil)
		var unsupported *observation.UnsupportedSelectorError
		require.ErrorAs(t, err, &unsupported)
	})

	t.Run("rejects named selectors", func(t *testing.T) {
		_, err := observation.ResolveSelections(maker, []string{"dashboards/foo"}, nil)
		var unsupported *observation.UnsupportedSelectorError
		require.ErrorAs(t, err, &unsupported)
	})
}
