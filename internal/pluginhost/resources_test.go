//go:build !wasip1

package pluginhost_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	sdk "github.com/grafana/gcx/experimental/pluginsdk"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/pluginhost"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/client-go/rest"
)

func TestTypedResourcesUseHostTransport(t *testing.T) {
	t.Setenv("GCX_DISCOVERY_CACHE_DIR", t.TempDir())
	t.Setenv("GCX_OPENAPI_CACHE_DIR", t.TempDir())
	server, lists, forbidden := resourceServer(t)
	host, err := pluginhost.New(t.Context(), config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL, BearerToken: "fixture-token"}, Namespace: "test-ns", GrafanaURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	types, err := host.ListTypes(t.Context(), &sdk.ListTypesRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(types.GetTypes()) != 2 {
		t.Fatalf("types=%v", types.GetTypes())
	}
	filtered, err := host.ListTypes(t.Context(), &sdk.ListTypesRequest{Selectors: []string{"things"}})
	if err != nil || len(filtered.GetTypes()) != 1 {
		t.Fatalf("filtered=%v err=%v", filtered, err)
	}
	typ := filtered.GetTypes()[0]
	schemas, err := host.GetSchemas(t.Context(), &sdk.GetSchemasRequest{Types: []*sdk.ResourceType{typ}})
	if err != nil || len(schemas.GetSchemas()) != 1 {
		t.Fatalf("schemas=%v err=%v", schemas, err)
	}
	var spec map[string]any
	if err := json.Unmarshal(schemas.GetSchemas()[0].GetSpecJson(), &spec); err != nil {
		t.Fatal(err)
	}
	if spec["type"] != "object" {
		t.Fatal(spec)
	}
	result, err := host.ListResources(t.Context(), &sdk.ListResourcesRequest{Type: typ})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.GetItems()) != 2 || lists.Load() != 2 || len(result.GetFailures()) != 0 || result.GetTruncated() {
		t.Fatalf("result=%v list requests=%d", result, lists.Load())
	}
	var missing *sdk.Resource
	for _, item := range result.GetItems() {
		if item.GetMetadata().GetName() == "two" {
			missing = item
		}
	}
	if missing == nil || missing.GetKind() != "" || !strings.Contains(string(missing.GetObjectJson()), "9007199254740993") {
		t.Fatalf("missing-kind object changed: %v", missing)
	}
	before := lists.Load()
	if _, err := host.ListResources(t.Context(), &sdk.ListResourcesRequest{Type: &sdk.ResourceType{Group: "test.app", Version: "v1", Plural: "things", Kind: "Wrong"}}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("bad descriptor: %v", err)
	}
	if lists.Load() != before {
		t.Fatal("invalid type reached backend")
	}
	forbidden.Store(true)
	failed, err := host.ListResources(t.Context(), &sdk.ListResourcesRequest{Type: typ})
	if err != nil || len(failed.GetFailures()) != 1 || len(failed.GetItems()) != 0 {
		t.Fatalf("failure was hidden: %v %v", failed, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := host.ListResources(ctx, &sdk.ListResourcesRequest{Type: typ}); status.Code(err) != codes.Canceled {
		t.Fatalf("cancellation=%v", err)
	}
}

func resourceServer(t *testing.T) (*httptest.Server, *atomic.Int32, *atomic.Bool) {
	t.Helper()
	var lists atomic.Int32
	var forbidden atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer fixture-token" {
			t.Errorf("incorrect host request method or authentication")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api":
			fmt.Fprint(w, `{"kind":"APIVersions","versions":[]}`)
		case "/apis":
			fmt.Fprint(w, `{"kind":"APIGroupList","groups":[{"name":"test.app","versions":[{"groupVersion":"test.app/v1","version":"v1"}],"preferredVersion":{"groupVersion":"test.app/v1","version":"v1"}}]}`)
		case "/apis/test.app/v1":
			fmt.Fprint(w, `{"kind":"APIResourceList","groupVersion":"test.app/v1","resources":[{"name":"things","singularName":"thing","kind":"Thing","namespaced":true,"verbs":["get","list"]},{"name":"others","singularName":"other","kind":"Other","namespaced":true,"verbs":["get","list"]}]}`)
		case "/openapi/v3":
			fmt.Fprint(w, `{"paths":{"apis/test.app/v1":{"serverRelativeURL":"/openapi/v3/apis/test.app/v1"}}}`)
		case "/openapi/v3/apis/test.app/v1":
			fmt.Fprint(w, `{"components":{"schemas":{"Thing":{"x-kubernetes-group-version-kind":[{"group":"test.app","version":"v1","kind":"Thing"}],"properties":{"spec":{"type":"object","properties":{"title":{"type":"string"}}}}}}}}`)
		case "/apis/test.app/v1/namespaces/test-ns/things":
			lists.Add(1)
			if forbidden.Load() {
				w.WriteHeader(http.StatusForbidden)
				fmt.Fprint(w, `{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"Forbidden","message":"denied","code":403}`)
				return
			}
			if r.URL.Query().Get("continue") == "next" {
				fmt.Fprint(w, `{"apiVersion":"test.app/v1","kind":"ThingList","metadata":{},"items":[{"apiVersion":"test.app/v1","metadata":{"name":"two","generation":9007199254740993},"spec":{"title":"Two"}}]}`)
			} else {
				fmt.Fprint(w, `{"apiVersion":"test.app/v1","kind":"ThingList","metadata":{"continue":"next"},"items":[{"apiVersion":"test.app/v1","kind":"Thing","metadata":{"name":"one"},"spec":{"title":"One"}}]}`)
			}
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server, &lists, &forbidden
}
