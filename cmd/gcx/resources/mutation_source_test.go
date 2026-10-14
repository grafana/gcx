package resources //nolint:testpackage // exercises the shared conversion used by push and delete

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	internalresources "github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/remote"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestBatchMutation_FailureSource(t *testing.T) {
	for _, action := range []string{"pushed", "deleted"} {
		t.Run(action, func(t *testing.T) {
			registry := failureSourceRegistry{descs: internalresources.Descriptors{{
				GroupVersion: schema.GroupVersion{Group: "dashboard.grafana.app", Version: "v1"},
				Kind:         "Dashboard", Singular: "dashboard", Plural: "dashboards",
			}}}
			objects := internalresources.NewResources(
				failureSourceResource("local", "./manifests/dashboard.json"),
				failureSourceResource("remote", ""),
			)
			var summary *remote.OperationSummary
			var err error
			if action == "pushed" {
				summary, err = remote.NewPusher(failureSourceClient{}, registry).Push(t.Context(), remote.PushRequest{
					Resources: objects, MaxConcurrency: 1, IncludeManaged: true,
				})
			} else {
				summary, err = remote.NewDeleterWithClient(failureSourceClient{}, registry).Delete(t.Context(), remote.DeleteRequest{
					Resources: objects, MaxConcurrency: 1,
				})
			}
			require.NoError(t, err)
			result := batchMutationFromSummary(action, summary, false)
			require.Equal(t, 2, result.Summary.Failed)
			require.Len(t, result.Failures, 2)
			data, err := json.Marshal(result)
			require.NoError(t, err)
			var got struct {
				SchemaVersion string           `json:"schema_version"`
				Failures      []map[string]any `json:"failures"`
			}
			require.NoError(t, json.Unmarshal(data, &got))
			require.Equal(t, "1", got.SchemaVersion)
			byName := map[string]map[string]any{}
			for _, failure := range got.Failures {
				target, ok := failure["target"].(map[string]any)
				require.True(t, ok)
				require.Equal(t, "Dashboard", target["kind"])
				name, ok := target["name"].(string)
				require.True(t, ok)
				byName[name] = failure
			}
			require.Len(t, byName, 2)
			require.Contains(t, byName, "remote")
			require.Equal(t, "./manifests/dashboard.json", byName["local"]["source"])
			require.NotContains(t, byName["remote"], "source")
		})
	}
}

func TestBatchMutationFromSummary_NilResource(t *testing.T) {
	summary := &remote.OperationSummary{}
	summary.RecordFailure(nil, errors.New("operation failed"))
	result := batchMutationFromSummary("pushed", summary, false)
	require.Len(t, result.Failures, 1)
	require.Empty(t, result.Failures[0].Source)
	require.Equal(t, "operation failed", result.Failures[0].Error)
}

func failureSourceResource(name, source string) *internalresources.Resource {
	return internalresources.MustFromObject(map[string]any{
		"apiVersion": "dashboard.grafana.app/v1", "kind": "Dashboard",
		"metadata": map[string]any{"name": name}, "spec": map[string]any{"title": name},
	}, internalresources.SourceInfo{Path: source})
}

type failureSourceRegistry struct{ descs internalresources.Descriptors }

func (r failureSourceRegistry) SupportedResources() internalresources.Descriptors { return r.descs }

type failureSourceClient struct{}

func (failureSourceClient) Get(_ context.Context, desc internalresources.Descriptor, name string, _ metav1.GetOptions) (*unstructured.Unstructured, error) {
	return nil, apierrors.NewNotFound(desc.GroupVersionResource().GroupResource(), name)
}

func (failureSourceClient) Create(context.Context, internalresources.Descriptor, *unstructured.Unstructured, metav1.CreateOptions) (*unstructured.Unstructured, error) {
	return nil, errors.New("write failed")
}

func (failureSourceClient) Update(context.Context, internalresources.Descriptor, *unstructured.Unstructured, metav1.UpdateOptions) (*unstructured.Unstructured, error) {
	return nil, errors.New("write failed")
}

func (failureSourceClient) Delete(context.Context, internalresources.Descriptor, string, metav1.DeleteOptions) error {
	return errors.New("write failed")
}
