package observation_test

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/observation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func folderDesc() resources.Descriptor {
	return resources.Descriptor{
		GroupVersion: schema.GroupVersion{Group: "folder.grafana.app", Version: "v1beta1"},
		Kind:         "Folder", Singular: "folder", Plural: "folders",
	}
}

func folder(name, title, parent string) map[string]any {
	annotations := map[string]any{
		"grafana.app/fullpath":     title,
		"grafana.app/fullpathUIDs": name,
	}
	if parent != "" {
		annotations["grafana.app/folder"] = parent
		annotations["grafana.app/fullpath"] = parent + "/" + title
		annotations["grafana.app/fullpathUIDs"] = parent + "/" + name
	}
	return map[string]any{
		"apiVersion": "folder.grafana.app/v1beta1",
		"kind":       "Folder",
		"metadata": map[string]any{
			"name":            name,
			"namespace":       "stacks-1",
			"resourceVersion": "9",
			"annotations":     annotations,
		},
		"spec": map[string]any{"title": title},
	}
}

func datasource(plugin, name, title string, secure map[string]any) map[string]any {
	obj := map[string]any{
		"apiVersion": plugin + ".datasource.grafana.app/v0alpha1",
		"kind":       "DataSource",
		"metadata":   map[string]any{"name": name, "namespace": "stacks-1"},
		"spec":       map[string]any{"title": title, "type": plugin, "url": "http://" + plugin + ":3100"},
	}
	if secure != nil {
		obj["secure"] = secure
	}
	return obj
}

func selectionFor(typeKey string, desc resources.Descriptor) observation.Selection {
	return observation.Selection{Selector: desc.Plural, Type: typeKey, Filter: resources.Filter{Type: resources.FilterTypeAll, Descriptor: desc}}
}

// kindFixture builds objects of one supported type for the shared tests.
type kindFixture struct {
	key    string
	sel    observation.Selection
	action string
	// object returns a resource whose spec.title is title.
	object func(name, title string) map[string]any
}

func kindFixtures() []kindFixture {
	return []kindFixture{
		{
			key: observation.TypeFolders, sel: selectionFor(observation.TypeFolders, folderDesc()), action: "folders:read",
			object: func(name, title string) map[string]any { return folder(name, title, "") },
		},
		{
			key: observation.TypeDashboards, sel: selectionFor(observation.TypeDashboards, dashboardDesc()), action: "dashboards:read",
			object: func(name, title string) map[string]any { return dashboard(name, title, 1) },
		},
		{
			key: observation.TypeDatasources, sel: selectionFor(observation.TypeDatasources, datasourceDesc()), action: "datasources:read",
			object: func(name, title string) map[string]any {
				return datasource("prometheus", name, title, map[string]any{"basicAuthPassword": map[string]any{"name": "ref-" + name}})
			},
		},
	}
}

func (k kindFixture) puller(objs ...map[string]any) *fakePuller {
	return &fakePuller{items: map[string][]map[string]any{k.key: objs}}
}

func (k kindFixture) truncated(objs ...map[string]any) *fakePuller {
	p := k.puller(objs...)
	p.truncated = map[string]bool{k.key: true}
	return p
}

func (k kindFixture) collect(t *testing.T, p *fakePuller) *observation.Capture {
	t.Helper()
	return collect(t, p, []observation.Selection{k.sel})
}

