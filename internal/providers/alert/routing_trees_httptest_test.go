package alert_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/providers/native"
	"github.com/grafana/gcx/internal/resources/discovery"
	"github.com/grafana/gcx/internal/retry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

const rtCollectionPath = "/apis/" + rtGroup + "/v1beta1/namespaces/default/routingtrees"

// fakeNativeAPI stands in for Grafana's native routing-trees API.
type fakeNativeAPI struct {
	mu       sync.Mutex
	trees    map[string]map[string]any
	requests []string
	// status, when non-zero, answers every request with that code.
	status int
}

func (f *fakeNativeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, r.Method+" "+r.URL.Path)

	if f.status != 0 {
		writeNativeStatus(w, f.status, http.StatusText(f.status))
		return
	}

	name := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, rtCollectionPath), "/")
	switch {
	case r.Method == http.MethodGet && name == "":
		items := make([]any, 0, len(f.trees))
		for _, n := range []string{"user-defined", "team-a", "team-c"} {
			if t, ok := f.trees[n]; ok {
				items = append(items, t)
			}
		}
		writeNativeJSON(w, http.StatusOK, map[string]any{"apiVersion": rtGroup + "/v1beta1", "kind": "RoutingTreeList", "metadata": map[string]any{}, "items": items})
	case r.Method == http.MethodGet:
		t, ok := f.trees[name]
		if !ok {
			writeNativeStatus(w, http.StatusNotFound, "routingtrees \""+name+"\" not found")
			return
		}
		writeNativeJSON(w, http.StatusOK, t)
	case r.Method == http.MethodPost:
		obj := decodeBody(r)
		meta, _ := obj["metadata"].(map[string]any)
		n, _ := meta["name"].(string)
		if _, ok := f.trees[n]; ok {
			writeNativeStatus(w, http.StatusConflict, "routingtrees \""+n+"\" already exists")
			return
		}
		meta["resourceVersion"] = "1"
		f.trees[n] = obj
		writeNativeJSON(w, http.StatusCreated, obj)
	case r.Method == http.MethodPut:
		obj := decodeBody(r)
		meta, _ := obj["metadata"].(map[string]any)
		current, _ := f.trees[name]["metadata"].(map[string]any)
		if meta["resourceVersion"] != current["resourceVersion"] {
			writeNativeStatus(w, http.StatusConflict, "the object has been modified")
			return
		}
		meta["resourceVersion"] = "2"
		f.trees[name] = obj
		writeNativeJSON(w, http.StatusOK, obj)
	case r.Method == http.MethodDelete:
		delete(f.trees, name)
		writeNativeJSON(w, http.StatusOK, map[string]any{"kind": "Status", "apiVersion": "v1", "status": "Success"})
	default:
		writeNativeStatus(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func decodeBody(r *http.Request) map[string]any {
	var obj map[string]any
	body, _ := io.ReadAll(r.Body)
	_ = json.Unmarshal(body, &obj)
	return obj
}

func writeNativeJSON(w http.ResponseWriter, code int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write(body)
}

func writeNativeStatus(w http.ResponseWriter, code int, msg string) {
	reason := metav1.StatusReasonUnknown
	switch code {
	case http.StatusNotFound:
		reason = metav1.StatusReasonNotFound
	case http.StatusConflict:
		reason = metav1.StatusReasonConflict
	case http.StatusForbidden:
		reason = metav1.StatusReasonForbidden
	}
	writeNativeJSON(w, code, metav1.Status{
		TypeMeta: metav1.TypeMeta{Kind: "Status", APIVersion: "v1"},
		Status:   metav1.StatusFailure,
		Message:  msg,
		Reason:   reason,
		Code:     int32(code), //nolint:gosec // HTTP status codes fit in int32.
	})
}

type staticLoader struct{ cfg config.NamespacedRESTConfig }

func (l staticLoader) LoadGrafanaConfig(context.Context) (config.NamespacedRESTConfig, error) {
	return l.cfg, nil
}

// bindFakeNativeAPI binds routing trees to api through the real native
// binding: real dynamic client and HTTP, discovery from a static fixture.
func bindFakeNativeAPI(t *testing.T, api *fakeNativeAPI) native.Binding {
	t.Helper()
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)

	group := &metav1.APIGroup{
		Name:     rtGroup,
		Versions: []metav1.GroupVersionForDiscovery{{GroupVersion: rtGroup + "/v1beta1", Version: "v1beta1"}},
	}
	group.PreferredVersion = group.Versions[0]
	disco := &staticDiscovery{
		groups: []*metav1.APIGroup{group},
		resources: []*metav1.APIResourceList{{
			GroupVersion: rtGroup + "/v1beta1",
			APIResources: []metav1.APIResource{{Name: "routingtrees", SingularName: "routingtree", Kind: "RoutingTree", Namespaced: true}},
		}},
	}

	// Wrap with the production retry transport so the single-request
	// assertions cover what the CLI really sends.
	restCfg := rest.Config{Host: srv.URL, WrapTransport: func(rt http.RoundTripper) http.RoundTripper {
		return &retry.Transport{Base: rt}
	}}
	loader := staticLoader{cfg: config.NamespacedRESTConfig{Config: restCfg, Namespace: "default"}}
	return native.Bind(loader, native.Config{Group: rtGroup, Resource: "routingtrees"},
		native.WithRegistry(func(ctx context.Context, _ config.NamespacedRESTConfig) (*discovery.Registry, error) {
			return discovery.NewCachedRegistry(ctx, disco)
		}))
}

type staticDiscovery struct {
	groups    []*metav1.APIGroup
	resources []*metav1.APIResourceList
}

func (d *staticDiscovery) ServerGroupsAndResources() ([]*metav1.APIGroup, []*metav1.APIResourceList, error) {
	return d.groups, d.resources, nil
}

func newFakeNativeAPI() *fakeNativeAPI {
	return &fakeNativeAPI{trees: map[string]map[string]any{
		"user-defined": {
			"apiVersion": rtGroup + "/v1beta1",
			"kind":       "RoutingTree",
			"metadata":   map[string]any{"name": "user-defined", "namespace": "default", "resourceVersion": "7"},
			"spec":       map[string]any{"defaults": map[string]any{"receiver": "grafana-default-email"}, "routes": []any{}},
		},
		"team-a": {
			"apiVersion": rtGroup + "/v1beta1",
			"kind":       "RoutingTree",
			"metadata":   map[string]any{"name": "team-a", "namespace": "default", "resourceVersion": "3"},
			"spec":       map[string]any{"defaults": map[string]any{"receiver": "team-a-email"}, "routes": []any{}},
		},
	}}
}

func TestRoutingTrees_NativeAPI(t *testing.T) {
	tests := []struct {
		name         string
		status       int
		stdin        string
		args         []string
		wantCode     int
		wantOut      string
		wantMessage  string
		wantRequests []string
	}{
		{
			name:         "list",
			args:         []string{"list"},
			wantOut:      "team-a-email",
			wantRequests: []string{"GET " + rtCollectionPath},
		},
		{
			name:         "get",
			args:         []string{"get", "team-a"},
			wantOut:      "resourceVersion: \"3\"",
			wantRequests: []string{"GET " + rtCollectionPath + "/team-a"},
		},
		{
			name:         "create",
			stdin:        newTreeYAML,
			args:         []string{"create", "-f", "-"},
			wantOut:      `routing tree "team-c" created`,
			wantRequests: []string{"POST " + rtCollectionPath},
		},
		{
			name:         "create conflict",
			stdin:        manifestYAML(rtGroup+"/v1beta1", "RoutingTree", "team-a", ""),
			args:         []string{"create", "-f", "-"},
			wantCode:     gcxerrors.ExitGeneralError,
			wantMessage:  `routingtrees "team-a" already exists`,
			wantRequests: []string{"POST " + rtCollectionPath},
		},
		{
			name:         "update",
			stdin:        manifestYAML(rtGroup+"/v1beta1", "RoutingTree", "team-a", "3"),
			args:         []string{"update", "team-a", "-f", "-"},
			wantOut:      `routing tree "team-a" updated`,
			wantRequests: []string{"PUT " + rtCollectionPath + "/team-a"},
		},
		{
			name:         "update stale version",
			stdin:        manifestYAML(rtGroup+"/v1beta1", "RoutingTree", "team-a", "1"),
			args:         []string{"update", "team-a", "-f", "-"},
			wantCode:     gcxerrors.ExitGeneralError,
			wantMessage:  "the object has been modified",
			wantRequests: []string{"PUT " + rtCollectionPath + "/team-a"},
		},
		{
			name:         "delete",
			args:         []string{"delete", "team-a", "--force"},
			wantOut:      `routing tree "team-a" deleted`,
			wantRequests: []string{"DELETE " + rtCollectionPath + "/team-a"},
		},
		{
			name:         "403 maps to auth failure",
			status:       http.StatusForbidden,
			args:         []string{"list"},
			wantCode:     gcxerrors.ExitAuthFailure,
			wantMessage:  "Forbidden",
			wantRequests: []string{"GET " + rtCollectionPath},
		},
		{
			name:         "501 named trees unsupported: one request, nothing else",
			status:       http.StatusNotImplemented,
			stdin:        newTreeYAML,
			args:         []string{"create", "-f", "-"},
			wantCode:     gcxerrors.ExitGeneralError,
			wantMessage:  "Not Implemented",
			wantRequests: []string{"POST " + rtCollectionPath},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setAgentMode(t, false)
			api := newFakeNativeAPI()
			api.status = tc.status

			res := runRoutingTrees(t, bindFakeNativeAPI(t, api), tc.stdin, tc.args...)
			assert.Equal(t, tc.wantCode, exitCodeOf(res.err), "err: %v", res.err)
			assert.Equal(t, tc.wantRequests, api.requests)
			if tc.wantOut != "" {
				assert.Contains(t, res.stdout, tc.wantOut)
			}
			if tc.wantMessage != "" {
				// The converted error keeps the server's response as Parent,
				// which is what the CLI renders.
				detailed := fail.ErrorToDetailedError(res.err)
				require.NotNil(t, detailed)
				require.Error(t, detailed.Parent)
				assert.Contains(t, detailed.Parent.Error(), tc.wantMessage)
			}
		})
	}
}
