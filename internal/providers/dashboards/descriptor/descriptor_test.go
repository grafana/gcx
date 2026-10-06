package descriptor_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/dashboards/descriptor"
	"github.com/grafana/gcx/internal/resources"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
)

func TestResolve(t *testing.T) {
	const group = "dashboard.grafana.app"
	const fallbackGroup = "v3.dashboard.grafana.app"
	versions := []string{"v0alpha1", "v1beta1", "v1", "v2"}
	groupVersions := make([]metav1.GroupVersionForDiscovery, 0, len(versions))
	for _, version := range versions {
		groupVersions = append(groupVersions, metav1.GroupVersionForDiscovery{
			GroupVersion: group + "/" + version,
			Version:      version,
		})
	}
	fallbackVersion := metav1.GroupVersionForDiscovery{GroupVersion: fallbackGroup + "/v1", Version: "v1"}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var response any
		switch r.URL.Path {
		case "/api":
			response = metav1.APIVersions{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "APIVersions"},
				Versions: []string{},
			}
		case "/apis":
			response = metav1.APIGroupList{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "APIGroupList"},
				Groups: []metav1.APIGroup{
					{
						Name:             group,
						Versions:         groupVersions,
						PreferredVersion: groupVersions[len(groupVersions)-1],
					},
					{
						// Explicit group/version input must not fall back to this group.
						Name:             fallbackGroup,
						Versions:         []metav1.GroupVersionForDiscovery{fallbackVersion},
						PreferredVersion: fallbackVersion,
					},
				},
			}
		default:
			version, ok := strings.CutPrefix(r.URL.Path, "/apis/"+group+"/")
			resourceGroup := group
			if !ok {
				version, ok = strings.CutPrefix(r.URL.Path, "/apis/"+fallbackGroup+"/")
				resourceGroup = fallbackGroup
			}
			if !ok {
				http.NotFound(w, r)
				return
			}
			response = metav1.APIResourceList{
				TypeMeta:     metav1.TypeMeta{APIVersion: "v1", Kind: "APIResourceList"},
				GroupVersion: resourceGroup + "/" + version,
				APIResources: []metav1.APIResource{{
					Name:         "dashboards",
					SingularName: "dashboard",
					Namespaced:   true,
					Kind:         "Dashboard",
					Verbs:        metav1.Verbs{"get", "list"},
				}},
			}
		}
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Errorf("encode discovery response: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	cfg := config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}, Namespace: "default"}

	tests := []struct {
		name        string
		apiVersion  string
		wantVersion string
		wantErr     string
	}{
		{name: "preferred default", wantVersion: "v2"},
		{name: "bare stable", apiVersion: "v1", wantVersion: "v1"},
		{name: "bare alpha", apiVersion: "v0alpha1", wantVersion: "v0alpha1"},
		{name: "bare beta", apiVersion: "v1beta1", wantVersion: "v1beta1"},
		{name: "qualified stable", apiVersion: group + "/v1", wantVersion: "v1"},
		{name: "qualified beta", apiVersion: group + "/v1beta1", wantVersion: "v1beta1"},
		{name: "unsupported bare", apiVersion: "v3", wantErr: "server does not support dashboards resource"},
		{name: "unsupported qualified", apiVersion: group + "/v3", wantErr: "server does not support dashboards resource"},
		{name: "unsupported group", apiVersion: "other.grafana.app/v1", wantErr: "server does not support dashboards resource"},
		{name: "invalid group version", apiVersion: group + "/v1/extra", wantErr: "invalid --api-version"},
		{name: "qualified empty version", apiVersion: group + "/", wantErr: "invalid --api-version"},
		{name: "empty group and version", apiVersion: "/", wantErr: "invalid --api-version"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("GCX_DISCOVERY_CACHE_DIR", t.TempDir())
			got, err := descriptor.Resolve(t.Context(), cfg, tt.apiVersion)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, resources.Descriptor{
				GroupVersion: schema.GroupVersion{Group: group, Version: tt.wantVersion},
				Kind:         "Dashboard",
				Singular:     "dashboard",
				Plural:       "dashboards",
			}, got)
		})
	}
}
