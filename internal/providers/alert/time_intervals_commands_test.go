package alert //nolint:testpackage // Tests inject native bindings into private command factories.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/native"
	"github.com/grafana/gcx/internal/resources/discovery"
	"github.com/grafana/gcx/internal/retry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/rest"
)

type intervalTestLoader struct {
	cfg   config.NamespacedRESTConfig
	calls int
}

func (l *intervalTestLoader) LoadGrafanaConfig(context.Context) (config.NamespacedRESTConfig, error) {
	l.calls++
	return l.cfg, nil
}

type intervalDiscovery struct {
	groups    []*metav1.APIGroup
	resources []*metav1.APIResourceList
}

func (d intervalDiscovery) ServerGroupsAndResources() ([]*metav1.APIGroup, []*metav1.APIResourceList, error) {
	return d.groups, d.resources, nil
}

type intervalTestAPI struct {
	mu             sync.Mutex
	items          []unstructured.Unstructured
	requests       []string
	bodies         []*unstructured.Unstructured
	status         int
	mutationStatus int
	paginate       bool
}

func intervalObject(title, name, version string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{"apiVersion": routingTreesGroup + "/v1beta1", "kind": timeIntervalKind, "metadata": map[string]any{"name": name, "namespace": "default", "resourceVersion": version, "uid": "source-uid", "annotations": map[string]any{provenanceAnnotation: "api"}}, "spec": map[string]any{"name": title, "time_intervals": []any{map[string]any{"weekdays": []any{"saturday"}}}, "future": "preserve"}}}
}
func intervalJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
func intervalStatus(w http.ResponseWriter, status int) {
	reason := metav1.StatusReasonUnknown
	switch status {
	case 401:
		reason = metav1.StatusReasonUnauthorized
	case 403:
		reason = metav1.StatusReasonForbidden
	case 404:
		reason = metav1.StatusReasonNotFound
	case 409:
		reason = metav1.StatusReasonConflict
	}
	intervalJSON(w, status, metav1.Status{TypeMeta: metav1.TypeMeta{Kind: "Status", APIVersion: "v1"}, Status: metav1.StatusFailure, Reason: reason, Message: fmt.Sprintf("native status %d", status), Code: int32(status)}) //nolint:gosec // HTTP statuses fit int32.
}

