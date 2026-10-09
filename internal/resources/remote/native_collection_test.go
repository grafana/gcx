package remote_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/local"
	"github.com/grafana/gcx/internal/resources/remote"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// unnamedCollectionClient records the valid manifests that reach mutation.
// Reads are empty, so every valid logical identity must take the create path.
type unnamedCollectionClient struct {
	mu      sync.Mutex
	creates []*unstructured.Unstructured
	updates int
	gets    int
}

func (c *unnamedCollectionClient) Get(_ context.Context, desc resources.Descriptor, name string, _ metav1.GetOptions) (*unstructured.Unstructured, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gets++
	return nil, apierrors.NewNotFound(desc.GroupVersionResource().GroupResource(), name)
}

func (*unnamedCollectionClient) List(_ context.Context, _ resources.Descriptor, _ metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	return &unstructured.UnstructuredList{}, nil
}

func (c *unnamedCollectionClient) Create(_ context.Context, _ resources.Descriptor, obj *unstructured.Unstructured, _ metav1.CreateOptions) (*unstructured.Unstructured, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.creates = append(c.creates, obj.DeepCopy())
	result := obj.DeepCopy()
	result.SetName(fmt.Sprintf("server-%d", len(c.creates)))
	return result, nil
}

func (c *unnamedCollectionClient) Update(_ context.Context, _ resources.Descriptor, obj *unstructured.Unstructured, _ metav1.UpdateOptions) (*unstructured.Unstructured, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.updates++
	return obj, nil
}

func TestPusher_UnnamedNativeDirectoryAggregation(t *testing.T) {
	for _, version := range []string{"v0alpha1", "v1beta1"} {
		for _, kind := range []string{"TimeInterval", "TemplateGroup"} {
			t.Run(version+"/"+kind, func(t *testing.T) {
				desc := resources.Descriptor{
					GroupVersion: schema.GroupVersion{Group: "notifications.alerting.grafana.app", Version: version},
					Kind:         kind, Plural: "timeintervals", Singular: "timeinterval",
				}
				specs := []map[string]any{{"name": "weekdays"}, {"name": "weekends"}, {"name": ""}, {"name": int64(42)}}
				wantIdentities := []string{"weekdays", "weekends"}
				wantFailures := 2
				if kind == "TemplateGroup" {
					desc.Plural, desc.Singular = "templategroups", "templategroup"
					specs = []map[string]any{
						{"title": "same", "kind": "grafana", "content": "{{ define \"same\" }}data{{ end }}"},
						{"title": "other", "content": "{{ define \"other\" }}data{{ end }}"},
						{"title": "same", "kind": "mimir", "content": "imported"},
						{"title": ""},
						{"title": "bad-kind", "kind": true},
					}
					wantIdentities = []string{"grafana\x00same", "grafana\x00other"}
					wantFailures = 2
				}
				dir := t.TempDir()
				for i, spec := range specs {
					obj := &unstructured.Unstructured{Object: map[string]any{
						"apiVersion": desc.GroupVersion.String(), "kind": kind,
						"metadata": map[string]any{"namespace": "target", "uid": "source-uid", "resourceVersion": "source-version", "generation": int64(1)},
						"spec":     spec,
					}}
					file, err := os.Create(filepath.Join(dir, fmt.Sprintf("%d.yaml", i)))
					require.NoError(t, err)
					require.NoError(t, format.NewYAMLCodec().Encode(file, obj))
					require.NoError(t, file.Close())
				}
				reader := local.FSReader{Decoders: format.Codecs(), MaxConcurrentReads: 3, StopOnError: true}
				objects := resources.NewResources()
				require.NoError(t, reader.Read(t.Context(), objects, nil, []string{dir}))
				require.Equal(t, len(specs), objects.Len(), "reader must retain valid, imported, and each malformed object")
				client := &unnamedCollectionClient{}
				pusher := remote.NewPusher(client, &mockPushRegistry{supportedResources: resources.Descriptors{desc}})
				summary, err := pusher.Push(t.Context(), remote.PushRequest{Resources: objects, MaxConcurrency: 2, IncludeManaged: true, NoPushFailureLog: true})
				require.NoError(t, err)
				require.Equal(t, 2, summary.SuccessCount())
				require.Equal(t, wantFailures, summary.FailedCount())
				if kind == "TemplateGroup" {
					require.Equal(t, 1, summary.SkippedCount())
					require.Equal(t, 1, summary.SkippedReasons()["read-only"])
				} else {
					require.Zero(t, summary.SkippedCount())
				}
				require.Zero(t, client.gets, "unnamed manifests must not GET the collection as an object")
				require.Zero(t, client.updates)
				require.Len(t, client.creates, 2, "imported and malformed manifests must produce no write")
				identities := make([]string, 0, len(client.creates))
				for _, obj := range client.creates {
					identity, readOnly, err := resources.NativeIdentity(obj)
					require.NoError(t, err)
					require.False(t, readOnly)
					identities = append(identities, identity)
					require.Empty(t, obj.GetName())
					require.Empty(t, obj.GetResourceVersion())
					require.Empty(t, obj.GetUID())
					require.Equal(t, "target", obj.GetNamespace())
				}
				require.ElementsMatch(t, wantIdentities, identities)
				for _, obj := range objects.AsList() {
					require.Empty(t, obj.Name())
					require.Equal(t, "source-version", obj.Object.GetResourceVersion())
					require.Equal(t, "source-uid", string(obj.Object.GetUID()))
				}
			})
		}
	}
}
