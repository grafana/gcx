package resources_test

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	cmdresources "github.com/grafana/gcx/cmd/gcx/resources"
	internalconfig "github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers"
	resource "github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var configRoutingTestDescriptor = resource.Descriptor{ //nolint:gochecknoglobals // immutable test fixture
	GroupVersion: schema.GroupVersion{Group: "routing.test.grafana.app", Version: "v1"},
	Kind:         "RoutingTest",
	Singular:     "routingtest",
	Plural:       "routingtests",
}

type configRoutingTestAdapter struct {
	account   string
	deletedAt *string
}

func (a *configRoutingTestAdapter) List(context.Context, metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	return &unstructured.UnstructuredList{}, nil
}

func (a *configRoutingTestAdapter) Get(context.Context, string, metav1.GetOptions) (*unstructured.Unstructured, error) {
	return &unstructured.Unstructured{}, nil
}

func (a *configRoutingTestAdapter) Create(context.Context, *unstructured.Unstructured, metav1.CreateOptions) (*unstructured.Unstructured, error) {
	return &unstructured.Unstructured{}, nil
}

func (a *configRoutingTestAdapter) Update(context.Context, *unstructured.Unstructured, metav1.UpdateOptions) (*unstructured.Unstructured, error) {
	return &unstructured.Unstructured{}, nil
}

func (a *configRoutingTestAdapter) Delete(_ context.Context, _ string, _ metav1.DeleteOptions) error {
	*a.deletedAt = a.account
	return nil
}

func (a *configRoutingTestAdapter) Descriptor() resource.Descriptor {
	return configRoutingTestDescriptor
}
func (a *configRoutingTestAdapter) Aliases() []string        { return nil }
func (a *configRoutingTestAdapter) Schema() json.RawMessage  { return nil }
func (a *configRoutingTestAdapter) Example() json.RawMessage { return nil }

// TestResourcesCommand_ConfigFileRoutesLazyAdapterDeleteAndWriteback guards
// against the cross-account mutation class from #951/B1. The parent resource
// command selects S1 while GCX_CONFIG points at S2. A zero-value loader inside
// a lazy adapter factory must read S1, the destructive router call must target
// S1, and provider write-back must mutate S1 only.
func TestResourcesCommand_ConfigFileRoutesLazyAdapterDeleteAndWriteback(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_DIRS", t.TempDir())

	writeConfig := func(path, account string) {
		t.Helper()
		contents := `version: 1
stacks:
  selected:
    providers:
      routing:
        account: ` + account + `
contexts:
  selected:
    stack: selected
current-context: selected
`
		require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
	}

	selectedFile := filepath.Join(t.TempDir(), "selected.yaml")
	defaultFile := filepath.Join(t.TempDir(), "default.yaml")
	writeConfig(selectedFile, "S1")
	writeConfig(defaultFile, "S2")
	t.Setenv(internalconfig.ConfigFileEnvVar, defaultFile)

	var deletedAt string
	var rootPreRunCalls int
	root := &cobra.Command{
		Use: "gcx",
		PersistentPreRun: func(*cobra.Command, []string) {
			rootPreRunCalls++
		},
	}
	resourcesCmd := cmdresources.Command()
	resourcesCmd.AddCommand(&cobra.Command{
		Use: "delete-routing-test",
		RunE: func(cmd *cobra.Command, _ []string) error {
			factory := adapter.Factory(func(ctx context.Context) (adapter.ResourceAdapter, error) {
				loader := &providers.ConfigLoader{}
				providerCfg, _, err := loader.LoadProviderConfig(ctx, "routing")
				if err != nil {
					return nil, err
				}
				return &configRoutingTestAdapter{account: providerCfg["account"], deletedAt: &deletedAt}, nil
			})
			router := adapter.NewResourceClientRouter(nil, map[schema.GroupVersionKind]adapter.Factory{
				configRoutingTestDescriptor.GroupVersionKind(): factory,
			})
			if err := router.Delete(cmd.Context(), configRoutingTestDescriptor, "victim", metav1.DeleteOptions{}); err != nil {
				return err
			}

			// Adapter-side discovery/cache writes use the same central selection.
			loader := &providers.ConfigLoader{}
			return loader.SaveProviderConfig(cmd.Context(), "routing", "last-delete", "victim")
		},
	})
	root.AddCommand(resourcesCmd)
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	root.SetArgs([]string{"resources", "--config", selectedFile, "--context", "selected", "delete-routing-test"})

	require.NoError(t, root.Execute())
	assert.Equal(t, 1, rootPreRunCalls)
	assert.Equal(t, "S1", deletedAt)

	selected, err := internalconfig.Load(t.Context(), internalconfig.ExplicitConfigFile(selectedFile))
	require.NoError(t, err)
	assert.Equal(t, "victim", selected.Stacks["selected"].Providers["routing"]["last-delete"])

	wrongDefault, err := internalconfig.Load(t.Context(), internalconfig.ExplicitConfigFile(defaultFile))
	require.NoError(t, err)
	assert.Empty(t, wrongDefault.Stacks["selected"].Providers["routing"]["last-delete"])
}