//nolint:nestif // The recording fixture models HTTP methods, title conflicts and optimistic concurrency.
func (a *intervalTestAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.requests = append(a.requests, r.Method+" "+r.URL.RequestURI())
	if a.status != 0 {
		intervalStatus(w, a.status)
		return
	}
	parts := strings.Split(r.URL.Path, "/")
	name := ""
	if len(parts) == 8 {
		name = parts[7]
	}
	if r.Method == http.MethodGet {
		if name != "" {
			for _, obj := range a.items {
				if obj.GetName() == name {
					intervalJSON(w, 200, &obj)
					return
				}
			}
			intervalStatus(w, 404)
			return
		}
		items := a.items
		if items == nil {
			items = []unstructured.Unstructured{}
		}
		more := ""
		if a.paginate && len(items) > 1 {
			if r.URL.Query().Get("continue") == "page2" {
				items = items[1:]
			} else {
				items = items[:1]
				more = "page2"
			}
		}
		intervalJSON(w, 200, map[string]any{"apiVersion": routingTreesGroup + "/" + parts[3], "kind": "TimeIntervalList", "metadata": map[string]any{"continue": more}, "items": items})
		return
	}
	if r.Method == http.MethodPost || r.Method == http.MethodPut {
		obj := &unstructured.Unstructured{}
		if err := json.NewDecoder(r.Body).Decode(&obj.Object); err != nil {
			intervalStatus(w, 400)
			return
		}
		a.bodies = append(a.bodies, obj.DeepCopy())
		if a.mutationStatus != 0 {
			intervalStatus(w, a.mutationStatus)
			return
		}
		title, _, _ := unstructured.NestedString(obj.Object, "spec", "name")
		current := -1
		for i, item := range a.items {
			if item.GetName() == name {
				current = i
			}
			existing, _, _ := unstructured.NestedString(item.Object, "spec", "name")
			if existing == title && (r.Method == http.MethodPost || item.GetName() != name) {
				intervalStatus(w, 409)
				return
			}
		}
		if r.Method == http.MethodPut {
			if current < 0 {
				intervalStatus(w, 404)
				return
			}
			if obj.GetResourceVersion() != a.items[current].GetResourceVersion() {
				intervalStatus(w, 409)
				return
			}
		}
		obj.SetName("derived-" + title)
		obj.SetResourceVersion("8")
		if current < 0 {
			a.items = append(a.items, *obj)
		} else {
			a.items[current] = *obj
		}
		intervalJSON(w, 200, obj)
		return
	}
	if r.Method == http.MethodDelete {
		if a.mutationStatus != 0 {
			intervalStatus(w, a.mutationStatus)
			return
		}
		for i, item := range a.items {
			if item.GetName() == name {
				a.items = append(a.items[:i], a.items[i+1:]...)
				intervalJSON(w, 200, metav1.Status{TypeMeta: metav1.TypeMeta{Kind: "Status", APIVersion: "v1"}, Status: metav1.StatusSuccess})
				return
			}
		}
		intervalStatus(w, 404)
		return
	}
	intervalStatus(w, 405)
}
func intervalBinding(t *testing.T, api *intervalTestAPI, preferred string) (native.Binding, *intervalTestLoader) {
	t.Helper()
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)
	loader := &intervalTestLoader{cfg: config.NamespacedRESTConfig{Config: rest.Config{Host: srv.URL, WrapTransport: func(rt http.RoundTripper) http.RoundTripper { return &retry.Transport{Base: rt} }}, Namespace: "default"}}
	group := &metav1.APIGroup{Name: routingTreesGroup}
	lists := make([]*metav1.APIResourceList, 0, 2)
	for _, version := range []string{"v0alpha1", "v1beta1"} {
		v := metav1.GroupVersionForDiscovery{GroupVersion: routingTreesGroup + "/" + version, Version: version}
		group.Versions = append(group.Versions, v)
		if version == preferred {
			group.PreferredVersion = v
		}
		lists = append(lists, &metav1.APIResourceList{GroupVersion: v.GroupVersion, APIResources: []metav1.APIResource{{Name: "timeintervals", SingularName: "timeinterval", Kind: timeIntervalKind, Namespaced: true}}})
	}
	binding := native.Bind(loader, native.Config{Group: routingTreesGroup, Resource: "timeintervals"}, native.WithRegistry(func(ctx context.Context, _ config.NamespacedRESTConfig) (*discovery.Registry, error) {
		return discovery.NewCachedRegistry(ctx, intervalDiscovery{groups: []*metav1.APIGroup{group}, resources: lists})
	}))
	return binding, loader
}
func intervalRun(t *testing.T, binding native.Binding, stdin string, args ...string) (string, error) {
	t.Helper()
	cmd := newTimeIntervalsCommand(binding)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	var out, diag bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&diag)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}
func intervalEnv(t *testing.T) {
	t.Helper()
	agent.SetFlag(false)
	t.Cleanup(agent.ResetForTesting)
	t.Setenv("GCX_AUTO_APPROVE", "false")
}
func intervalManifest(t *testing.T, obj *unstructured.Unstructured) string {
	t.Helper()
	b, err := json.Marshal(obj)
	require.NoError(t, err)
	return string(b)
}

