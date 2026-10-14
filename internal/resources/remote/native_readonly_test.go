package remote_test

import (
	"context"
	"testing"

	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/remote"
	"github.com/grafana/grafana/pkg/apimachinery/utils"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestNativeReadOnly_LocalMutationsSkipWithoutIO(t *testing.T) {
	for _, version := range []string{"v0alpha1", "v1beta1"} {
		for _, kind := range []string{"TimeInterval", "TemplateGroup"} {
			for _, source := range []string{"system", "file", "converted_prometheus", "mimir", "api", ""} {
				for _, operation := range []string{"push", "delete"} {
					t.Run(version+"/"+kind+"/"+source+"/"+operation, func(t *testing.T) {
						desc := readOnlyDescriptor(kind, version)
						obj := readOnlyObject(desc, "opaque", source)
						res := resources.MustFromUnstructured(obj)
						before := obj.DeepCopy()
						client := &readOnlyClient{}
						registry := &mockPushRegistry{supportedResources: []resources.Descriptor{desc}}
						var summary *remote.OperationSummary
						var err error
						if operation == "push" {
							summary, err = remote.NewPusher(client, registry).Push(t.Context(), remote.PushRequest{
								Resources: resources.NewResources(res), MaxConcurrency: 1, StopOnError: true, IncludeManaged: true,
							})
						} else {
							summary, err = remote.NewDeleterWithClient(client, registry).Delete(t.Context(), remote.DeleteRequest{
								Resources: resources.NewResources(res), MaxConcurrency: 1, StopOnError: true,
							})
						}
						require.NoError(t, err)
						require.Zero(t, summary.FailedCount())
						if source == "api" || source == "" {
							require.Equal(t, 1, summary.SuccessCount())
							require.Zero(t, summary.SkippedCount())
							require.Equal(t, 1, client.writes)
						} else {
							require.Zero(t, summary.SuccessCount())
							require.Equal(t, 1, summary.SkippedCount())
							require.Equal(t, map[string]int{"read-only": 1}, summary.SkippedReasons())
							require.Zero(t, client.gets+client.lists+client.writes)
						}
						require.Equal(t, before, &res.Object)
					})
				}
			}
		}
	}
}

func TestNativeReadOnly_PushDestinationMatchRejectsBeforeWrite(t *testing.T) {
	for _, version := range []string{"v0alpha1", "v1beta1"} {
		for _, lookup := range []string{"get", "list"} {
			t.Run(version+"/"+lookup, func(t *testing.T) {
				desc := readOnlyDescriptor("TimeInterval", version)
				remoteObj := readOnlyObject(desc, "destination", "converted_prometheus")
				client := &readOnlyClient{objects: []*unstructured.Unstructured{remoteObj}}
				localName := "destination"
				if lookup == "list" {
					localName = ""
				}
				local := readOnlyObject(desc, localName, "api")
				pusher := remote.NewPusher(client, &mockPushRegistry{supportedResources: []resources.Descriptor{desc}})
				summary, err := pusher.Push(t.Context(), remote.PushRequest{
					Resources: resources.NewResources(resources.MustFromUnstructured(local)), MaxConcurrency: 1, IncludeManaged: true,
				})
				require.NoError(t, err)
				require.Equal(t, 1, summary.FailedCount())
				require.Zero(t, summary.SuccessCount()+summary.SkippedCount())
				require.Len(t, summary.Failures(), 1)
				require.ErrorContains(t, summary.Failures()[0].Error, "read-only")
				require.ErrorContains(t, summary.Failures()[0].Error, "external Alertmanager configuration")
				require.ErrorContains(t, summary.Failures()[0].Error, "weekend")
				require.ErrorContains(t, summary.Failures()[0].Error, "different title")
				require.Zero(t, client.writes)
				if lookup == "get" {
					require.Equal(t, 1, client.gets)
					require.Zero(t, client.lists)
				} else {
					require.Zero(t, client.gets)
					require.Equal(t, 1, client.lists)
				}
			})
		}
	}
}

func TestNativeReadOnly_PullFilterKeepsReadsWhenUnset(t *testing.T) {
	for _, version := range []string{"v0alpha1", "v1beta1"} {
		for _, kind := range []string{"TimeInterval", "TemplateGroup"} {
			for _, exclude := range []bool{false, true} {
				for _, explicit := range []bool{false, true} {
					name := version + "/" + kind
					if exclude {
						name += "/exclude"
					}
					if explicit {
						name += "/single"
					}
					t.Run(name, func(t *testing.T) {
						desc := readOnlyDescriptor(kind, version)
						client := &readOnlyClient{objects: []*unstructured.Unstructured{
							readOnlyObject(desc, "read-only", "system"), readOnlyObject(desc, "writable", "api"),
						}}
						filters := resources.Filters{{Type: resources.FilterTypeAll, Descriptor: desc}}
						if explicit {
							filters = resources.Filters{{Type: resources.FilterTypeSingle, Descriptor: desc, ResourceUIDs: []string{"read-only"}}}
						}
						dest := resources.NewResources()
						puller := remote.NewPuller(client, &mockPullRegistry{descriptors: resources.Descriptors{desc}})
						summary, err := puller.Pull(t.Context(), remote.PullRequest{
							Filters: filters, Resources: dest, ExcludeReadOnly: exclude, StopOnError: true,
						})
						require.NoError(t, err)
						require.Zero(t, summary.FailedCount())
						want := 2
						if explicit {
							want = 1
						}
						if exclude {
							want--
							require.Equal(t, 1, summary.SkippedCount())
							require.Equal(t, map[string]int{"read-only": 1}, summary.SkippedReasons())
						} else {
							require.Zero(t, summary.SkippedCount())
							require.Empty(t, summary.SkippedReasons())
						}
						require.Equal(t, want, dest.Len())
						require.Equal(t, want, summary.SuccessCount())
						wantNames := []string{}
						if !exclude {
							wantNames = append(wantNames, "read-only")
						}
						if !explicit {
							wantNames = append(wantNames, "writable")
						}
						names := make([]string, 0, dest.Len())
						for _, res := range dest.AsList() {
							names = append(names, res.Name())
						}
						require.ElementsMatch(t, wantNames, names)
					})
				}
			}
		}
	}
}

func TestNativeReadOnly_PullManagedExclusionUnchanged(t *testing.T) {
	desc := readOnlyDescriptor("TimeInterval", "v1beta1")
	obj := readOnlyObject(desc, "external-manager", "api")
	obj.SetAnnotations(map[string]string{
		utils.AnnoKeyManagerKind:     string(utils.ManagerKindTerraform),
		utils.AnnoKeyManagerIdentity: "terraform",
	})
	for _, excludeReadOnly := range []bool{false, true} {
		client := &readOnlyClient{objects: []*unstructured.Unstructured{obj}}
		dest := resources.NewResources()
		puller := remote.NewPuller(client, &mockPullRegistry{descriptors: resources.Descriptors{desc}})
		summary, err := puller.Pull(t.Context(), remote.PullRequest{
			Resources: dest, ExcludeManaged: true, ExcludeReadOnly: excludeReadOnly,
		})
		require.NoError(t, err)
		require.Zero(t, dest.Len())
		require.Zero(t, summary.SuccessCount()+summary.FailedCount()+summary.SkippedCount())
		require.Empty(t, summary.SkippedReasons())
	}
}

func readOnlyDescriptor(kind, version string) resources.Descriptor {
	plural, singular := "timeintervals", "timeinterval"
	if kind == "TemplateGroup" {
		plural, singular = "templategroups", "templategroup"
	}
	return resources.Descriptor{GroupVersion: schema.GroupVersion{Group: "notifications.alerting.grafana.app", Version: version}, Kind: kind, Plural: plural, Singular: singular}
}

func readOnlyObject(desc resources.Descriptor, name, source string) *unstructured.Unstructured {
	spec := map[string]any{"name": "weekend"}
	if desc.Kind == "TemplateGroup" {
		spec = map[string]any{"title": "weekend", "kind": "grafana"}
	}
	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": desc.GroupVersion.String(), "kind": desc.Kind,
		"metadata": map[string]any{"name": name, "namespace": "default", "resourceVersion": "1"}, "spec": spec,
	}}
	if source == "mimir" {
		spec["kind"] = "mimir"
	} else if source != "" {
		obj.SetAnnotations(map[string]string{"grafana.com/provenance": source})
	}
	return obj
}