func TestCompare_PerType(t *testing.T) {
	for _, k := range kindFixtures() {
		t.Run(k.key, func(t *testing.T) {
			t.Run("added, modified, deleted and unchanged", func(t *testing.T) {
				before := k.collect(t, k.puller(k.object("a", "A"), k.object("b", "B"), k.object("c", "C")))
				after := k.collect(t, k.puller(k.object("a", "A"), k.object("b", "B2"), k.object("d", "D")))
				res := compare(t, before, after)

				assert.True(t, res.Complete)
				assert.Equal(t, observation.DiffSummary{Added: 1, Modified: 1, Deleted: 1, Unchanged: 1}, res.Summary)
				require.Len(t, res.Types, 1)
				assert.Equal(t, observation.TypeCompared, res.Types[0].Status)
				require.Len(t, res.Changes, 3)

				modified, deleted, added := res.Changes[0], res.Changes[1], res.Changes[2]
				assert.Equal(t, [3]string{"b", "c", "d"}, [3]string{modified.Name, deleted.Name, added.Name})
				for _, c := range res.Changes {
					assert.Equal(t, k.key, c.Type)
				}

				assert.Equal(t, observation.ChangeModified, modified.Change)
				assert.Contains(t, fieldPaths(modified), "/spec/title")

				assert.Equal(t, observation.ChangeDeleted, deleted.Change)
				assert.Equal(t, "c", asMap(t, deleted.Object["metadata"])["name"])

				assert.Equal(t, observation.ChangeAdded, added.Change)
				assert.Equal(t, "D", asMap(t, added.Object["spec"])["title"])
			})

			t.Run("incomplete before capture makes an addition indeterminate", func(t *testing.T) {
				before := k.collect(t, k.truncated(k.object("a", "A")))
				after := k.collect(t, k.puller(k.object("a", "A"), k.object("d", "D")))
				res := compare(t, before, after)

				assert.False(t, res.Complete)
				assert.Equal(t, observation.DiffSummary{Unchanged: 1, Indeterminate: 1}, res.Summary)
				assert.Equal(t, observation.TypePartial, res.Types[0].Status)
				assert.Equal(t, []string{"before capture is incomplete"}, res.Types[0].Reasons)
				require.Len(t, res.Changes, 1)
				assert.Equal(t, observation.ChangeIndeterminate, res.Changes[0].Change)
				assert.Contains(t, res.Changes[0].Reason, "incomplete before capture")
				assert.Nil(t, res.Changes[0].Object)
			})

			t.Run("incomplete after capture makes a deletion indeterminate", func(t *testing.T) {
				before := k.collect(t, k.puller(k.object("a", "A"), k.object("c", "C")))
				after := k.collect(t, k.truncated(k.object("a", "A")))
				res := compare(t, before, after)

				assert.False(t, res.Complete)
				assert.Equal(t, observation.DiffSummary{Unchanged: 1, Indeterminate: 1}, res.Summary)
				assert.Equal(t, []string{"after capture is incomplete"}, res.Types[0].Reasons)
				require.Len(t, res.Changes, 1)
				assert.Equal(t, "c", res.Changes[0].Name)
				assert.Contains(t, res.Changes[0].Reason, "deletion cannot be established")
			})

			t.Run("a restricted caller makes the type incomplete", func(t *testing.T) {
				access := fullAccess()
				access.Permissions[k.action] = []string{"folders:uid:team-only"}
				c := collectAs(t, k.puller(k.object("a", "A")), []observation.Selection{k.sel}, access)
				cov := c.Manifest.Types[0]
				assert.Equal(t, observation.ReadScopeRestricted, cov.ReadScope)
				assert.Equal(t, observation.CoverageIncomplete, cov.Status)
				require.Len(t, cov.Reasons, 1)
				assert.Contains(t, cov.Reasons[0], k.action+" is not granted")
			})
		})
	}
}