func TestTimeIntervalsReads(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		items     []unstructured.Unstructured
		preferred string
		want      string
		wantErr   string
		methods   []string
	}{
		{name: "complete paginated list", args: []string{"list", "-o", "json"}, items: []unstructured.Unstructured{*intervalObject("weekends", "opaque-first", "7"), *intervalObject("holidays", "opaque-second", "4")}, want: "opaque-second", methods: []string{"GET", "GET"}},
		{name: "empty items", args: []string{"list", "-o", "json"}, want: `"items": []`, methods: []string{"GET"}},
		{name: "table", args: []string{"list", "-o", "wide"}, items: []unstructured.Unstructured{*intervalObject("weekends", "opaque-first", "7")}, want: "opaque-first", methods: []string{"GET"}},
		{name: "get table", args: []string{"get", "weekends", "-o", "table"}, items: []unstructured.Unstructured{*intervalObject("weekends", "opaque-first", "7")}, want: "opaque-first", methods: []string{"GET", "GET"}},
		{name: "get wide", args: []string{"get", "weekends", "-o", "wide"}, items: []unstructured.Unstructured{*intervalObject("weekends", "opaque-first", "7")}, want: "PROVENANCE", methods: []string{"GET", "GET"}},
		{name: "title lookup preserves native", args: []string{"get", "weekends", "-o", "json"}, items: []unstructured.Unstructured{*intervalObject("weekends", "opaque-first", "7")}, want: "preserve", methods: []string{"GET", "GET"}},
		{name: "case sensitive", args: []string{"get", "Weekends"}, items: []unstructured.Unstructured{*intervalObject("weekends", "opaque-first", "7")}, wantErr: "not found", methods: []string{"GET"}},
		{name: "encoded name not title", args: []string{"get", "opaque-first"}, items: []unstructured.Unstructured{*intervalObject("weekends", "opaque-first", "7")}, wantErr: "not found", methods: []string{"GET"}},
		{name: "duplicates", args: []string{"get", "weekends"}, items: []unstructured.Unstructured{*intervalObject("weekends", "one", "7"), *intervalObject("weekends", "two", "4")}, wantErr: "ambiguous", methods: []string{"GET", "GET"}},
		{name: "old preferred", args: []string{"list", "-o", "json"}, preferred: "v0alpha1", want: "v0alpha1", methods: []string{"GET"}},
		{name: "explicit old", args: []string{"list", "--api-version", routingTreesGroup + "/v0alpha1"}, methods: []string{"GET"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			intervalEnv(t)
			api := &intervalTestAPI{items: tc.items, paginate: true}
			preferred := tc.preferred
			if preferred == "" {
				preferred = "v1beta1"
			}
			binding, _ := intervalBinding(t, api, preferred)
			out, err := intervalRun(t, binding, "", tc.args...)
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
			} else {
				require.NoError(t, err)
				assert.Contains(t, out, tc.want)
			}
			require.Len(t, api.requests, len(tc.methods))
			for i, m := range tc.methods {
				assert.True(t, strings.HasPrefix(api.requests[i], m+" "), api.requests[i])
			}
			if tc.name == "title lookup preserves native" {
				var obj unstructured.Unstructured
				require.NoError(t, json.Unmarshal([]byte(out), &obj.Object))
				assert.Equal(t, tc.items[0].Object, obj.Object)
			}
			if tc.name == "get table" || tc.name == "get wide" {
				assert.Contains(t, out, "TITLE")
				assert.Contains(t, out, "API_NAME")
				assert.Contains(t, api.requests[1], "/timeintervals/opaque-first")
			}
			if tc.name == "table" {
				assert.Contains(t, out, "TITLE")
				assert.Contains(t, out, "API_NAME")
				assert.Contains(t, out, "PROVENANCE")
			}
			if tc.name == "explicit old" {
				assert.Contains(t, api.requests[0], "/v0alpha1/")
			}
		})
	}
}

