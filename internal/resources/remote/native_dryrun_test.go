package remote //nolint:testpackage // Exercises the unexported dry-run guard together with the real dynamic client.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/dynamic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
)

// nativePreviewServer deliberately applies every mutation, even dryRun=All. A
// forwarded preview would therefore change state, just as an unsupported API can.
type nativePreviewServer struct {
	t       *testing.T
	desc    resources.Descriptor
	objects map[string]*unstructured.Unstructured
	reads   []string
	writes  []string
	dryRuns []string
}

func (s *nativePreviewServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	collection := "/apis/" + s.desc.GroupVersion.String() + "/namespaces/default/" + s.desc.Plural
	if r.Method == http.MethodGet {
		s.reads = append(s.reads, r.URL.Path)
		if r.URL.Path == collection {
			items := make([]any, 0, len(s.objects))
			for _, obj := range s.objects {
				items = append(items, obj.Object)
			}
			assert.NoError(s.t, json.NewEncoder(w).Encode(map[string]any{"apiVersion": s.desc.GroupVersion.String(), "kind": s.desc.Kind + "List", "items": items}))
			return
		}
		if obj := s.objects[path.Base(r.URL.Path)]; obj != nil {
			assert.NoError(s.t, json.NewEncoder(w).Encode(obj.Object))
			return
		}
		w.WriteHeader(http.StatusNotFound)
		assert.NoError(s.t, json.NewEncoder(w).Encode(metav1.Status{Status: "Failure", Reason: metav1.StatusReasonNotFound, Code: http.StatusNotFound}))
		return
	}

	s.writes = append(s.writes, r.Method+" "+r.URL.Path)
	s.dryRuns = append(s.dryRuns, r.URL.Query().Get("dryRun"))
	if r.Method == http.MethodDelete {
		var opts metav1.DeleteOptions
		if err := json.NewDecoder(r.Body).Decode(&opts); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.dryRuns[len(s.dryRuns)-1] = strings.Join(opts.DryRun, ",")
		delete(s.objects, path.Base(r.URL.Path))
		assert.NoError(s.t, json.NewEncoder(w).Encode(metav1.Status{Status: "Success"}))
		return
	}
	var obj unstructured.Unstructured
	if err := json.NewDecoder(r.Body).Decode(&obj); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if obj.GetName() == "" {
		obj.SetName("server-derived-name")
	}
	s.objects[obj.GetName()] = obj.DeepCopy()
	assert.NoError(s.t, json.NewEncoder(w).Encode(obj.Object))
}

func nativePreviewDescriptor(kind, version string) resources.Descriptor {
	plural, singular := "timeintervals", "timeinterval"
	if kind == "TemplateGroup" {
		plural, singular = "templategroups", "templategroup"
	}
	return resources.Descriptor{
		GroupVersion: schema.GroupVersion{Group: "notifications.alerting.grafana.app", Version: version},
		Kind:         kind, Plural: plural, Singular: singular,
	}
}

func nativePreviewObject(desc resources.Descriptor, name string) *unstructured.Unstructured {
	spec := map[string]any{"name": "weekends", "time_intervals": []any{}}
	if desc.Kind == "TemplateGroup" {
		spec = map[string]any{"title": "weekends", "kind": "grafana", "content": "{{ define \"weekends\" }}preview{{ end }}"}
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": desc.GroupVersion.String(), "kind": desc.Kind,
		"metadata": map[string]any{"name": name, "namespace": "default", "resourceVersion": "source-version", "uid": "source-uid"},
		"spec":     spec,
	}}
}

func nativePreviewClient(t *testing.T, desc resources.Descriptor, existing *unstructured.Unstructured) (*dynamic.NamespacedClient, *nativePreviewServer) {
	t.Helper()
	recorder := &nativePreviewServer{t: t, desc: desc, objects: make(map[string]*unstructured.Unstructured)}
	if existing != nil {
		recorder.objects[existing.GetName()] = existing.DeepCopy()
	}
	server := httptest.NewServer(recorder)
	t.Cleanup(server.Close)
	client, err := dynamic.NewDefaultNamespacedClient(config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}, Namespace: "default"})
	require.NoError(t, err)
	return client, recorder
}

