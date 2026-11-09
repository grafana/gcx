package alert //nolint:testpackage // Tests exercise private command factories through real native HTTP bindings.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/providers/native"
	"github.com/grafana/gcx/internal/resources/discovery"
	"github.com/grafana/gcx/internal/retry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/yaml"
)

type templateNativeAPI struct {
	deleteStatus  int
	mu            sync.Mutex
	version, path string
	items         map[string]map[string]any
	requests      []string
	body          map[string]any
	status        int
	paginate      bool
}

func templateHTTPJSON(w http.ResponseWriter, status int, value any) {
	body, err := json.Marshal(value)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
func templateHTTPStatus(w http.ResponseWriter, status int, msg string) {
	reason := metav1.StatusReasonUnknown
	switch status {
	case http.StatusConflict:
		reason = metav1.StatusReasonConflict
	case http.StatusNotFound:
		reason = metav1.StatusReasonNotFound
	case http.StatusForbidden:
		reason = metav1.StatusReasonForbidden
	}
	templateHTTPJSON(w, status, metav1.Status{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"}, Status: metav1.StatusFailure, Message: msg, Reason: reason, Code: int32(status)}) //nolint:gosec // HTTP statuses fit int32.
}

//nolint:nestif // Fixture models native title conflicts and optimistic version checks.
func (a *templateNativeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.requests = append(a.requests, r.Method+" "+r.URL.Path)
	if a.status != 0 {
		templateHTTPStatus(w, a.status, http.StatusText(a.status))
		return
	}
	name := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, a.path), "/")
	switch r.Method {
	case http.MethodGet:
		if name != "" {
			obj, ok := a.items[name]
			if !ok {
				templateHTTPStatus(w, 404, "group not found")
				return
			}
			templateHTTPJSON(w, 200, obj)
			return
		}
		names := make([]string, 0, len(a.items))
		for n := range a.items {
			names = append(names, n)
		}
		slices.Sort(names)
		items := []any{}
		metadata := map[string]any{}
		if a.paginate && len(names) > 1 {
			if r.URL.Query().Get("continue") == "" {
				names = names[:1]
				metadata["continue"] = "page-two"
			} else {
				names = names[1:]
			}
		}
		for _, n := range names {
			items = append(items, a.items[n])
		}
		templateHTTPJSON(w, 200, map[string]any{"apiVersion": routingTreesGroup + "/" + a.version, "kind": "TemplateGroupList", "metadata": metadata, "items": items})
	case http.MethodPost, http.MethodPut:
		obj := map[string]any{}
		if err := json.NewDecoder(r.Body).Decode(&obj); err != nil {
			templateHTTPStatus(w, 400, err.Error())
			return
		}
		a.body = runtime.DeepCopyJSON(obj)
		meta, _ := obj["metadata"].(map[string]any)
		spec, _ := obj["spec"].(map[string]any)
		title, _ := spec["title"].(string)
		desiredKind, _ := spec["kind"].(string)
		if desiredKind == "" {
			desiredKind = "grafana"
		}
		for existingName, existing := range a.items {
			es, _ := existing["spec"].(map[string]any)
			existingKind, _ := es["kind"].(string)
			if existingKind == "" {
				existingKind = "grafana"
			}
			if existingName != name && es["title"] == title && existingKind == desiredKind {
				templateHTTPStatus(w, 409, "destination title already exists")
				return
			}
		}
		if r.Method == http.MethodPost {
			if meta["name"] != nil || meta["resourceVersion"] != nil || meta["uid"] != nil {
				templateHTTPStatus(w, 400, "source identity must be omitted")
				return
			}
		} else {
			existing, ok := a.items[name]
			if !ok {
				templateHTTPStatus(w, 404, "group not found")
				return
			}
			current, _ := existing["metadata"].(map[string]any)
			if meta["name"] != name {
				templateHTTPStatus(w, 400, "request name must be current API address")
				return
			}
			if meta["resourceVersion"] != current["resourceVersion"] {
				templateHTTPStatus(w, 409, "the object has been modified")
				return
			}
			delete(a.items, name)
		}
		newName := "server-" + title
		meta["name"] = newName
		meta["resourceVersion"] = "8"
		a.items[newName] = obj
		code := 200
		if r.Method == http.MethodPost {
			code = 201
		}
		templateHTTPJSON(w, code, obj)
	case http.MethodDelete:
		if a.deleteStatus != 0 {
			templateHTTPStatus(w, a.deleteStatus, http.StatusText(a.deleteStatus))
			return
		}
		if _, ok := a.items[name]; !ok {
			templateHTTPStatus(w, 404, "group not found")
			return
		}
		delete(a.items, name)
		templateHTTPJSON(w, 200, map[string]any{"apiVersion": "v1", "kind": "Status", "status": "Success"})
	default:
		templateHTTPStatus(w, 405, "unexpected request")
	}
}

type templateStaticLoader struct{ cfg config.NamespacedRESTConfig }

func (l templateStaticLoader) LoadGrafanaConfig(context.Context) (config.NamespacedRESTConfig, error) {
	return l.cfg, nil
}

type templateStaticDiscovery struct {
	groups    []*metav1.APIGroup
	resources []*metav1.APIResourceList
}

func (d templateStaticDiscovery) ServerGroupsAndResources() ([]*metav1.APIGroup, []*metav1.APIResourceList, error) {
	return d.groups, d.resources, nil
}
func templateAPIBinding(t *testing.T, a *templateNativeAPI) native.Binding {
	t.Helper()
	srv := httptest.NewServer(a)
	t.Cleanup(srv.Close)
	gv := routingTreesGroup + "/" + a.version
	group := &metav1.APIGroup{Name: routingTreesGroup, Versions: []metav1.GroupVersionForDiscovery{{GroupVersion: gv, Version: a.version}}, PreferredVersion: metav1.GroupVersionForDiscovery{GroupVersion: gv, Version: a.version}}
	disco := templateStaticDiscovery{groups: []*metav1.APIGroup{group}, resources: []*metav1.APIResourceList{{GroupVersion: gv, APIResources: []metav1.APIResource{{Name: "templategroups", SingularName: "templategroup", Kind: templateGroupKind, Namespaced: true}}}}}
	cfg := config.NamespacedRESTConfig{Config: rest.Config{Host: srv.URL, WrapTransport: func(rt http.RoundTripper) http.RoundTripper { return &retry.Transport{Base: rt} }}, Namespace: "default"}
	return native.Bind(templateStaticLoader{cfg}, native.Config{Group: routingTreesGroup, Resource: "templategroups"}, native.WithRegistry(func(ctx context.Context, _ config.NamespacedRESTConfig) (*discovery.Registry, error) {
		return discovery.NewCachedRegistry(ctx, disco)
	}))
}
func newTemplateNativeAPI(t *testing.T, version string) *templateNativeAPI {
	t.Helper()
	a := &templateNativeAPI{version: version, path: "/apis/" + routingTreesGroup + "/" + version + "/namespaces/default/templategroups", items: map[string]map[string]any{}}
	for _, f := range []struct{ name, title, kind string }{{"opaque-grafana", "shared", "grafana"}, {"opaque-mimir", "shared", "mimir"}, {"old-api-address", "historical", "absent"}} {
		obj := map[string]any{}
		require.NoError(t, json.Unmarshal([]byte(templateManifest(version, f.name, f.title, f.kind, "7")), &obj))
		a.items[f.name] = obj
	}
	return a
}
func TestTemplateGroupsNativeHTTP(t *testing.T) {
	templateTestMode(t)
	for _, version := range []string{"v0alpha1", "v1beta1"} {
		t.Run(version, func(t *testing.T) {
			tests := []struct {
				name         string
				args         []string
				stdin, title string
				wantRequests []string
				wantErr      string
				status       int
			}{
				{name: "list includes imports", args: []string{"list", "-o", "json"}, wantRequests: []string{"GET"}},
				{name: "get exact title via list", args: []string{"get", "shared", "--kind", "mimir", "-o", "json"}, title: "shared", wantRequests: []string{"GET", "GET opaque-mimir"}},
				{name: "historical kind omitted", args: []string{"get", "historical", "--kind", "grafana", "-o", "json"}, title: "historical", wantRequests: []string{"GET", "GET old-api-address"}},
				{name: "ambiguous", args: []string{"get", "shared"}, wantRequests: []string{"GET"}, wantErr: "--kind"},
				{name: "title case sensitive", args: []string{"get", "Shared"}, wantRequests: []string{"GET"}, wantErr: "not found"},
				{name: "create unnamed", args: []string{"create", "-f", "-", "-o", "json"}, stdin: templateManifest(version, "source-api", "new", "grafana", "1"), title: "new", wantRequests: []string{"POST"}},
				{name: "create historical", args: []string{"create", "-f", "-"}, stdin: templateManifest(version, "source-old", "new", "absent", "1"), wantRequests: []string{"POST"}},
				{name: "create conflict", args: []string{"create", "-f", "-"}, stdin: templateManifest(version, "source-api", "shared", "grafana", "1"), wantRequests: []string{"POST"}, wantErr: "already exists"},
				{name: "content update", args: []string{"update", "shared", "-f", "-", "-o", "json"}, stdin: templateManifest(version, "opaque-grafana", "shared", "grafana", "7"), title: "shared", wantRequests: []string{"GET", "PUT opaque-grafana"}},
				{name: "rename with current name", args: []string{"update", "shared", "-f", "-", "-o", "json"}, stdin: templateManifest(version, "opaque-grafana", "renamed", "grafana", "7"), title: "renamed", wantRequests: []string{"GET", "PUT opaque-grafana"}},
				{name: "rename conflict", args: []string{"update", "historical", "-f", "-"}, stdin: templateManifest(version, "old-api-address", "shared", "absent", "7"), wantRequests: []string{"GET", "PUT old-api-address"}, wantErr: "destination title already exists"},
				{name: "rename with omitted name", args: []string{"update", "shared", "-f", "-", "-o", "json"}, stdin: templateManifest(version, "", "renamed", "grafana", "7"), title: "renamed", wantRequests: []string{"GET", "PUT opaque-grafana"}},
				{name: "conversion import to grafana", args: []string{"update", "shared", "-f", "-"}, stdin: templateManifest(version, "opaque-mimir", "shared", "grafana", "7"), wantRequests: []string{"GET"}, wantErr: "kind cannot change"},
				{name: "conversion grafana to import", args: []string{"update", "shared", "-f", "-"}, stdin: templateManifest(version, "opaque-grafana", "shared", "mimir", "7"), wantRequests: []string{}, wantErr: "read-only"},
				{name: "delete not found", args: []string{"delete", "missing", "--force"}, wantRequests: []string{"GET"}, wantErr: "not found"},
				{name: "wrong API name", args: []string{"update", "shared", "-f", "-"}, stdin: templateManifest(version, "opaque-mimir", "shared", "grafana", "7"), wantRequests: []string{"GET"}, wantErr: "metadata.name"},
				{name: "stale version retained", args: []string{"update", "shared", "-f", "-"}, stdin: templateManifest(version, "opaque-grafana", "shared", "grafana", "1"), wantRequests: []string{"GET", "PUT opaque-grafana"}, wantErr: "reapply edits"},
				{name: "missing update target", args: []string{"update", "missing", "-f", "-"}, stdin: templateManifest(version, "", "new", "grafana", "7"), wantRequests: []string{"GET"}, wantErr: "not found"},
				{name: "delete grafana", args: []string{"delete", "shared", "--kind", "grafana", "--force", "-o", "json"}, title: "shared", wantRequests: []string{"GET", "DELETE opaque-grafana"}},
				{name: "delete imported rejected", args: []string{"delete", "shared", "--kind", "mimir", "--force"}, wantRequests: []string{"GET"}, wantErr: "read-only"},
				{name: "delete ambiguity", args: []string{"delete", "shared", "--force"}, wantRequests: []string{"GET"}, wantErr: "--kind"},
				{name: "HTTP 403", args: []string{"list"}, status: 403, wantRequests: []string{"GET"}, wantErr: "Forbidden"},
				{name: "HTTP 501 no fallback", args: []string{"create", "-f", "-"}, stdin: templateManifest(version, "", "new", "grafana", ""), status: 501, wantRequests: []string{"POST"}, wantErr: "Not Implemented"},
			}
			for _, tc := range tests {
				t.Run(tc.name, func(t *testing.T) {
					a := newTemplateNativeAPI(t, version)
					a.status = tc.status
					res := runTemplateGroups(t, templateAPIBinding(t, a), tc.stdin, tc.args...)
					a.mu.Lock()
					defer a.mu.Unlock()
					if tc.wantErr != "" {
						require.ErrorContains(t, res.err, tc.wantErr)
					} else {
						require.NoError(t, res.err)
					}
					var want []string
					for _, request := range tc.wantRequests {
						parts := strings.SplitN(request, " ", 2)
						path := a.path
						if len(parts) == 2 {
							path += "/" + parts[1]
						}
						want = append(want, parts[0]+" "+path)
					}
					assert.Equal(t, want, a.requests)
					if tc.name == "ambiguous" {
						for _, candidate := range []string{"grafana/opaque-grafana", "mimir/opaque-mimir"} {
							assert.Contains(t, res.err.Error(), candidate)
						}
					}
					if tc.name == "list includes imports" {
						var doc map[string]any
						require.NoError(t, json.Unmarshal([]byte(res.stdout), &doc))
						assert.Len(t, doc["items"], 3)
						assert.NotContains(t, res.stdout, "readOnly")
					}
					if tc.name == "historical kind omitted" {
						var obj map[string]any
						require.NoError(t, json.Unmarshal([]byte(res.stdout), &obj))
						assert.NotContains(t, obj["spec"], "kind")
					}
					if tc.name == "create unnamed" {
						meta := templateMap(t, a.body["metadata"])
						for _, field := range []string{"uid", "generation", "creationTimestamp", "managedFields", "generateName"} {
							assert.NotContains(t, meta, field)
						}
						assert.Contains(t, meta, "annotations")
						assert.Equal(t, "preserve me", templateMap(t, a.body["spec"])["unknownField"])
					}
					if tc.title != "" && tc.args[len(tc.args)-1] == "json" {
						var doc map[string]any
						require.NoError(t, json.Unmarshal([]byte(res.stdout), &doc))
						if tc.args[0] == "get" {
							assert.Equal(t, tc.title, templateMap(t, doc["spec"])["title"])
						} else {
							assert.Equal(t, tc.title, doc["title"])
							assert.NotEmpty(t, templateMap(t, doc["target"])["name"])
							assert.NotEmpty(t, doc["resourceVersion"])
							if tc.title == "renamed" {
								assert.Equal(t, "shared", doc["previousTitle"])
							}
						}
					}
					if tc.title == "renamed" {
						assert.NotContains(t, a.items, "opaque-grafana")
						assert.Contains(t, a.items, "server-renamed")
						assert.Contains(t, a.items, "opaque-mimir")
						assert.Equal(t, "opaque-grafana", templateMap(t, a.body["metadata"])["name"])
					}
					if tc.name == "stale version retained" {
						assert.True(t, apierrors.IsConflict(res.err))
						assert.Equal(t, "1", templateMap(t, a.body["metadata"])["resourceVersion"])
					}
					if tc.name == "wrong API name" {
						assert.Equal(t, gcxerrors.ExitUsageError, templateUsageCode(res.err))
					}
				})
			}
		})
	}
}
func TestTemplateGroupsPagination(t *testing.T) {
	templateTestMode(t)
	for _, args := range [][]string{{"list", "-o", "json"}, {"get", "shared", "--kind", "mimir", "-o", "json"}, {"get", "shared"}} {
		t.Run(fmt.Sprint(args), func(t *testing.T) {
			a := newTemplateNativeAPI(t, "v1beta1")
			a.paginate = true
			res := runTemplateGroups(t, templateAPIBinding(t, a), "", args...)
			a.mu.Lock()
			defer a.mu.Unlock()
			assert.GreaterOrEqual(t, len(a.requests), 2)
			if len(args) == 2 {
				require.ErrorContains(t, res.err, "--kind")
			} else {
				require.NoError(t, res.err)
			}
			if args[0] == "list" {
				var doc map[string]any
				require.NoError(t, json.Unmarshal([]byte(res.stdout), &doc))
				assert.Len(t, doc["items"], 3)
			}
		})
	}
}
func TestTemplateGroupsEmptyList(t *testing.T) {
	templateTestMode(t)
	a := newTemplateNativeAPI(t, "v1beta1")
	a.items = map[string]map[string]any{}
	res := runTemplateGroups(t, templateAPIBinding(t, a), "", "list", "-o", "json")
	require.NoError(t, res.err)
	var doc map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.stdout), &doc))
	assert.Equal(t, []any{}, doc["items"])
}

