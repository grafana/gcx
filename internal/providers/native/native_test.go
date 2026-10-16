package native_test

import (
	"context"
	"errors"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/native"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/discovery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

const testGroup = "notifications.alerting.grafana.app"

type fakeLoader struct {
	calls int
	err   error
}

func (l *fakeLoader) LoadGrafanaConfig(context.Context) (config.NamespacedRESTConfig, error) {
	l.calls++
	if l.err != nil {
		return config.NamespacedRESTConfig{}, l.err
	}
	return config.NamespacedRESTConfig{
		Config:    rest.Config{Host: "http://grafana.invalid"},
		Namespace: "default",
	}, nil
}

type fakeDiscovery struct {
	groups    []*metav1.APIGroup
	resources []*metav1.APIResourceList
}

func (f *fakeDiscovery) ServerGroupsAndResources() ([]*metav1.APIGroup, []*metav1.APIResourceList, error) {
	return f.groups, f.resources, nil
}

func routingTreesDiscovery(versions ...string) *fakeDiscovery {
	group := &metav1.APIGroup{Name: testGroup}
	lists := make([]*metav1.APIResourceList, 0, len(versions))
	for _, v := range versions {
		gv := testGroup + "/" + v
		group.Versions = append(group.Versions, metav1.GroupVersionForDiscovery{GroupVersion: gv, Version: v})
		lists = append(lists, &metav1.APIResourceList{
			GroupVersion: gv,
			APIResources: []metav1.APIResource{
				{Name: "routingtrees", SingularName: "routingtree", Kind: "RoutingTree", Namespaced: true, Verbs: []string{"get", "list", "create", "update", "delete"}},
			},
		})
	}
	if len(versions) > 0 {
		group.PreferredVersion = group.Versions[0]
	}
	return &fakeDiscovery{groups: []*metav1.APIGroup{group}, resources: lists}
}

func registryFrom(d discovery.Client) native.RegistryFunc {
	return func(ctx context.Context, _ config.NamespacedRESTConfig) (*discovery.Registry, error) {
		return discovery.NewCachedRegistry(ctx, d)
	}
}

func TestBindingLoad(t *testing.T) {
	cfg := native.Config{Group: testGroup, Resource: "routingtrees"}

	tests := []struct {
		name        string
		discovery   *fakeDiscovery
		apiVersion  string
		wantVersion string
		wantErr     string
		wantNoLoad  bool
	}{
		{
			name:        "preferred version",
			discovery:   routingTreesDiscovery("v1beta1", "v0alpha1"),
			wantVersion: "v1beta1",
		},
		{
			name:        "explicit group/version",
			discovery:   routingTreesDiscovery("v1beta1", "v0alpha1"),
			apiVersion:  testGroup + "/v0alpha1",
			wantVersion: "v0alpha1",
		},
		{
			name:        "version only",
			discovery:   routingTreesDiscovery("v1beta1", "v0alpha1"),
			apiVersion:  "v0alpha1",
			wantVersion: "v0alpha1",
		},
		{
			name:       "group mismatch rejected before config load",
			discovery:  routingTreesDiscovery("v1beta1"),
			apiVersion: "dashboard.grafana.app/v1",
			wantErr:    `is not in group "notifications.alerting.grafana.app"`,
			wantNoLoad: true,
		},
		{
			name:       "version not served",
			discovery:  routingTreesDiscovery("v1beta1"),
			apiVersion: "v2",
			wantErr:    "server does not serve routingtrees.notifications.alerting.grafana.app (version v2)",
		},
		{
			name:      "resource not served",
			discovery: &fakeDiscovery{},
			wantErr:   "server does not serve routingtrees.notifications.alerting.grafana.app",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			loader := &fakeLoader{}
			b := native.Bind(loader, cfg, native.WithRegistry(registryFrom(tc.discovery)))

			access, err := b.Load(t.Context(), native.LoadOptions{APIVersion: tc.apiVersion})
			if tc.wantNoLoad {
				assert.Zero(t, loader.calls)
			}
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				// Server-capability failures retain their type for CLI classification.
				var selErr resources.InvalidSelectorError
				assert.NotErrorAs(t, err, &selErr)
				if !tc.wantNoLoad {
					var unsupported *resources.UnsupportedResourceError
					require.ErrorAs(t, err, &unsupported)
				}
				return
			}

			require.NoError(t, err)
			assert.Equal(t, testGroup, access.Descriptor.GroupVersion.Group)
			assert.Equal(t, tc.wantVersion, access.Descriptor.GroupVersion.Version)
			assert.Equal(t, "RoutingTree", access.Descriptor.Kind)
			assert.Equal(t, "routingtrees", access.Descriptor.Plural)
			assert.NotNil(t, access.Client)
			assert.Equal(t, "default", access.Config.Namespace)
		})
	}
}