func TestNativeNotificationDryRunGuard(t *testing.T) {
	for _, kind := range []string{"TimeInterval", "TemplateGroup"} {
		for _, version := range []string{"v0alpha1", "v1beta1"} {
			for _, verb := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
				for _, asserted := range []bool{false, true} {
					name := kind + "/" + version + "/" + verb
					if asserted {
						name += "/user-asserted"
					}
					t.Run(name, func(t *testing.T) {
						desc := nativePreviewDescriptor(kind, version)
						obj := nativePreviewObject(desc, "existing-name")
						client, recorder := nativePreviewClient(t, desc, obj)
						warn := &bytes.Buffer{}
						cfg := GuardConfig{Warn: warn}
						if asserted {
							cfg.AssumeServerDryRun = []string{desc.Plural + "." + desc.GroupVersion.Group}
						}
						guard := newGuardedDynamicClient(client, cfg)
						var err error
						switch verb {
						case http.MethodPost:
							_, err = guard.Create(t.Context(), desc, obj, metav1.CreateOptions{DryRun: dryRun()})
						case http.MethodPut:
							_, err = guard.Update(t.Context(), desc, obj, metav1.UpdateOptions{DryRun: dryRun()})
						case http.MethodDelete:
							err = guard.Delete(t.Context(), desc, obj.GetName(), metav1.DeleteOptions{DryRun: dryRun()})
						}
						if asserted {
							require.NoError(t, err)
							require.Len(t, recorder.writes, 1)
							require.True(t, strings.HasPrefix(recorder.writes[0], verb+" "))
							require.Equal(t, []string{metav1.DryRunAll}, recorder.dryRuns)
							require.Contains(t, warn.String(), "user assertion")
						} else {
							require.ErrorIs(t, err, errDryRunUnverified)
							require.Empty(t, recorder.writes, "no POST, PUT, PATCH, or DELETE may escape default preview")
							require.Equal(t, obj, recorder.objects[obj.GetName()])
							require.Contains(t, warn.String(), "client-side only")
							require.Contains(t, warn.String(), "does not support server-side dry-run")
							if verb == http.MethodDelete {
								require.Len(t, recorder.reads, 1)
								require.Contains(t, warn.String(), "No delete was sent")
							} else {
								require.Contains(t, warn.String(), "No changes were sent")
							}
						}
					})
				}
			}
		}
	}
}

func TestNativeNotificationPusherDryRun(t *testing.T) {
	for _, kind := range []string{"TimeInterval", "TemplateGroup"} {
		for _, version := range []string{"v0alpha1", "v1beta1"} {
			for _, scenario := range []string{"create", "unnamed create", "name-matched update", "logical-matched update"} {
				t.Run(kind+"/"+version+"/"+scenario, func(t *testing.T) {
					desc := nativePreviewDescriptor(kind, version)
					src := nativePreviewObject(desc, "source-name")
					var existing *unstructured.Unstructured
					switch scenario {
					case "unnamed create":
						src.SetName("")
					case "name-matched update":
						existing = src.DeepCopy()
					case "logical-matched update":
						existing = src.DeepCopy()
						existing.SetName("destination-name")
					}
					if existing != nil {
						existing.SetResourceVersion("destination-version")
					}
					before := src.DeepCopy()
					client, recorder := nativePreviewClient(t, desc, existing)
					warn := &bytes.Buffer{}
					guard := newGuardedDynamicClient(client, GuardConfig{Warn: warn})
					pusher := NewPusher(guard, fixedRegistry{descs: resources.Descriptors{desc}})
					res := resources.MustFromObject(src.Object, resources.SourceInfo{})
					summary, err := pusher.Push(t.Context(), PushRequest{
						Resources: resources.NewResources(res), DryRun: true, MaxConcurrency: 1, StopOnError: true, IncludeManaged: true,
					})
					require.NoError(t, err)
					require.Zero(t, summary.SuccessCount())
					require.Zero(t, summary.FailedCount())
					require.Equal(t, 1, summary.SkippedCount())
					require.Empty(t, recorder.writes, "even normalized creates and logical-matched updates must not reach the server")
					require.Contains(t, warn.String(), "did not validate the spec")
					require.Contains(t, warn.String(), "No changes were sent")
					require.Equal(t, *before, res.Object)
					if existing == nil {
						require.Empty(t, recorder.objects)
					} else {
						require.Equal(t, existing, recorder.objects[existing.GetName()])
					}
					if scenario == "logical-matched update" {
						require.Len(t, recorder.reads, 2, "a stale source name must be followed by logical identity listing before guarded update")
					}
				})
			}
		}
	}
}

func TestNativeNotificationDeleterDryRun(t *testing.T) {
	for _, kind := range []string{"TimeInterval", "TemplateGroup"} {
		for _, version := range []string{"v0alpha1", "v1beta1"} {
			t.Run(kind+"/"+version, func(t *testing.T) {
				desc := nativePreviewDescriptor(kind, version)
				obj := nativePreviewObject(desc, "existing-name")
				client, recorder := nativePreviewClient(t, desc, obj)
				warn := &bytes.Buffer{}
				guard := newGuardedDynamicClient(client, GuardConfig{Warn: warn})
				deleter := NewDeleterWithClient(guard, fixedRegistry{descs: resources.Descriptors{desc}})
				summary, err := deleter.Delete(t.Context(), DeleteRequest{
					Resources: resources.NewResources(resources.MustFromObject(obj.Object, resources.SourceInfo{})),
					DryRun:    true, MaxConcurrency: 1, StopOnError: true,
				})
				require.NoError(t, err)
				require.Zero(t, summary.SuccessCount())
				require.Zero(t, summary.FailedCount())
				require.Equal(t, 1, summary.SkippedCount())
				require.Empty(t, recorder.writes)
				require.Len(t, recorder.reads, 1)
				require.Equal(t, obj, recorder.objects[obj.GetName()])
				require.Contains(t, warn.String(), "whether the target exists")
				require.Contains(t, warn.String(), "No delete was sent")
			})
		}
	}
}