func TestAccess_ReadScope(t *testing.T) {
	// grants holds, per type, every scope that grants read access on all of
	// its resources. Every other scope must be restricted.
	grants := map[string][]string{
		observation.TypeFolders:     {"*", "folders:*", "folders:uid:*"},
		observation.TypeDashboards:  {"*", "dashboards:*", "dashboards:uid:*", "folders:*", "folders:uid:*"},
		observation.TypeDatasources: {"*", "datasources:*", "datasources:uid:*"},
	}
	restricted := map[string][]string{
		observation.TypeFolders:     {"folders:uid:team", "dashboards:*", "datasources:*"},
		observation.TypeDashboards:  {"dashboards:uid:a", "folders:uid:team", "datasources:*"},
		observation.TypeDatasources: {"datasources:uid:prom", "folders:*", "dashboards:*"},
	}
	actions := map[string]string{
		observation.TypeFolders:     "folders:read",
		observation.TypeDashboards:  "dashboards:read",
		observation.TypeDatasources: "datasources:read",
	}

	for _, typeKey := range observation.SupportedTypes() {
		action := actions[typeKey]
		t.Run(typeKey, func(t *testing.T) {
			for _, scope := range grants[typeKey] {
				got, reason := observation.Access{Permissions: map[string][]string{action: {scope}}}.ReadScope(typeKey)
				assert.Equal(t, observation.ReadScopeAll, got, scope)
				assert.Empty(t, reason, scope)
			}
			for _, scope := range restricted[typeKey] {
				got, reason := observation.Access{Permissions: map[string][]string{action: {scope}}}.ReadScope(typeKey)
				assert.Equal(t, observation.ReadScopeRestricted, got, scope)
				assert.Contains(t, reason, action+" is not granted on ", scope)
			}

			// A wildcard granted for another action does not count.
			got, _ := observation.Access{Permissions: map[string][]string{"other:write": {"*"}}}.ReadScope(typeKey)
			assert.Equal(t, observation.ReadScopeRestricted, got)

			// A specific scope next to a wildcard is still all.
			got, _ = observation.Access{Permissions: map[string][]string{action: {"x:uid:1", grants[typeKey][1]}}}.ReadScope(typeKey)
			assert.Equal(t, observation.ReadScopeAll, got)

			got, reason := observation.Access{PermissionsErr: errors.New("status 403")}.ReadScope(typeKey)
			assert.Equal(t, observation.ReadScopeUnknown, got)
			assert.Contains(t, reason, "status 403")
		})
	}

	t.Run("unsupported type is unknown", func(t *testing.T) {
		got, _ := fullAccess().ReadScope("alertrules.rules.alerting.grafana.app")
		assert.Equal(t, observation.ReadScopeUnknown, got)
	})
}

func asMap(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	require.True(t, ok, "not an object: %v", v)
	return m
}

func fieldPaths(c observation.ResourceChange) []string {
	paths := make([]string, 0, len(c.Fields))
	for _, f := range c.Fields {
		paths = append(paths, f.Path)
	}
	return paths
}

func TestCompare_Folders(t *testing.T) {
	k := kindFixtures()[0]

	t.Run("a move reports only the parent annotation", func(t *testing.T) {
		before := k.collect(t, k.puller(folder("p1", "P1", ""), folder("p2", "P2", ""), folder("child", "Child", "p1")))
		after := k.collect(t, k.puller(folder("p1", "P1", ""), folder("p2", "P2", ""), folder("child", "Child", "p2")))
		res := compare(t, before, after)

		assert.Equal(t, observation.DiffSummary{Modified: 1, Unchanged: 2}, res.Summary)
		require.Len(t, res.Changes, 1)
		assert.Equal(t, "child", res.Changes[0].Name)
		require.Len(t, res.Changes[0].Fields, 1)
		field := res.Changes[0].Fields[0]
		assert.Equal(t, "/metadata/annotations/grafana.app~1folder", field.Path)
		assert.Equal(t, observation.FieldChanged, field.Op)
		assert.JSONEq(t, `"p1"`, string(field.Before))
		assert.JSONEq(t, `"p2"`, string(field.After))
	})

	t.Run("a parent rename does not modify the children", func(t *testing.T) {
		before := k.collect(t, k.puller(folder("p1", "P1", ""), folder("child", "Child", "p1")))
		renamedChild := folder("child", "Child", "p1")
		asMap(t, asMap(t, renamedChild["metadata"])["annotations"])["grafana.app/fullpath"] = "Renamed/Child"
		after := k.collect(t, k.puller(folder("p1", "Renamed", ""), renamedChild))
		res := compare(t, before, after)

		assert.Equal(t, observation.DiffSummary{Modified: 1, Unchanged: 1}, res.Summary)
		require.Len(t, res.Changes, 1)
		assert.Equal(t, "p1", res.Changes[0].Name)
		assert.Equal(t, []string{"/spec/title"}, fieldPaths(res.Changes[0]))
	})
}

