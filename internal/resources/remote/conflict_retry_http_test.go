package remote //nolint:testpackage // Compare the wrapped and unwrapped clients using the same recording API.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/resources/dynamic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
)

type retryHTTPRegistry struct{ desc resources.Descriptor }

func (r retryHTTPRegistry) SupportedResources() resources.Descriptors {
	return resources.Descriptors{r.desc}
}

// Each mutation snapshots a shared configuration version, yields to other
// writers, then commits only if its snapshot is still current. This models
// transient conflicts between otherwise independent resource writes.
func TestConflictRetryHTTPContention(t *testing.T) {
	for _, operation := range []string{"create", "update", "delete"} {
		for _, retryEnabled := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/retry=%t", operation, retryEnabled), func(t *testing.T) {
				const count = 20
				const maxConcurrency = 10
				desc := resources.Descriptor{GroupVersion: schema.GroupVersion{Group: "notifications.alerting.grafana.app", Version: "v1beta1"}, Kind: "TimeInterval", Plural: "timeintervals", Singular: "timeinterval"}
				makeObj := func(index int, named bool) *unstructured.Unstructured {
					obj := &unstructured.Unstructured{Object: map[string]any{
						"apiVersion": desc.GroupVersion.String(), "kind": desc.Kind,
						"metadata": map[string]any{"namespace": "target"},
						"spec":     map[string]any{"name": fmt.Sprintf("interval-%02d", index), "time_intervals": []any{}},
					}}
					if named {
						obj.SetName(fmt.Sprintf("opaque-%02d", index))
						obj.SetResourceVersion("1")
					}
					return obj
				}
				objects := map[string]*unstructured.Unstructured{}
				local := resources.NewResources()
				original := map[resources.ResourceRef]*unstructured.Unstructured{}
				for i := range count {
					if operation != "create" {
						obj := makeObj(i, true)
						objects[obj.GetName()] = obj
					}
					obj := makeObj(i, operation == "delete")
					res := resources.MustFromUnstructured(obj)
					local.Add(res)
					original[res.Ref()] = obj.DeepCopy()
				}
				var mu sync.Mutex
				configVersion, conflicts, mutations, active, peak := 0, 0, 0, 0, 0
				successful := map[string]int{}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					status := func(code int32, reason string) {
						w.WriteHeader(int(code))
						assert.NoError(t, json.NewEncoder(w).Encode(metav1.Status{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"}, Status: "Failure", Reason: metav1.StatusReason(reason), Code: code, Message: reason}))
					}
					base := "/apis/" + desc.GroupVersion.String() + "/namespaces/target/" + desc.Plural
					if !strings.HasPrefix(r.URL.Path, base) {
						status(404, "NotFound")
						return
					}
					name := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, base), "/")
					if r.Method == http.MethodGet {
						mu.Lock()
						defer mu.Unlock()
						if name != "" {
							obj, ok := objects[name]
							if !ok {
								status(404, "NotFound")
								return
							}
							assert.NoError(t, json.NewEncoder(w).Encode(obj))
							return
						}
						list := &unstructured.UnstructuredList{Items: []unstructured.Unstructured{}}
						list.SetAPIVersion(desc.GroupVersion.String())
						list.SetKind(desc.Kind + "List")
						for _, obj := range objects {
							list.Items = append(list.Items, *obj.DeepCopy())
						}
						assert.NoError(t, json.NewEncoder(w).Encode(list))
						return
					}
					var obj unstructured.Unstructured
					switch r.Method {
					case http.MethodPost, http.MethodPut:
						if err := json.NewDecoder(r.Body).Decode(&obj); err != nil {
							t.Errorf("decode request: %v", err)
							status(400, "BadRequest")
							return
						}
						if r.Method == http.MethodPost {
							assert.Empty(t, obj.GetName())
							title, _, _ := unstructured.NestedString(obj.Object, "spec", "name")
							name = "opaque-" + strings.TrimPrefix(title, "interval-")
							obj.SetName(name)
						}
					case http.MethodDelete:
						assert.NotEmpty(t, name)
					default:
						t.Errorf("unexpected method %s", r.Method)
						status(500, "InternalError")
						return
					}
					mu.Lock()
					snapshot := configVersion
					mutations++
					active++
					if active > peak {
						peak = active
					}
					mu.Unlock()
					time.Sleep(3 * time.Millisecond)
					mu.Lock()
					defer mu.Unlock()
					active--
					if snapshot != configVersion {
						conflicts++
						status(409, "Conflict")
						return
					}
					configVersion++
					successful[name]++
					if r.Method == http.MethodDelete {
						delete(objects, name)
						assert.NoError(t, json.NewEncoder(w).Encode(metav1.Status{Status: "Success"}))
						return
					}
					if r.Method == http.MethodPut {
						assert.Equal(t, objects[name].GetResourceVersion(), obj.GetResourceVersion())
					}
					obj.SetResourceVersion(strconv.Itoa(configVersion + 1))
					objects[name] = obj.DeepCopy()
					assert.NoError(t, json.NewEncoder(w).Encode(&obj))
				}))
				defer server.Close()
				dynamicClient, err := dynamic.NewDefaultNamespacedClient(config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL, QPS: 1000, Burst: 1000}, Namespace: "target"})
				require.NoError(t, err)
				var client adapter.DynamicClient = dynamicClient
				if retryEnabled {
					client = newConflictRetryClient(client)
				}
				client = newGuardedDynamicClient(client, GuardConfig{})
				registry := retryHTTPRegistry{desc: desc}
				var summary *OperationSummary
				if operation == "delete" {
					summary, err = NewDeleterWithClient(client, registry).Delete(t.Context(), DeleteRequest{Resources: local, MaxConcurrency: maxConcurrency})
				} else {
					summary, err = NewPusher(client, registry).Push(t.Context(), PushRequest{Resources: local, MaxConcurrency: maxConcurrency, IncludeManaged: true, NoPushFailureLog: true})
				}
				require.NoError(t, err)
				mu.Lock()
				defer mu.Unlock()
				require.Positive(t, conflicts, "test must actually exercise contention")
				require.Greater(t, peak, 1, "writes must overlap")
				require.LessOrEqual(t, peak, maxConcurrency)
				if retryEnabled {
					require.Equal(t, count, summary.SuccessCount(), "failures: %v", summary.Failures())
					require.Zero(t, summary.FailedCount())
					require.Len(t, successful, count)
					for name, committed := range successful {
						require.Equal(t, 1, committed, "%s committed more than once", name)
					}
					if operation == "delete" {
						require.Empty(t, objects)
					} else {
						require.Len(t, objects, count)
					}
					require.Greater(t, mutations, count)
				} else {
					require.Positive(t, summary.FailedCount(), "control batch must fail without retry")
					require.Equal(t, count, mutations, "unwrapped client must make one request per manifest")
				}
				for _, res := range local.AsList() {
					require.Equal(t, original[res.Ref()], &res.Object, "retry must not alter local manifests")
				}
			})
		}
	}
}