func TestTemplateGroupsFileAndPipeRoundTrip(t *testing.T) {
	templateTestMode(t)
	for _, version := range []string{"v0alpha1", "v1beta1"} {
		t.Run(version, func(t *testing.T) {
			a := newTemplateNativeAPI(t, version)
			binding := templateAPIBinding(t, a)
			get := runTemplateGroups(t, binding, "", "get", "shared", "--kind", "grafana", "-o", "yaml")
			require.NoError(t, get.err)
			var fetched map[string]any
			require.NoError(t, yaml.Unmarshal([]byte(get.stdout), &fetched))
			a.mu.Lock()
			assert.Equal(t, a.items["opaque-grafana"], fetched)
			a.mu.Unlock()
			pipe := runTemplateGroups(t, binding, get.stdout, "update", "shared", "-f", "-", "-o", "json")
			require.NoError(t, pipe.err)
			var receipt map[string]any
			require.NoError(t, json.Unmarshal([]byte(pipe.stdout), &receipt))
			assert.Equal(t, "shared", receipt["title"])
			assert.Equal(t, "8", receipt["resourceVersion"])
			for _, format := range []string{"json", "yaml"} {
				t.Run(format, func(t *testing.T) {
					manifest := []byte(templateManifest(version, "source-name", "new-"+format, "grafana", "1"))
					if format == "yaml" {
						var err error
						manifest, err = yaml.JSONToYAML(manifest)
						require.NoError(t, err)
					}
					path := filepath.Join(t.TempDir(), "manifest."+format)
					require.NoError(t, os.WriteFile(path, manifest, 0600))
					create := runTemplateGroups(t, binding, "", "create", "-f", path)
					require.NoError(t, create.err)
					assert.Contains(t, create.stdout, "new-"+format)
					assert.Contains(t, create.stdout, "API name")
					after, err := os.ReadFile(path)
					require.NoError(t, err)
					assert.Equal(t, manifest, after, "commands must not rewrite manifests")
				})
			}
		})
	}
}
func TestTemplateGroupsSameKindAmbiguity(t *testing.T) {
	templateTestMode(t)
	a := newTemplateNativeAPI(t, "v1beta1")
	var duplicate map[string]any
	require.NoError(t, json.Unmarshal([]byte(templateManifest("v1beta1", "second-grafana", "shared", "grafana", "7")), &duplicate))
	a.items["second-grafana"] = duplicate
	for _, args := range [][]string{{"get", "shared", "--kind", "grafana"}, {"delete", "shared", "--kind", "grafana", "--force"}, {"update", "shared", "-f", "-"}} {
		res := runTemplateGroups(t, templateAPIBinding(t, a), templateManifest("v1beta1", "opaque-grafana", "shared", "grafana", "7"), args...)
		require.ErrorContains(t, res.err, "ambiguous")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, request := range a.requests {
		assert.True(t, strings.HasPrefix(request, "GET "), "ambiguous subject must never mutate")
	}
}

func templateMap(t *testing.T, value any) map[string]any {
	t.Helper()
	result, ok := value.(map[string]any)
	require.True(t, ok, "expected object, got %T", value)
	return result
}

func TestTemplateGroupsDeleteServerError(t *testing.T) {
	templateTestMode(t)
	a := newTemplateNativeAPI(t, "v1beta1")
	a.deleteStatus = http.StatusForbidden
	res := runTemplateGroups(t, templateAPIBinding(t, a), "", "delete", "shared", "--kind", "grafana", "--force")
	require.ErrorContains(t, res.err, "Forbidden")
	assert.Equal(t, gcxerrors.ExitAuthFailure, templateUsageCode(res.err))
	a.mu.Lock()
	defer a.mu.Unlock()
	assert.Equal(t, []string{"GET " + a.path, "DELETE " + a.path + "/opaque-grafana"}, a.requests)
	assert.Contains(t, a.items, "opaque-grafana")
}