func TestTimeIntervalsWrites(t *testing.T) {
	tests := []struct {
		name, verb, subject, title, apiName, rv string
		items                                   []unstructured.Unstructured
		status                                  int
		wantErr                                 string
		wantMutations                           int
	}{
		{name: "create strips source", verb: "create", title: "holidays", apiName: "source-address", rv: "source-version", wantMutations: 1},
		{name: "create conflict", verb: "create", title: "weekends", wantErr: "409", wantMutations: 1},
		{name: "ordinary update", verb: "update", subject: "weekends", title: "weekends", apiName: "old-address", rv: "7", wantMutations: 1},
		{name: "unnamed update", verb: "update", subject: "weekends", title: "weekends", rv: "7", wantMutations: 1},
		{name: "explicit rename", verb: "update", subject: "weekends", title: "holidays", apiName: "old-address", rv: "7", wantMutations: 1},
		{name: "stale caller version", verb: "update", subject: "weekends", title: "weekends", apiName: "old-address", rv: "stale", wantMutations: 1, wantErr: "409"},
		{name: "rename conflict", verb: "update", subject: "weekends", title: "holidays", apiName: "old-address", rv: "7", status: 409, wantMutations: 1, wantErr: "409"},
		{name: "missing update target", verb: "update", subject: "missing", title: "missing", rv: "7", wantErr: "not found"},
		{name: "changed subject retained name", verb: "update", subject: "holidays", title: "holidays", apiName: "old-address", rv: "7", items: []unstructured.Unstructured{*intervalObject("weekends", "old-address", "7"), *intervalObject("holidays", "other-address", "7")}, wantErr: "does not match"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			intervalEnv(t)
			items := tc.items
			if items == nil {
				items = []unstructured.Unstructured{*intervalObject("weekends", "old-address", "7")}
			}
			api := &intervalTestAPI{items: items, mutationStatus: tc.status}
			binding, _ := intervalBinding(t, api, "v1beta1")
			obj := intervalObject(tc.title, tc.apiName, tc.rv)
			obj.SetGeneration(8)
			obj.SetGenerateName("source-")
			obj.SetLabels(map[string]string{"portable": "yes"})
			input := intervalManifest(t, obj)
			args := []string{tc.verb}
			if tc.verb == "update" {
				args = append(args, tc.subject)
			}
			args = append(args, "-f", "-", "-o", "json")
			out, err := intervalRun(t, binding, input, args...)
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
			} else {
				require.NoError(t, err)
				var receipt nativeAlertMutationResult
				require.NoError(t, json.Unmarshal([]byte(out), &receipt))
				assert.Equal(t, "derived-"+tc.title, receipt.Target.Name)
				assert.Equal(t, tc.title, receipt.Title)
				assert.Equal(t, "8", receipt.ResourceVersion)
				if tc.name == "explicit rename" {
					assert.Equal(t, "weekends", receipt.PreviousTitle)
				}
			}
			require.Len(t, api.bodies, tc.wantMutations)
			if tc.wantMutations > 0 {
				body := api.bodies[0]
				if tc.verb == "create" {
					assert.Empty(t, body.GetName())
					assert.Empty(t, body.GetResourceVersion())
					assert.Empty(t, body.GetUID())
					assert.Zero(t, body.GetGeneration())
					assert.Empty(t, body.GetGenerateName())
					assert.Equal(t, "yes", body.GetLabels()["portable"])
				} else {
					assert.Equal(t, "old-address", body.GetName())
					assert.Equal(t, tc.rv, body.GetResourceVersion())
					assert.Contains(t, api.requests[len(api.requests)-1], "PUT ")
					assert.Contains(t, api.requests[len(api.requests)-1], "/old-address")
				}
			}
			assert.Equal(t, input, intervalManifest(t, obj), "caller manifest must remain unchanged")
		})
	}
}

