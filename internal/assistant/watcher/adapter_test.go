//nolint:testpackage // White-box coverage verifies private identity never enters serialization.
package watcher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/assistant/assistanthttp"
	"github.com/grafana/gcx/internal/assistant/watchers"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/rest"
)

const testCollectionPath = "/api/plugins/grafana-assistant-app/resources/api/v1/watcher-agents"

func fixtureWatcher(id, title string) watchers.Watcher {
	return watchers.Watcher{ID: id, Name: title, Prompt: "Watch sustained errors", TriggerIntervalSeconds: 900, Sensitivity: "balanced"}
}

func clientForServer(t *testing.T, handler http.HandlerFunc) *watchers.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	base, err := assistanthttp.NewClient(config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}, Namespace: "example"})
	require.NoError(t, err)
	return watchers.NewClient(base)
}

func writeData(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"data": value}))
}

func TestResolveAllPartitionsIDPrecedenceAndCollisions(t *testing.T) {
	active := []watchers.Watcher{fixtureWatcher("id-1", "Checkout Health"), fixtureWatcher("id-2", "id-1")}
	archived := fixtureWatcher("id-3", "Checkout / Health")
	client := clientForServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == testCollectionPath {
			if r.URL.Query().Get("archived") == "true" {
				writeData(t, w, map[string]any{"agents": []watchers.Watcher{archived}})
				return
			}
			if r.URL.Query().Get("cursor") == "later" {
				writeData(t, w, map[string]any{"agents": active[1:]})
				return
			}
			writeData(t, w, map[string]any{"agents": active[:1], "nextCursor": "later"})
			return
		}
		id := strings.TrimPrefix(r.URL.Path, testCollectionPath+"/")
		for _, item := range append(active, archived) {
			if item.ID == id {
				writeData(t, w, item)
				return
			}
		}
		http.NotFound(w, r)
	})
	raw, err := Resolve(t.Context(), client, "id-1")
	require.NoError(t, err)
	assert.Equal(t, "Checkout Health", raw.Name)
	raw, err = Resolve(t.Context(), client, "id-3")
	require.NoError(t, err)
	assert.Equal(t, "id-3", raw.ID)
	_, err = Resolve(t.Context(), client, "checkout-health")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "id-1")
	assert.Contains(t, err.Error(), "id-3")
	_, err = Resolve(t.Context(), client, "missing")
	require.ErrorIs(t, err, adapter.ErrNotFound)
	index, err := IdentityIndex(t.Context(), client)
	require.NoError(t, err)
	assert.Len(t, index["checkout-health"], 2)
}

func TestManifestProjectionAndEnvelopeParity(t *testing.T) {
	stop := time.Date(2030, 1, 1, 18, 0, 0, 0, time.UTC)
	raw := fixtureWatcher("id-1", "Checkout health")
	raw.Description = "Checkout checks"
	raw.DatasourceUIDs = []string{"example-prometheus"}
	raw.Labels = map[string]string{"service": "checkout"}
	raw.AutoStop = &watchers.AutoStop{PauseAt: &stop, Archive: true}
	raw.DisableDecisionSkip = true
	raw.Actions = watchers.Actions{
		Slack:    &watchers.ChatAction{Enabled: true, Target: &watchers.ChatTarget{ChannelID: "C_EXAMPLE"}, MinSeverity: "warning"},
		MSTeams:  &watchers.ChatAction{Enabled: false, Target: &watchers.ChatTarget{ChannelID: "example-channel"}, MinSeverity: "critical"},
		Webhook:  &watchers.WebhookAction{Enabled: false, MinSeverity: "critical", SecureFields: &watchers.SecureFields{URL: true, HMACSecret: true, AuthorizationCredentials: true}},
		Alerting: &watchers.AlertingAction{Enabled: true}, Investigation: &watchers.InvestigationAction{Enabled: true, TeamNames: []string{"example-team"}},
	}
	raw.Status = "paused"
	raw.CalibrationContext = "runtime-only"
	raw.Queries = []map[string]any{{"expression": "runtime-only"}}
	raw.DefinitionVersion = 123
	client := clientForServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case testCollectionPath:
			if r.URL.Query().Get("archived") == "true" {
				writeData(t, w, map[string]any{"agents": []any{}})
			} else {
				writeData(t, w, map[string]any{"agents": []watchers.Watcher{raw}})
			}
		case testCollectionPath + "/id-1":
			data, err := json.Marshal(raw)
			assert.NoError(t, err)
			var response map[string]any
			assert.NoError(t, json.Unmarshal(data, &response))
			webhook := testObject(t, testObject(t, response["actions"])["webhook"])
			webhook["url"] = "DO_NOT_EXPORT_URL"
			webhook["urlDisplay"] = "DO_NOT_EXPORT_DISPLAY"
			webhook["secureSettings"] = map[string]any{"hmacSecret": "DO_NOT_EXPORT_SECRET"}
			writeData(t, w, response)
		case testCollectionPath + "/id-1/auto-calibration":
			writeData(t, w, map[string]any{"enabled": true})
		default:
			t.Errorf("unexpected request %s", r.URL)
			http.NotFound(w, r)
		}
	})
	crud := NewTypedCRUDForClient(client, "example")
	domain, err := crud.GetFn(t.Context(), "checkout-health")
	require.NoError(t, err)
	dedicated, err := crud.ToUnstructured(*domain)
	require.NoError(t, err)
	generic, err := crud.AsAdapter().Get(t.Context(), "id-1", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, dedicated.Object, generic.Object)
	assert.Equal(t, "id-1", generic.GetAnnotations()[WatcherIDAnnotation])
	assert.Equal(t, "example", generic.GetNamespace())
	spec := testObject(t, generic.Object["spec"])
	assert.Equal(t, true, testObject(t, spec["automaticRecalibration"])["enabled"])
	assert.Equal(t, false, spec["skipReviewOnCleanRuns"])
	assert.Equal(t, "2030-01-01T18:00:00Z", testObject(t, spec["autoStop"])["at"])
	webhook := testObject(t, testObject(t, spec["notifications"])["webhook"])
	for _, key := range []string{"url", "bearerToken", "signingSecret"} {
		assert.Equal(t, map[string]any{"preserve": true}, webhook[key])
	}
	payload, err := json.Marshal(generic.Object)
	require.NoError(t, err)
	for _, excluded := range []string{"runtime-only", "DO_NOT_EXPORT", "calibrationContext", "queries", "definitionVersion", "status", "serverID"} {
		assert.NotContains(t, string(payload), excluded)
	}
	var reparsed struct {
		Spec Watcher `json:"spec"`
	}
	require.NoError(t, json.Unmarshal(payload, &reparsed))
	require.NoError(t, reparsed.Spec.Validate())
}

