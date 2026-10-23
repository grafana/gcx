package remote_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/dynamic"
	"github.com/grafana/gcx/internal/resources/local"
	"github.com/grafana/gcx/internal/resources/process"
	"github.com/grafana/gcx/internal/resources/remote"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
)

func TestPullerPreservesSuccessfullyConvertedDashboard(t *testing.T) {
	for _, conversion := range []string{"successful", "failed", "unspecified"} {
		for _, selection := range []resources.FilterType{resources.FilterTypeAll, resources.FilterTypeSingle, resources.FilterTypeMultiple} {
			t.Run(fmt.Sprintf("%s/%s", conversion, selection), func(t *testing.T) {
				desc := resources.Descriptor{GroupVersion: schema.GroupVersion{Group: "dashboard.grafana.app", Version: "v2"}, Kind: "Dashboard", Plural: "dashboards", Singular: "dashboard"}
				status := map[string]any{"storedVersion": "v0alpha1"}
				if conversion != "unspecified" {
					status["failed"] = conversion == "failed"
				}
				obj := &unstructured.Unstructured{Object: map[string]any{
					"apiVersion": "dashboard.grafana.app/v2", "kind": "Dashboard",
					"metadata": map[string]any{"name": "sample", "namespace": "default"},
					"spec":     map[string]any{"title": "Sample", "layout": map[string]any{"kind": "AutoGridLayout"}},
					"status":   map[string]any{"conversion": status},
				}}
				legacy := obj.DeepCopy()
				legacy.SetAPIVersion("dashboard.grafana.app/v0alpha1")
				legacy.Object["spec"] = map[string]any{"title": "Sample", "panels": []any{}}
				fakeClient := fake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
					desc.GroupVersionResource(): "DashboardList",
					{Group: "dashboard.grafana.app", Version: "v0alpha1", Resource: "dashboards"}: "DashboardList",
				}, obj, legacy)
				client := dynamic.WrapNamespacedClient(dynamic.NewNamespacedClient("default", fakeClient))
				puller := remote.NewPuller(client, &mockPullRegistry{descriptors: resources.Descriptors{desc}})
				var filters resources.Filters
				if selection != resources.FilterTypeAll {
					filters = resources.Filters{{Type: selection, Descriptor: desc, ResourceUIDs: []string{"sample"}}}
				}
				var result resources.Resources
				_, err := puller.Pull(t.Context(), remote.PullRequest{Filters: filters, Resources: &result, StopOnError: true, Processors: []remote.Processor{&process.ServerFieldsStripper{}}})
				require.NoError(t, err)
				path := t.TempDir()
				writer := local.FSWriter{Path: path, Namer: local.GroupResourcesByKind("json", nil), Encoder: format.NewJSONCodec(), StopOnError: true}
				require.NoError(t, writer.Write(t.Context(), &result))
				wantVersion := "v2"
				if conversion == "failed" {
					wantVersion = "v0alpha1"
				}
				data, err := os.ReadFile(filepath.Join(path, "dashboards."+wantVersion+".dashboard.grafana.app", "sample.json"))
				require.NoError(t, err)
				var saved map[string]any
				require.NoError(t, json.Unmarshal(data, &saved))
				require.Equal(t, "dashboard.grafana.app/"+wantVersion, saved["apiVersion"])
				if conversion != "failed" {
					spec, ok := saved["spec"].(map[string]any)
					require.True(t, ok)
					require.Equal(t, map[string]any{"kind": "AutoGridLayout"}, spec["layout"])
				}
				for _, action := range fakeClient.Actions() {
					if conversion != "failed" {
						require.Equal(t, "v2", action.GetResource().Version, "successful conversion must not re-fetch a legacy version")
					}
				}
			})
		}
	}
}