func TestTimeIntervalsValidateBeforeLoad(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		modify func(*unstructured.Unstructured)
	}{
		{name: "empty API flag", args: []string{"list", "--api-version="}},
		{name: "wrong version group", args: []string{"list", "--api-version", "other.example/v1"}},
		{name: "empty filename", args: []string{"create", "-f", ""}},
		{name: "empty title", args: []string{"get", ""}},
		{name: "blank filename", args: []string{"create", "-f", " "}},
		{name: "nonstring name", args: []string{"create", "-f", "-"}, modify: func(o *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(o.Object, int64(4), "metadata", "name")
		}},
		{name: "nonstring version", args: []string{"update", "weekends", "-f", "-"}, modify: func(o *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(o.Object, int64(4), "metadata", "resourceVersion")
		}},
		{name: "wrong group", args: []string{"create", "-f", "-"}, modify: func(o *unstructured.Unstructured) { o.SetAPIVersion("other.example/v1beta1") }},
		{name: "wrong kind", args: []string{"create", "-f", "-"}, modify: func(o *unstructured.Unstructured) { o.SetKind("RoutingTree") }},
		{name: "missing title", args: []string{"create", "-f", "-"}, modify: func(o *unstructured.Unstructured) { unstructured.RemoveNestedField(o.Object, "spec", "name") }},
		{name: "malformed title", args: []string{"create", "-f", "-"}, modify: func(o *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(o.Object, int64(2), "spec", "name")
		}},
		{name: "missing update RV", args: []string{"update", "weekends", "-f", "-"}, modify: func(o *unstructured.Unstructured) { o.SetResourceVersion("") }},
		{name: "conflicting API versions", args: []string{"create", "-f", "-", "--api-version", "v0alpha1"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			intervalEnv(t)
			loads := 0
			binding := native.Func(func(context.Context, native.LoadOptions) (native.Access, error) {
				loads++
				return native.Access{}, errors.New("unexpected load")
			})
			obj := intervalObject("weekends", "old-address", "7")
			if tc.modify != nil {
				tc.modify(obj)
			}
			_, err := intervalRun(t, binding, intervalManifest(t, obj), tc.args...)
			require.Error(t, err)
			assert.Zero(t, loads)
			assert.Equal(t, gcxerrors.ExitUsageError, *fail.ErrorToDetailedError(err).ExitCode)
		})
	}
}

func TestTimeIntervalsDeleteConfirmation(t *testing.T) {
	tests := []struct {
		name, stdin  string
		agent, force bool
		wantErr      string
		wantLoad     bool
	}{
		{name: "forced", force: true, wantLoad: true},
		{name: "confirmed", stdin: "yes\n", wantLoad: true},
		{name: "declined", stdin: "no\n", wantErr: "delete cancelled"},
		{name: "closed stdin", wantErr: "--force"},
		{name: "agent requires force", agent: true, wantErr: "--force"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			intervalEnv(t)
			if tc.agent {
				agent.SetFlag(true)
			}
			api := &intervalTestAPI{items: []unstructured.Unstructured{*intervalObject("weekends", "old-address", "7")}}
			binding, loader := intervalBinding(t, api, "v1beta1")
			args := []string{"delete", "weekends", "-o", "json"}
			if tc.force {
				args = append(args, "--force")
			}
			out, err := intervalRun(t, binding, tc.stdin, args...)
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
			} else {
				require.NoError(t, err)
				assert.Contains(t, out, `"name": "old-address"`)
				assert.Contains(t, out, `"title": "weekends"`)
			}
			if tc.wantLoad {
				assert.Len(t, api.requests, 2)
				assert.Empty(t, api.items)
			} else {
				assert.Empty(t, api.requests)
				assert.Zero(t, loader.calls)
			}
			if tc.name == "declined" {
				assert.Equal(t, gcxerrors.ExitCancelled, *fail.ErrorToDetailedError(err).ExitCode)
			}
			if tc.agent {
				assert.ErrorIs(t, err, providers.ErrAgentModeRequiresForce)
			}
		})
	}
}

func TestTimeIntervalsFileInputUnchanged(t *testing.T) {
	intervalEnv(t)
	api := &intervalTestAPI{items: []unstructured.Unstructured{*intervalObject("weekends", "old-address", "7")}}
	binding, _ := intervalBinding(t, api, "v1beta1")
	input := []byte(intervalManifest(t, intervalObject("holidays", "old-address", "7")))
	path := filepath.Join(t.TempDir(), "interval.json")
	require.NoError(t, os.WriteFile(path, input, 0600))
	out, err := intervalRun(t, binding, "", "update", "weekends", "-f", path)
	require.NoError(t, err)
	assert.Contains(t, out, `"weekends" renamed to "holidays"`)
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, input, after)
}