type readOnlyClient struct {
	objects             []*unstructured.Unstructured
	gets, lists, writes int
}

func (c *readOnlyClient) Get(_ context.Context, desc resources.Descriptor, name string, _ metav1.GetOptions) (*unstructured.Unstructured, error) {
	c.gets++
	for _, obj := range c.objects {
		if obj.GetName() == name {
			return obj.DeepCopy(), nil
		}
	}
	return nil, apierrors.NewNotFound(desc.GroupVersionResource().GroupResource(), name)
}

func (c *readOnlyClient) List(context.Context, resources.Descriptor, metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	c.lists++
	list := &unstructured.UnstructuredList{Items: []unstructured.Unstructured{}}
	for _, obj := range c.objects {
		list.Items = append(list.Items, *obj.DeepCopy())
	}
	return list, nil
}

func (c *readOnlyClient) GetMultiple(ctx context.Context, desc resources.Descriptor, names []string, opts metav1.GetOptions) ([]unstructured.Unstructured, error) {
	var items []unstructured.Unstructured
	for _, name := range names {
		obj, err := c.Get(ctx, desc, name, opts)
		if err != nil {
			return nil, err
		}
		items = append(items, *obj)
	}
	return items, nil
}

func (c *readOnlyClient) Create(_ context.Context, _ resources.Descriptor, obj *unstructured.Unstructured, _ metav1.CreateOptions) (*unstructured.Unstructured, error) {
	c.writes++
	return obj.DeepCopy(), nil
}

func (c *readOnlyClient) Update(_ context.Context, _ resources.Descriptor, obj *unstructured.Unstructured, _ metav1.UpdateOptions) (*unstructured.Unstructured, error) {
	c.writes++
	return obj.DeepCopy(), nil
}

func (c *readOnlyClient) Delete(context.Context, resources.Descriptor, string, metav1.DeleteOptions) error {
	c.writes++
	return nil
}