func TestCompare_Dashboards(t *testing.T) {
	k := kindFixtures()[1]
	panel := func(expr string) any {
		return map[string]any{"type": "timeseries", "targets": []any{map[string]any{"expr": expr}}}
	}
	v2 := func(panelTitle string) map[string]any {
		return map[string]any{
			"apiVersion": "dashboard.grafana.app/v2beta1",
			"kind":       "Dashboard",
			"metadata":   map[string]any{"name": "v2", "namespace": "stacks-1", "resourceVersion": "4"},
			"spec": map[string]any{
				"title":    "V2",
				"elements": map[string]any{"panel-1": map[string]any{"kind": "Panel", "spec": map[string]any{"title": panelTitle}}},
			},
		}
	}
	withTags := func(obj map[string]any, tags ...any) map[string]any {
		if tags == nil {
			tags = []any{}
		}
		asMap(t, obj["spec"])["tags"] = tags
		return obj
	}

	t.Run("v1beta1 and v2beta1 objects in one capture", func(t *testing.T) {
		before := k.collect(t, k.puller(dashboard("v1", "V1", 1), v2("Old")))
		after := k.collect(t, k.puller(dashboard("v1", "V1", 2), v2("New")))
		require.Len(t, before.Objects[observation.TypeDashboards], 2)
		assert.Equal(t, "dashboard.grafana.app/v2beta1", before.Objects[observation.TypeDashboards]["v2"]["apiVersion"])
		assert.Equal(t, "dashboard.grafana.app/v1beta1", before.Objects[observation.TypeDashboards]["v1"]["apiVersion"])

		res := compare(t, before, after)
		assert.Equal(t, observation.DiffSummary{Modified: 1, Unchanged: 1}, res.Summary)
		require.Len(t, res.Changes, 1)
		assert.Equal(t, "v2", res.Changes[0].Name)
		assert.Equal(t, []string{"/spec/elements/panel-1/spec/title"}, fieldPaths(res.Changes[0]))
	})

	t.Run("an added tag is an added array element", func(t *testing.T) {
		before := k.collect(t, k.puller(withTags(dashboard("a", "A", 1))))
		after := k.collect(t, k.puller(withTags(dashboard("a", "A", 2), "prod")))
		res := compare(t, before, after)
		require.Len(t, res.Changes, 1)
		require.Len(t, res.Changes[0].Fields, 1)
		field := res.Changes[0].Fields[0]
		assert.Equal(t, "/spec/tags/0", field.Path)
		assert.Equal(t, observation.FieldAdded, field.Op)
		assert.JSONEq(t, `"prod"`, string(field.After))
	})

	t.Run("an appended panel is an added array element", func(t *testing.T) {
		before := k.collect(t, k.puller(dashboard("a", "A", 1, panel("up"))))
		after := k.collect(t, k.puller(dashboard("a", "A", 2, panel("up"), panel("rate(x[5m])"))))
		res := compare(t, before, after)
		require.Len(t, res.Changes, 1)
		require.Len(t, res.Changes[0].Fields, 1)
		field := res.Changes[0].Fields[0]
		assert.Equal(t, "/spec/panels/1", field.Path)
		assert.Equal(t, observation.FieldAdded, field.Op)
		assert.JSONEq(t, `{"type":"timeseries","targets":[{"expr":"rate(x[5m])"}]}`, string(field.After))
	})
}