func TestTimeIntervalsPreserveServerErrors(t *testing.T) {
	for _, status := range []int{401, 403, 404, 409, 500, 501} {
		for _, verb := range []string{"list", "get", "create", "update", "delete"} {
			t.Run(fmt.Sprintf("%d/%s", status, verb), func(t *testing.T) {
				intervalEnv(t)
				api := &intervalTestAPI{items: []unstructured.Unstructured{*intervalObject("weekends", "old-address", "7")}}
				if verb == "create" || verb == "update" || verb == "delete" {
					api.mutationStatus = status
				} else {
					api.status = status
				}
				binding, _ := intervalBinding(t, api, "v1beta1")
				args := []string{verb}
				if verb == "get" || verb == "update" || verb == "delete" {
					args = append(args, "weekends")
				}
				if verb == "create" || verb == "update" {
					args = append(args, "-f", "-")
				}
				if verb == "delete" {
					args = append(args, "--force")
				}
				_, err := intervalRun(t, binding, intervalManifest(t, intervalObject("holidays", "old-address", "7")), args...)
				require.Error(t, err)
				assert.Contains(t, err.Error(), fmt.Sprintf("native status %d", status))
				if verb == "create" {
					assert.Len(t, api.bodies, 1)
				}
				if verb == "update" {
					assert.Len(t, api.bodies, 1)
				}
			})
		}
	}
}

func TestTimeIntervalsBindingCancellation(t *testing.T) {
	intervalEnv(t)
	for _, err := range []error{context.Canceled, context.DeadlineExceeded, errors.New("discovery unavailable")} {
		t.Run(err.Error(), func(t *testing.T) {
			binding := native.Func(func(context.Context, native.LoadOptions) (native.Access, error) { return native.Access{}, err })
			_, got := intervalRun(t, binding, "", "list")
			assert.ErrorIs(t, got, err)
		})
	}
}

func TestTimeIntervalsManifestSelectsVersion(t *testing.T) {
	for _, version := range []string{"v0alpha1", "v1beta1"} {
		t.Run(version, func(t *testing.T) {
			intervalEnv(t)
			api := &intervalTestAPI{}
			binding, _ := intervalBinding(t, api, "v1beta1")
			obj := intervalObject("new", "source-name", "source-version")
			obj.SetAPIVersion(routingTreesGroup + "/" + version)
			_, err := intervalRun(t, binding, intervalManifest(t, obj), "create", "-f", "-", "--api-version", version)
			require.NoError(t, err)
			require.Len(t, api.requests, 1)
			assert.Contains(t, api.requests[0], "/"+version+"/")
			require.Len(t, api.bodies, 1)
			assert.Equal(t, routingTreesGroup+"/"+version, api.bodies[0].GetAPIVersion())
		})
	}
}

func TestTimeIntervalsYAMLFileAndPipeRoundTrip(t *testing.T) {
	intervalEnv(t)
	api := &intervalTestAPI{}
	binding, _ := intervalBinding(t, api, "v1beta1")
	input := []byte(`apiVersion: notifications.alerting.grafana.app/v1beta1
kind: TimeInterval
metadata:
  name: source-name
  resourceVersion: "source-version"
  generation: 8
  annotations:
    grafana.com/provenance: api
spec:
  name: weekends
  time_intervals:
    - times:
        - start_time: "09:00"
          end_time: "17:00"
      days_of_month:
        - "1:5"
`)
	path := filepath.Join(t.TempDir(), "interval.yaml")
	require.NoError(t, os.WriteFile(path, input, 0600))
	_, err := intervalRun(t, binding, "", "create", "-f", path)
	require.NoError(t, err)
	require.Len(t, api.bodies, 1)
	assert.Empty(t, api.bodies[0].GetName())
	assert.Zero(t, api.bodies[0].GetGeneration())
	fetched, err := intervalRun(t, binding, "", "get", "weekends", "-o", "yaml")
	require.NoError(t, err)
	_, err = intervalRun(t, binding, fetched, "update", "weekends", "-f", "-")
	require.NoError(t, err)
	require.Len(t, api.bodies, 2)
	assert.Equal(t, "derived-weekends", api.bodies[1].GetName())
	assert.Equal(t, "8", api.bodies[1].GetResourceVersion())
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, input, after)
}