func TestBindingLoad_LoaderError(t *testing.T) {
	loader := &fakeLoader{err: errors.New("no context")}
	b := native.Bind(loader, native.Config{Group: testGroup, Resource: "routingtrees"},
		native.WithRegistry(func(context.Context, config.NamespacedRESTConfig) (*discovery.Registry, error) {
			t.Fatal("registry must not be built when config loading fails")
			return nil, errors.New("unreachable")
		}))

	_, err := b.Load(t.Context(), native.LoadOptions{})
	require.ErrorContains(t, err, "no context")
}

func TestBindDoesNoIO(t *testing.T) {
	loader := &fakeLoader{}
	native.Bind(loader, native.Config{Group: testGroup, Resource: "routingtrees"})
	assert.Zero(t, loader.calls)
}

func TestFixedAndFunc(t *testing.T) {
	want := native.Access{Config: config.NamespacedRESTConfig{Namespace: "stack-1"}}
	got, err := native.Fixed(want).Load(t.Context(), native.LoadOptions{APIVersion: "ignored"})
	require.NoError(t, err)
	assert.Equal(t, want, got)

	var seen native.LoadOptions
	b := native.Func(func(_ context.Context, o native.LoadOptions) (native.Access, error) {
		seen = o
		return want, nil
	})
	_, err = b.Load(t.Context(), native.LoadOptions{APIVersion: "v0alpha1"})
	require.NoError(t, err)
	assert.Equal(t, "v0alpha1", seen.APIVersion)

	_, err = native.Binding{}.Load(t.Context(), native.LoadOptions{})
	require.Error(t, err)
}

func TestParseAPIVersion(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "", want: ""},
		{in: "v1beta1", want: "v1beta1"},
		{in: testGroup + "/v0alpha1", want: "v0alpha1"},
		{in: "dashboard.grafana.app/v1", wantErr: true},
		{in: testGroup + "/", wantErr: true},
		{in: "a/b/c", wantErr: true},
		// No slash: ParseGroupVersion reads the whole value as a version.
		{in: "rules.alerting.grafana.app", wantErr: true},
		{in: testGroup, wantErr: true},
		{in: "V1", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, err := native.ParseAPIVersion(testGroup, tc.in)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestNoCLIImports enforces that the package imports no CLI packages
// directly, so other agent-facing surfaces can reuse it.
func TestNoCLIImports(t *testing.T) {
	forbidden := []string{
		"github.com/spf13/cobra",
		"github.com/spf13/pflag",
		"github.com/grafana/gcx/internal/output",
		"github.com/grafana/gcx/internal/terminal",
		"github.com/grafana/gcx/internal/style",
		"github.com/grafana/gcx/internal/providers",
		"golang.org/x/term",
		"github.com/charmbracelet/",
	}

	files, err := filepath.Glob("*.go")
	require.NoError(t, err)

	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		require.NoError(t, err)
		f, err := parser.ParseFile(fset, name, src, parser.ImportsOnly)
		require.NoError(t, err)
		for _, imp := range f.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			require.NoError(t, err)
			for _, bad := range forbidden {
				if path == bad || (strings.HasSuffix(bad, "/") && strings.HasPrefix(path, bad)) {
					t.Errorf("%s imports CLI package %q", name, path)
				}
			}
		}
	}
}