func TestListPartitionAndEnrollmentFailure(t *testing.T) {
	for _, denied := range []bool{false, true} {
		t.Run(fmt.Sprintf("denied=%v", denied), func(t *testing.T) {
			var mu sync.Mutex
			var requests []string
			client := clientForServer(t, func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				requests = append(requests, r.URL.String())
				mu.Unlock()
				switch r.URL.Path {
				case testCollectionPath:
					assert.Equal(t, "true", r.URL.Query().Get("archived"))
					writeData(t, w, map[string]any{"agents": []watchers.Watcher{fixtureWatcher("archived-id", "Archived")}})
				case testCollectionPath + "/archived-id":
					writeData(t, w, fixtureWatcher("archived-id", "Archived"))
				case testCollectionPath + "/archived-id/auto-calibration":
					if denied {
						w.WriteHeader(http.StatusForbidden)
						writeData(t, w, map[string]any{})
					} else {
						writeData(t, w, map[string]any{"enabled": false})
					}
				default:
					t.Errorf("unexpected request %s", r.URL)
					http.NotFound(w, r)
				}
			})
			items, err := NewTypedCRUDForClientArchived(client, "", true).ListFn(t.Context(), 0)
			if denied {
				assert.Nil(t, items)
				require.ErrorIs(t, err, watchers.ErrPermissionDenied)
			} else {
				require.NoError(t, err)
				require.Len(t, items, 1)
				assert.False(t, items[0].AutomaticRecalibration.Enabled)
			}
			assert.Len(t, requests, 3)
		})
	}
}

func TestListRetainsCollisionsAndEmptyResults(t *testing.T) {
	for _, empty := range []bool{false, true} {
		t.Run(fmt.Sprintf("empty=%v", empty), func(t *testing.T) {
			raws := []watchers.Watcher{}
			if !empty {
				raws = []watchers.Watcher{fixtureWatcher("1", "Same"), fixtureWatcher("2", "Same!")}
			}
			client := clientForServer(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == testCollectionPath {
					writeData(t, w, map[string]any{"agents": raws})
					return
				}
				if strings.HasSuffix(r.URL.Path, "/auto-calibration") {
					writeData(t, w, map[string]any{"enabled": false})
					return
				}
				for _, raw := range raws {
					if strings.HasSuffix(r.URL.Path, "/"+raw.ID) {
						writeData(t, w, raw)
						return
					}
				}
				http.NotFound(w, r)
			})
			items, err := NewTypedCRUDForClient(client, "").ListFn(t.Context(), 0)
			require.NoError(t, err)
			assert.NotNil(t, items)
			assert.Len(t, items, len(raws))
			if !empty {
				assert.Equal(t, items[0].GetResourceName(), items[1].GetResourceName())
				assert.NotEqual(t, items[0].ServerID(), items[1].ServerID())
			}
		})
	}
}

func TestIdentityIndexHasNoSupplementaryReads(t *testing.T) {
	client := clientForServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, testCollectionPath, r.URL.Path)
		id := "active"
		if r.URL.Query().Get("archived") == "true" {
			id = "archived"
		}
		writeData(t, w, map[string]any{"agents": []watchers.Watcher{fixtureWatcher(id, "Same")}})
	})
	index, err := IdentityIndex(t.Context(), client)
	require.NoError(t, err)
	assert.Len(t, index["same"], 2)
}