func TestCompare_Datasources(t *testing.T) {
	k := kindFixtures()[2]
	const secretValue = "s3cr3t-value"
	const secretRef = "secret-ref-name"
	password := map[string]any{"name": secretRef}

	t.Run("an added secret key is reported redacted", func(t *testing.T) {
		before := k.collect(t, k.puller(datasource("prometheus", "prom", "Prom", map[string]any{"basicAuthPassword": password})))
		after := k.collect(t, k.puller(datasource("prometheus", "prom", "Prom", map[string]any{
			"basicAuthPassword": password,
			"httpHeaderValue1":  map[string]any{"create": secretValue},
		})))
		res := compare(t, before, after)
		require.Len(t, res.Changes, 1)
		require.Len(t, res.Changes[0].Fields, 1)
		field := res.Changes[0].Fields[0]
		assert.Equal(t, "/secure/httpHeaderValue1", field.Path)
		assert.Equal(t, observation.FieldAdded, field.Op)
		assert.JSONEq(t, `{"redacted":true}`, string(field.After))
	})

	t.Run("a removed secret key is reported redacted", func(t *testing.T) {
		before := k.collect(t, k.puller(datasource("prometheus", "prom", "Prom", map[string]any{
			"basicAuthPassword": password,
			"httpHeaderValue1":  map[string]any{"name": "other-ref"},
		})))
		after := k.collect(t, k.puller(datasource("prometheus", "prom", "Prom", map[string]any{"basicAuthPassword": password})))
		res := compare(t, before, after)
		require.Len(t, res.Changes, 1)
		require.Len(t, res.Changes[0].Fields, 1)
		field := res.Changes[0].Fields[0]
		assert.Equal(t, "/secure/httpHeaderValue1", field.Path)
		assert.Equal(t, observation.FieldRemoved, field.Op)
		assert.JSONEq(t, `{"redacted":true}`, string(field.Before))
	})

	t.Run("a password-only change is not observed", func(t *testing.T) {
		before := k.collect(t, k.puller(datasource("prometheus", "prom", "Prom", map[string]any{"basicAuthPassword": password})))
		after := k.collect(t, k.puller(datasource("prometheus", "prom", "Prom", map[string]any{"basicAuthPassword": map[string]any{"create": secretValue}})))
		res := compare(t, before, after)
		assert.Equal(t, observation.DiffSummary{Unchanged: 1}, res.Summary)
		assert.True(t, res.Complete)
	})

	t.Run("plugin types share one type key", func(t *testing.T) {
		loki := func(url string) map[string]any {
			obj := datasource("loki", "logs", "Logs", nil)
			asMap(t, obj["spec"])["url"] = url
			return obj
		}
		before := k.collect(t, k.puller(datasource("prometheus", "prom", "Prom", nil), loki("http://loki:3100")))
		after := k.collect(t, k.puller(datasource("prometheus", "prom", "Prom", nil), loki("http://loki-2:3100")))

		require.Len(t, before.Manifest.Types, 1)
		assert.Equal(t, 2, before.Manifest.Types[0].Count)
		objs := before.Objects[observation.TypeDatasources]
		assert.Equal(t, "prometheus.datasource.grafana.app/v0alpha1", objs["prom"]["apiVersion"])
		assert.Equal(t, "loki.datasource.grafana.app/v0alpha1", objs["logs"]["apiVersion"])

		res := compare(t, before, after)
		assert.Equal(t, observation.DiffSummary{Modified: 1, Unchanged: 1}, res.Summary)
		require.Len(t, res.Changes, 1)
		assert.Equal(t, "logs", res.Changes[0].Name)
		assert.Equal(t, []string{"/spec/url"}, fieldPaths(res.Changes[0]))
	})

	t.Run("no secret value or reference reaches the result or the files", func(t *testing.T) {
		secure := func() map[string]any {
			return map[string]any{"basicAuthPassword": map[string]any{"name": secretRef}, "token": map[string]any{"create": secretValue}}
		}
		before := k.collect(t, k.puller(
			datasource("prometheus", "kept", "Kept", secure()),
			datasource("prometheus", "gone", "Gone", secure()),
		))
		after := k.collect(t, k.puller(
			datasource("prometheus", "kept", "Kept 2", secure()),
			datasource("loki", "new", "New", secure()),
		))
		res := compare(t, before, after)
		assert.Equal(t, observation.DiffSummary{Added: 1, Modified: 1, Deleted: 1}, res.Summary)

		data, err := json.Marshal(res)
		require.NoError(t, err)
		assert.NotContains(t, string(data), secretValue)
		assert.NotContains(t, string(data), secretRef)

		for name, c := range map[string]*observation.Capture{"before": before, "after": after} {
			dir := filepath.Join(t.TempDir(), name)
			require.NoError(t, observation.Write(c, dir))
			root, err := os.OpenRoot(dir)
			require.NoError(t, err)
			t.Cleanup(func() { _ = root.Close() })
			require.NoError(t, fs.WalkDir(root.FS(), ".", func(path string, d fs.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return err
				}
				content, err := fs.ReadFile(root.FS(), path)
				require.NoError(t, err)
				assert.NotContains(t, string(content), secretValue, path)
				assert.NotContains(t, string(content), secretRef, path)
				return nil
			}))
		}
	})
}

