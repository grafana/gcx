package scim //nolint:testpackage // Tests inject native bindings into private command factories.

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/native"
	"github.com/grafana/gcx/internal/resources"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type configClient struct {
	native.Client

	object       *unstructured.Unstructured
	gets, writes int
	conflict     bool
	written      *unstructured.Unstructured
}

func (c *configClient) Get(_ context.Context, _ resources.Descriptor, name string, _ metav1.GetOptions) (*unstructured.Unstructured, error) {
	c.gets++
	if name != "default" {
		panic("incorrect singleton name")
	}
	return c.object.DeepCopy(), nil
}
func (c *configClient) Update(_ context.Context, _ resources.Descriptor, obj *unstructured.Unstructured, _ metav1.UpdateOptions) (*unstructured.Unstructured, error) {
	c.writes++
	c.written = obj.DeepCopy()
	if c.conflict {
		return nil, apierrors.NewConflict(schema.GroupResource{Group: scimGroup, Resource: "config"}, "default", nil)
	}
	return obj, nil
}
func fixtureConfig() *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{"apiVersion": scimGroup + "/v0alpha1", "kind": "SCIMConfig", "metadata": map[string]any{"name": "default", "namespace": "stacks-12345", "resourceVersion": "17", "annotations": map[string]any{"secret": "hidden"}}, "spec": map[string]any{"enableUserSync": true, "enableGroupSync": true, "rejectNonProvisionedUsers": false, "future": map[string]any{"token": "hidden"}}}}
}
func configBinding(c *configClient) native.Binding {
	return native.Fixed(native.Access{Client: c, Descriptor: resources.Descriptor{GroupVersion: schema.GroupVersion{Group: scimGroup, Version: "v0alpha1"}, Plural: "config", Kind: "SCIMConfig"}, Config: config.NamespacedRESTConfig{Namespace: "default", GrafanaURL: "https://example.grafana.net"}})
}
func executeConfig(cmd *cobra.Command, args ...string) (string, error) {
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}
func TestConfigReadRedactsUnknownFields(t *testing.T) {
	c := &configClient{object: fixtureConfig()}
	out, err := executeConfig(newConfigCommand(configBinding(c)), "get", "-o", "json")
	require.NoError(t, err)
	require.JSONEq(t, `{"apiVersion":"scim.grafana.app/v0alpha1","kind":"SCIMConfig","metadata":{"name":"default","namespace":"stacks-12345","resourceVersion":"17"},"spec":{"enableUserSync":true,"enableGroupSync":true,"rejectNonProvisionedUsers":false}}`, out)
	require.NotContains(t, out, "hidden")
	require.Zero(t, c.writes)
}
func TestConfigUpdatePreservesOtherFields(t *testing.T) {
	c := &configClient{object: fixtureConfig()}
	out, err := executeConfig(newConfigCommand(configBinding(c)), "update", "--user-sync=false", "-o", "json")
	require.NoError(t, err)
	require.Equal(t, 1, c.gets)
	require.Equal(t, 1, c.writes)
	require.Equal(t, "17", c.written.GetResourceVersion())
	spec, found, err := unstructured.NestedMap(c.written.Object, "spec")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, true, spec["enableGroupSync"])
	require.Equal(t, false, spec["enableUserSync"])
	require.Equal(t, map[string]any{"token": "hidden"}, spec["future"])
	original, found, err := unstructured.NestedBool(c.object.Object, "spec", "enableUserSync")
	require.NoError(t, err)
	require.True(t, found)
	require.True(t, original)
	require.NotContains(t, out, "hidden")
}
func TestConfigUpdateDryRun(t *testing.T) {
	c := &configClient{object: fixtureConfig()}
	out, err := executeConfig(newConfigCommand(configBinding(c)), "update", "--group-sync=false", "--reject-non-provisioned=true", "--dry-run", "-o", "json")
	require.NoError(t, err)
	require.Equal(t, 1, c.gets)
	require.Zero(t, c.writes)
	var result struct {
		DryRun bool `json:"dry_run"`
		Config struct {
			Spec map[string]bool `json:"spec"`
		} `json:"config"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	require.True(t, result.DryRun)
	require.False(t, result.Config.Spec["enableGroupSync"])
	require.True(t, result.Config.Spec["rejectNonProvisionedUsers"])
	require.NotContains(t, out, "hidden")
}
func TestConfigValidationBeforeLoad(t *testing.T) {
	for _, args := range [][]string{{"update"}, {"update", "extra", "--user-sync"}, {"get", "extra"}} {
		cmd := newConfigCommand(native.Func(func(context.Context, native.LoadOptions) (native.Access, error) {
			t.Fatal("must validate before load")
			return native.Access{}, nil
		}))
		_, err := executeConfig(cmd, args...)
		require.Error(t, err)
	}
}
func TestConfigConflictHasNoRetry(t *testing.T) {
	c := &configClient{object: fixtureConfig(), conflict: true}
	_, err := executeConfig(newConfigCommand(configBinding(c)), "update", "--user-sync=false")
	require.Error(t, err)
	require.True(t, apierrors.IsConflict(err))
	require.ErrorContains(t, err, "apply the update again")
	require.Equal(t, 1, c.writes)
}
func TestConfigMissingVersionPreventsWrite(t *testing.T) {
	c := &configClient{object: fixtureConfig()}
	c.object.SetResourceVersion("")
	_, err := executeConfig(newConfigCommand(configBinding(c)), "update", "--user-sync=false")
	require.ErrorContains(t, err, "resource version")
	require.Zero(t, c.writes)
}
func TestEndpointUsesObservedNamespace(t *testing.T) {
	c := &configClient{object: fixtureConfig()}
	out, err := executeConfig(newEndpointCommand(configBinding(c)), "get", "-o", "json")
	require.NoError(t, err)
	require.JSONEq(t, `{"url":"https://example.grafana.net/apis/scim.grafana.app/v0alpha1/namespaces/stacks-12345"}`, out)
	require.Zero(t, c.writes)
}
func TestTenantEndpointRejectsUnsafeInput(t *testing.T) {
	for _, server := range []string{"", "://bad", "https://token@example.net", "https://example.net?token=secret", "https://example.net#secret", "file:///tmp"} {
		_, err := tenantEndpoint(server, "stacks-12345", "v0alpha1")
		require.Error(t, err)
	}
	for _, ns := range []string{"", "../other", "stacks/a", "stacks-12345?token=x"} {
		_, err := tenantEndpoint("https://example.net", ns, "v0alpha1")
		require.Error(t, err)
	}
	got, err := tenantEndpoint("https://example.net/grafana/", "stacks-12345", "v0alpha1")
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(got, "https://example.net/grafana/apis/"))
}
