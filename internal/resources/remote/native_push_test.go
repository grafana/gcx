package remote_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/dynamic"
	"github.com/grafana/gcx/internal/resources/remote"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
)

// These tests exercise real HTTP request bodies and repeat pushes against a
// recording API, including overlapping historical/current opaque names.
//
//nolint:gocyclo,maintidx // Keeping all identity cases in one table makes request-body and repeat-push assertions comparable.
func TestNativePushHTTP(t *testing.T) {
	for _, version := range []string{"v0alpha1", "v1beta1"} {
		for _, kind := range []string{"TimeInterval", "TemplateGroup"} {
			for _, tc := range []struct {
				name, sourceName, desired, remoteTitle                                                    string
				forbidden, listForbidden, ambiguous, imported, getTransport, listTransport, unknownRemote bool
			}{
				{name: "empty create", sourceName: "source", desired: "new"},
				{name: "unnamed create", desired: "new"},
				{name: "get transport failure", sourceName: "source", desired: "new", getTransport: true},
				{name: "list transport failure", sourceName: "source", desired: "new", listTransport: true},
				{name: "unnamed match", desired: "same", remoteTitle: "same"},
				{name: "missing hint match", sourceName: "source", desired: "same", remoteTitle: "same"},
				{name: "matching hint", sourceName: "destination", desired: "same", remoteTitle: "same"},
				{name: "unknown server kind name hint", sourceName: "destination", desired: "new", remoteTitle: "old", unknownRemote: true},
				{name: "edited title", sourceName: "destination", desired: "new", remoteTitle: "old"},
				{name: "encoding collision", sourceName: "destination", desired: "grafana|foo", remoteTitle: "foo"},
				{name: "forbidden get", sourceName: "source", desired: "new", forbidden: true},
				{name: "forbidden list", sourceName: "source", desired: "new", listForbidden: true},
				{name: "ambiguity", desired: "same", remoteTitle: "same", ambiguous: true},
				{name: "imported", sourceName: "source", desired: "same", imported: true},
			} {
				if (tc.imported || tc.unknownRemote) && kind != "TemplateGroup" {
					continue
				}
				t.Run(version+"/"+kind+"/"+tc.name, func(t *testing.T) {
					desc := resources.Descriptor{GroupVersion: schema.GroupVersion{Group: "notifications.alerting.grafana.app", Version: version}, Kind: kind, Plural: "timeintervals", Singular: "timeinterval"}
					if kind == "TemplateGroup" {
						desc.Plural = "templategroups"
						desc.Singular = "templategroup"
					}
					makeObj := func(name, title, rv string) *unstructured.Unstructured {
						field := "name"
						if kind == "TemplateGroup" {
							field = "title"
						}
						return &unstructured.Unstructured{Object: map[string]any{"apiVersion": desc.GroupVersion.String(), "kind": kind, "metadata": map[string]any{"name": name, "namespace": "target", "resourceVersion": rv, "uid": "source-uid", "creationTimestamp": "2026-01-01T00:00:00Z"}, "spec": map[string]any{field: title}}}
					}
					objects := map[string]*unstructured.Unstructured{}
					if tc.remoteTitle != "" {
						objects["destination"] = makeObj("destination", tc.remoteTitle, "destination-version")
						if tc.unknownRemote {
							require.NoError(t, unstructured.SetNestedField(objects["destination"].Object, "future", "spec", "kind"))
						}
						if kind == "TemplateGroup" {
							require.NoError(t, unstructured.SetNestedField(objects["destination"].Object, "grafana", "spec", "kind"))
						}
					}
					if tc.ambiguous {
						objects["duplicate"] = makeObj("duplicate", tc.remoteTitle, "other-version")
					}
					// An imported equal-title group must not substitute for the editable one.
					if kind == "TemplateGroup" && tc.remoteTitle != "" {
						importObj := makeObj("import", tc.remoteTitle, "import-version")
						require.NoError(t, unstructured.SetNestedField(importObj.Object, "mimir", "spec", "kind"))
						objects["import"] = importObj
					}
					originalRemote := map[string]*unstructured.Unstructured{}
					for name, obj := range objects {
						originalRemote[name] = obj.DeepCopy()
					}
					var mutations []string
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						w.Header().Set("Content-Type", "application/json")
						base := "/apis/" + desc.GroupVersion.String() + "/namespaces/target/" + desc.Plural
						name := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, base), "/")
						status := func(code int, reason string) {
							w.WriteHeader(code)
							assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"apiVersion": "v1", "kind": "Status", "status": "Failure", "reason": reason, "code": code, "message": reason}))
						}
						switch r.Method {
						case http.MethodGet:
							if name == "" {
								if tc.listForbidden {
									status(403, "Forbidden")
									return
								}
								list := &unstructured.UnstructuredList{Items: []unstructured.Unstructured{}}
								list.SetAPIVersion(desc.GroupVersion.String())
								list.SetKind(kind + "List")
								for _, obj := range objects {
									list.Items = append(list.Items, *obj.DeepCopy())
								}
								assert.NoError(t, json.NewEncoder(w).Encode(list))
								return
							}
							if tc.forbidden {
								status(403, "Forbidden")
								return
							}
							obj, ok := objects[name]
							if !ok {
								status(404, "NotFound")
								return
							}
							assert.NoError(t, json.NewEncoder(w).Encode(obj))
						case http.MethodPost, http.MethodPut:
							var obj unstructured.Unstructured
							assert.NoError(t, json.NewDecoder(r.Body).Decode(&obj))
							mutations = append(mutations, r.Method)
							if r.Method == http.MethodPost {
								assert.Empty(t, obj.GetName())
								assert.Empty(t, obj.GetResourceVersion())
								assert.Empty(t, obj.GetUID())
								assert.Empty(t, obj.GetCreationTimestamp())
								obj.SetName("created-by-server")
							} else {
								assert.Equal(t, name, obj.GetName())
								assert.Equal(t, objects[name].GetResourceVersion(), obj.GetResourceVersion())
							}
							obj.SetResourceVersion("target-next-version")
							objects[obj.GetName()] = obj.DeepCopy()
							assert.NoError(t, json.NewEncoder(w).Encode(&obj))
						default:
							t.Errorf("unexpected method %s", r.Method)
							status(500, "InternalError")
						}
					}))
					defer server.Close()
					client, err := dynamic.NewDefaultNamespacedClient(config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL, WrapTransport: func(rt http.RoundTripper) http.RoundTripper {
						return &nativeFailTransport{RoundTripper: rt, failGet: tc.getTransport, failList: tc.listTransport, collection: desc.Plural}
					}}, Namespace: "target"})
					require.NoError(t, err)
					pusher := remote.NewPusher(client, &mockPushRegistry{supportedResources: []resources.Descriptor{desc}})
					local := makeObj(tc.sourceName, tc.desired, "source-version")
					if tc.imported {
						require.NoError(t, unstructured.SetNestedField(local.Object, "mimir", "spec", "kind"))
					}
					unchanged := local.DeepCopy()
					res := resources.MustFromObject(local.Object, resources.SourceInfo{})
					push := func() {
						summary, err := pusher.Push(t.Context(), remote.PushRequest{Resources: resources.NewResources(res), MaxConcurrency: 1, IncludeManaged: true})
						require.NoError(t, err)
						switch {
						case tc.imported:
							require.Equal(t, 1, summary.SkippedCount())
							require.Zero(t, summary.FailedCount())
							require.Empty(t, mutations)
						case tc.forbidden || tc.listForbidden || tc.ambiguous || tc.getTransport || tc.listTransport:
							require.Equal(t, 1, summary.FailedCount())
							require.Empty(t, mutations)
						default:
							require.Equal(t, 1, summary.SuccessCount())
						}
						require.Equal(t, unchanged.Object, res.Object.Object)
					}
					push()
					if tc.forbidden || tc.listForbidden || tc.ambiguous || tc.imported || tc.getTransport || tc.listTransport {
						return
					}
					push()
					require.Len(t, mutations, 2)
					require.Equal(t, http.MethodPut, mutations[1])
					if tc.remoteTitle == tc.desired {
						require.Equal(t, []string{http.MethodPut, http.MethodPut}, mutations)
					} else {
						require.Equal(t, http.MethodPost, mutations[0])
					}
					if tc.remoteTitle != "" && tc.remoteTitle != tc.desired {
						require.Equal(t, originalRemote["destination"], objects["destination"])
					}
					if kind == "TemplateGroup" && tc.remoteTitle != "" {
						require.Equal(t, originalRemote["import"], objects["import"])
					}
				})
			}
		}
	}
}

// nativeFailTransport injects a non-retryable transport failure before HTTP I/O.
type nativeFailTransport struct {
	http.RoundTripper

	failGet, failList bool
	collection        string
}

func (rt *nativeFailTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	isList := strings.HasSuffix(r.URL.Path, "/"+rt.collection)
	if r.Method == http.MethodGet && ((rt.failGet && !isList) || (rt.failList && isList)) {
		return nil, errors.New("synthetic transport failure")
	}
	return rt.RoundTripper.RoundTrip(r)
}