// One type failing and another restricted must not affect the third.
func TestCollectCompare_MultipleTypes(t *testing.T) {
	fx := kindFixtures()
	folders, dashboards, datasources := fx[0], fx[1], fx[2]
	// Deliberately unsorted.
	sels := []observation.Selection{datasources.sel, folders.sel, dashboards.sel}

	allItems := func() map[string][]map[string]any {
		return map[string][]map[string]any{
			folders.key:     {folders.object("f1", "F1"), folders.object("f2", "F2")},
			dashboards.key:  {dashboards.object("d1", "D1")},
			datasources.key: {datasources.object("ds1", "DS1")},
		}
	}
	before := collect(t, &fakePuller{items: allItems()}, sels)
	for _, cov := range before.Manifest.Types {
		require.Equal(t, observation.CoverageComplete, cov.Status, cov.Type)
	}

	afterItems := allItems()
	afterItems[folders.key] = afterItems[folders.key][:1] // f2 deleted
	afterItems[datasources.key] = nil                     // hidden by the restricted scope
	access := fullAccess()
	access.Permissions["datasources:read"] = []string{"datasources:uid:other"}
	after := collectAs(t, &fakePuller{
		items: afterItems,
		err:   map[string]error{dashboards.key: errors.New("503 service unavailable")},
	}, sels, access)

	types := after.Manifest.Types
	require.Len(t, types, 3)
	assert.Equal(t, []string{dashboards.key, datasources.key, folders.key}, []string{types[0].Type, types[1].Type, types[2].Type})

	assert.Equal(t, observation.CoverageIncomplete, types[0].Status)
	assert.Equal(t, observation.ReadScopeAll, types[0].ReadScope)
	require.Len(t, types[0].Reasons, 1)
	assert.Contains(t, types[0].Reasons[0], "503 service unavailable")
	assert.Equal(t, 0, types[0].Count)

	assert.Equal(t, observation.CoverageIncomplete, types[1].Status)
	assert.Equal(t, observation.ReadScopeRestricted, types[1].ReadScope)

	assert.Equal(t, observation.CoverageComplete, types[2].Status)
	assert.Equal(t, observation.ReadScopeAll, types[2].ReadScope)
	assert.Empty(t, types[2].Reasons)
	assert.Equal(t, 1, types[2].Count)

	res := compare(t, before, after)
	assert.False(t, res.Complete)
	require.Len(t, res.Types, 3)
	statuses := map[string]string{}
	for _, tc := range res.Types {
		statuses[tc.Type] = tc.Status
	}
	assert.Equal(t, map[string]string{
		dashboards.key:  observation.TypePartial,
		datasources.key: observation.TypePartial,
		folders.key:     observation.TypeCompared,
	}, statuses)

	changes := map[string]string{}
	for _, c := range res.Changes {
		changes[c.Type+"/"+c.Name] = c.Change
	}
	assert.Equal(t, map[string]string{
		dashboards.key + "/d1":   observation.ChangeIndeterminate,
		datasources.key + "/ds1": observation.ChangeIndeterminate,
		folders.key + "/f2":      observation.ChangeDeleted,
	}, changes)
	assert.Equal(t, observation.DiffSummary{Deleted: 1, Unchanged: 1, Indeterminate: 2}, res.Summary)
}