func TestNoSecretMarkersWithoutConfirmedPresence(t *testing.T) {
	for _, fields := range []*watchers.SecureFields{nil, {}, {URL: true}} {
		raw := fixtureWatcher("1", "Example")
		raw.Actions.Webhook = &watchers.WebhookAction{SecureFields: fields}
		manifest := WatcherFromResponse(raw, false)
		assert.Nil(t, manifest.Notifications.Webhook.BearerToken)
		assert.Nil(t, manifest.Notifications.Webhook.SigningSecret)
		if fields != nil && fields.URL {
			assert.True(t, manifest.Notifications.Webhook.URL.Preserve)
		} else {
			assert.Nil(t, manifest.Notifications.Webhook.URL)
		}
	}
}

func TestMutationsUnsupportedWithoutHTTP(t *testing.T) {
	calls := 0
	client := clientForServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		t.Errorf("unexpected HTTP %s", r.URL)
		http.NotFound(w, r)
	})
	crud := NewTypedCRUDForClient(client, "")
	assert.Nil(t, crud.CreateFn)
	assert.Nil(t, crud.UpdateFn)
	assert.Nil(t, crud.DeleteFn)
	assert.Nil(t, crud.ValidateFn)
	api := crud.AsAdapter()
	obj := &unstructured.Unstructured{Object: map[string]any{"spec": validSpec()}}
	_, err := api.Create(context.Background(), obj, metav1.CreateOptions{})
	require.ErrorIs(t, err, errors.ErrUnsupported)
	_, err = api.Update(context.Background(), obj, metav1.UpdateOptions{})
	require.ErrorIs(t, err, errors.ErrUnsupported)
	err = api.Delete(context.Background(), "id", metav1.DeleteOptions{})
	require.ErrorIs(t, err, errors.ErrUnsupported)
	assert.Zero(t, calls)
	require.ErrorIs(t, UnsupportedMutation("push"), errors.ErrUnsupported)
}

func testObject(t *testing.T, value any) map[string]any {
	t.Helper()
	result, ok := value.(map[string]any)
	assert.True(t, ok, "fixture must contain an object")
	return result
}

func TestManifestReadsRejectInvalidObservedConfiguration(t *testing.T) {
	client := clientForServer(t, func(w http.ResponseWriter, r *http.Request) { writeData(t, w, map[string]any{"enabled": false}) })
	for _, corrupt := range []func(*watchers.Watcher){
		func(raw *watchers.Watcher) { raw.TriggerIntervalSeconds = 0 },
		func(raw *watchers.Watcher) { raw.Sensitivity = "future-value" },
		func(raw *watchers.Watcher) {
			raw.Actions.Webhook = &watchers.WebhookAction{MinSeverity: "future-value"}
		},
		func(raw *watchers.Watcher) { raw.Actions.Webhook = &watchers.WebhookAction{Enabled: true} },
	} {
		raw := fixtureWatcher("id-1", "Example")
		corrupt(&raw)
		manifest, err := ReadManifest(t.Context(), client, raw)
		require.Error(t, err)
		assert.Empty(t, manifest.ServerID())
	}
}

func TestListBoundsSupplementaryConcurrency(t *testing.T) {
	const count = 24
	raw := make([]watchers.Watcher, count)
	for i := range raw {
		raw[i] = fixtureWatcher(strconv.Itoa(i), fmt.Sprintf("Watcher %d", i))
	}
	var active, peak atomic.Int32
	client := clientForServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == testCollectionPath {
			writeData(t, w, map[string]any{"agents": raw})
			return
		}
		current := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); current > old; old = peak.Load() {
			if peak.CompareAndSwap(old, current) {
				break
			}
		}
		time.Sleep(3 * time.Millisecond)
		if strings.HasSuffix(r.URL.Path, "/auto-calibration") {
			writeData(t, w, map[string]any{"enabled": false})
			return
		}
		for _, item := range raw {
			if strings.HasSuffix(r.URL.Path, "/"+item.ID) {
				writeData(t, w, item)
				return
			}
		}
		http.NotFound(w, r)
	})
	items, err := NewTypedCRUDForClient(client, "").ListFn(t.Context(), 0)
	require.NoError(t, err)
	assert.Len(t, items, count)
	assert.LessOrEqual(t, peak.Load(), int32(10))
	assert.Greater(t, peak.Load(), int32(1))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	items, err = NewTypedCRUDForClient(client, "").ListFn(ctx, 0)
	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, items)
}

func TestDeadlinePrecision(t *testing.T) {
	deadline, err := time.Parse(time.RFC3339Nano, "2030-01-01T18:00:00.500Z")
	require.NoError(t, err)
	got := WatcherFromResponse(watchers.Watcher{AutoStop: &watchers.AutoStop{PauseAt: &deadline}}, false)
	parsed, err := time.Parse(time.RFC3339Nano, got.AutoStop.At)
	require.NoError(t, err)
	require.True(t, deadline.Equal(parsed), "projection must preserve the full observed deadline")
}
