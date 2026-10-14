package remote_test

import (
	"testing"

	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/remote"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func nativeDeleteDescriptor(kind, version string) resources.Descriptor {
	plural := "timeintervals"
	if kind == "TemplateGroup" {
		plural = "templategroups"
	}
	return resources.Descriptor{GroupVersion: schema.GroupVersion{Group: "notifications.alerting.grafana.app", Version: version}, Kind: kind, Plural: plural}
}

func nativeDeleteResource(desc resources.Descriptor, name, title, provenance, templateKind string) *resources.Resource {
	field := "name"
	if desc.Kind == "TemplateGroup" {
		field = "title"
	}
	metadata := map[string]any{"namespace": "default"}
	if name != "" {
		metadata["name"] = name
	}
	if provenance != "" {
		metadata["annotations"] = map[string]any{"grafana.com/provenance": provenance}
	}
	spec := map[string]any{field: title}
	if desc.Kind == "TemplateGroup" {
		spec["kind"] = templateKind
	}
	return resources.MustFromObject(map[string]any{"apiVersion": desc.GroupVersion.String(), "kind": desc.Kind, "metadata": metadata, "spec": spec}, resources.SourceInfo{Path: "fixtures/" + title + ".yaml"})
}

func TestNativeDeleteRequiresAPIName(t *testing.T) {
	for _, version := range []string{"v0alpha1", "v1beta1"} {
		for _, kind := range []string{"TimeInterval", "TemplateGroup"} {
			t.Run(version+"/"+kind, func(t *testing.T) {
				desc := nativeDeleteDescriptor(kind, version)
				res := nativeDeleteResource(desc, "", "unnamed-native", "api", "grafana")
				client := &mockDeleteClient{}
				registry := &mockPushRegistry{supportedResources: []resources.Descriptor{desc}}
				summary, err := remote.NewDeleterWithClient(client, registry).Delete(t.Context(), remote.DeleteRequest{Resources: resources.NewResources(res), MaxConcurrency: 1})
				require.NoError(t, err, "batch records per-file failures when StopOnError is unset")
				require.Empty(t, client.deletedNames, "unnamed native files must fail before the Delete call")
				require.Zero(t, summary.SuccessCount())
				require.Zero(t, summary.SkippedCount())
				require.Equal(t, 1, summary.FailedCount())
				failures := summary.Failures()
				require.Len(t, failures, 1)
				require.Same(t, res, failures[0].Resource)
				require.ErrorContains(t, failures[0].Error, "metadata.name")
				require.ErrorContains(t, failures[0].Error, "unnamed-native")
				command := "gcx alert time-intervals delete"
				if kind == "TemplateGroup" {
					command = "gcx alert template-groups delete"
				}
				require.ErrorContains(t, failures[0].Error, command)
			})
		}
	}
}

func TestNativeDeleteReadOnlyUnnamedSkipsBeforeNameRejection(t *testing.T) {
	for _, version := range []string{"v0alpha1", "v1beta1"} {
		for _, kind := range []string{"TimeInterval", "TemplateGroup"} {
			for _, source := range []string{"system", "converted_prometheus", "file", "mimir"} {
				if kind == "TimeInterval" && source == "mimir" {
					continue
				}
				t.Run(version+"/"+kind+"/"+source, func(t *testing.T) {
					desc := nativeDeleteDescriptor(kind, version)
					provenance, templateKind := source, "grafana"
					if source == "mimir" {
						provenance, templateKind = "", "mimir"
					}
					res := nativeDeleteResource(desc, "", "source-owned", provenance, templateKind)
					client := &mockDeleteClient{}
					registry := &mockPushRegistry{supportedResources: []resources.Descriptor{desc}}
					summary, err := remote.NewDeleterWithClient(client, registry).Delete(t.Context(), remote.DeleteRequest{Resources: resources.NewResources(res), MaxConcurrency: 1, StopOnError: true})
					require.NoError(t, err)
					require.Empty(t, client.deletedNames)
					require.Zero(t, summary.SuccessCount())
					require.Zero(t, summary.FailedCount())
					require.Empty(t, summary.Failures())
					require.Equal(t, 1, summary.SkippedCount())
					require.Equal(t, map[string]int{"read-only": 1}, summary.SkippedReasons())
				})
			}
		}
	}
}

func TestNativeDeleteNamedFilesStillDelete(t *testing.T) {
	for _, version := range []string{"v0alpha1", "v1beta1"} {
		for _, kind := range []string{"TimeInterval", "TemplateGroup"} {
			t.Run(version+"/"+kind, func(t *testing.T) {
				desc := nativeDeleteDescriptor(kind, version)
				res := nativeDeleteResource(desc, "opaque-api-name", "named-native", "api", "grafana")
				client := &mockDeleteClient{}
				registry := &mockPushRegistry{supportedResources: []resources.Descriptor{desc}}
				summary, err := remote.NewDeleterWithClient(client, registry).Delete(t.Context(), remote.DeleteRequest{Resources: resources.NewResources(res), MaxConcurrency: 1})
				require.NoError(t, err)
				require.Equal(t, []string{"opaque-api-name"}, client.deletedNames)
				require.Equal(t, 1, summary.SuccessCount())
				require.Zero(t, summary.FailedCount())
				require.Zero(t, summary.SkippedCount())
			})
		}
	}
}

func TestNativeDeleteUnnamedFailureDoesNotStopBatch(t *testing.T) {
	desc := nativeDeleteDescriptor("TimeInterval", "v1beta1")
	unnamed := nativeDeleteResource(desc, "", "unnamed-native", "api", "grafana")
	named := nativeDeleteResource(desc, "opaque-api-name", "named-native", "api", "grafana")
	client := &mockDeleteClient{}
	registry := &mockPushRegistry{supportedResources: []resources.Descriptor{desc}}
	summary, err := remote.NewDeleterWithClient(client, registry).Delete(t.Context(), remote.DeleteRequest{Resources: resources.NewResources(unnamed, named), MaxConcurrency: 1})
	require.NoError(t, err)
	require.Equal(t, []string{"opaque-api-name"}, client.deletedNames)
	require.Equal(t, 1, summary.SuccessCount())
	require.Equal(t, 1, summary.FailedCount())
	require.Zero(t, summary.SkippedCount())
	require.Len(t, summary.Failures(), 1)
	require.Same(t, unnamed, summary.Failures()[0].Resource)
}

func TestNativeDeleteOrdinaryUnnamedBehaviorUnchanged(t *testing.T) {
	desc := resources.Descriptor{GroupVersion: schema.GroupVersion{Group: "dashboard.grafana.app", Version: "v1"}, Kind: "Dashboard", Plural: "dashboards"}
	res := resources.MustFromObject(map[string]any{"apiVersion": desc.GroupVersion.String(), "kind": desc.Kind, "spec": map[string]any{"title": "ordinary"}}, resources.SourceInfo{})
	client := &mockDeleteClient{}
	registry := &mockPushRegistry{supportedResources: []resources.Descriptor{desc}}
	summary, err := remote.NewDeleterWithClient(client, registry).Delete(t.Context(), remote.DeleteRequest{Resources: resources.NewResources(res), MaxConcurrency: 1})
	require.NoError(t, err)
	require.Equal(t, []string{""}, client.deletedNames, "ordinary kinds continue to pass the name through to their client")
	require.Equal(t, 1, summary.SuccessCount())
	require.Zero(t, summary.FailedCount())
	require.Zero(t, summary.SkippedCount())
}
