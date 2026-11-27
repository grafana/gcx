package remote //nolint:testpackage // White-box test for the dry-run planned-action record.

import (
	"context"
	"testing"

	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// plannedClient behaves like a guarded client: Get serves `existing` by name
// (NotFound otherwise) and every dry-run mutation reports errDryRunUnverified.
type plannedClient struct {
	existing map[string]*unstructured.Unstructured
}

func (c plannedClient) Get(_ context.Context, desc resources.Descriptor, name string, _ metav1.GetOptions) (*unstructured.Unstructured, error) {
	if obj, ok := c.existing[name]; ok {
		return obj, nil
	}
	return nil, apierrors.NewNotFound(desc.GroupVersionResource().GroupResource(), name)
}

func (plannedClient) Create(context.Context, resources.Descriptor, *unstructured.Unstructured, metav1.CreateOptions) (*unstructured.Unstructured, error) {
	return nil, errDryRunUnverified
}

func (plannedClient) Update(context.Context, resources.Descriptor, *unstructured.Unstructured, metav1.UpdateOptions) (*unstructured.Unstructured, error) {
	return nil, errDryRunUnverified
}

func plannedRule(name string) *resources.Resource {
	return resources.MustFromObject(map[string]any{
		"apiVersion": "rules.alerting.grafana.app/v0alpha1",
		"kind":       "AlertRule",
		"metadata":   map[string]any{"name": name, "namespace": "default"},
		"spec":       map[string]any{"title": name},
	}, resources.SourceInfo{Path: name + ".json"})
}

func TestPusher_DryRun_RecordsPlannedAction(t *testing.T) {
	existing := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "rules.alerting.grafana.app/v0alpha1",
		"kind":       "AlertRule",
		"metadata":   map[string]any{"name": "rule-1", "namespace": "default", "resourceVersion": "7"},
	}}
	reg := fixedRegistry{descs: resources.Descriptors{guardAlertRuleDesc()}}
	pusher := NewPusher(plannedClient{existing: map[string]*unstructured.Unstructured{"rule-1": existing}}, reg)

	summary, err := pusher.Push(t.Context(), PushRequest{
		Resources:      resources.NewResources(plannedRule("rule-1"), plannedRule("rule-2")),
		MaxConcurrency: 1,
		DryRun:         true,
		IncludeManaged: true,
	})

	require.NoError(t, err)
	// Counts are unchanged: both stay skipped, and nothing is a real write.
	require.Equal(t, 2, summary.SkippedCount())
	require.Empty(t, summary.Successes())

	planned := summary.Planned()
	require.Len(t, planned, 2)
	byRequested := map[string]OperationSuccess{}
	for _, p := range planned {
		byRequested[p.RequestedName] = p
	}
	require.Equal(t, "updated", byRequested["rule-1"].Action)
	require.Equal(t, "rule-1", byRequested["rule-1"].Name)
	require.Equal(t, "AlertRule", byRequested["rule-1"].Kind)
	require.Equal(t, "rule-1.json", byRequested["rule-1"].SourcePath)
	require.Equal(t, "created", byRequested["rule-2"].Action)
	require.Empty(t, byRequested["rule-2"].Name, "a create has no target ID yet")
}

func TestPusher_RealPush_RecordsNoPlanned(t *testing.T) {
	reg := fixedRegistry{descs: resources.Descriptors{guardAlertRuleDesc()}}
	pusher := NewPusher(plannedClient{}, reg)

	summary, err := pusher.Push(t.Context(), PushRequest{
		Resources:      resources.NewResources(plannedRule("rule-1")),
		MaxConcurrency: 1,
		IncludeManaged: true,
	})

	require.NoError(t, err)
	require.Empty(t, summary.Planned())
}

func TestOperationSummary_PlannedIsDeterministic(t *testing.T) {
	summary := &OperationSummary{}
	for _, n := range []string{"c", "a", "b"} {
		summary.RecordPlanned(OperationSuccess{Kind: "K", RequestedName: n, Action: "created"})
	}
	got := summary.Planned()
	require.Equal(t, []string{"a", "b", "c"}, []string{got[0].RequestedName, got[1].RequestedName, got[2].RequestedName})
}

// nilCreateClient returns (nil, nil) from Create, as some fakes do.
type nilCreateClient struct{ plannedClient }

func (nilCreateClient) Create(context.Context, resources.Descriptor, *unstructured.Unstructured, metav1.CreateOptions) (*unstructured.Unstructured, error) {
	return nil, nil //nolint:nilnil // Simulates a client that reports success without a body.
}

func TestPusher_DryRun_NilAppliedDoesNotPanic(t *testing.T) {
	reg := fixedRegistry{descs: resources.Descriptors{guardAlertRuleDesc()}}
	pusher := NewPusher(nilCreateClient{}, reg)
	summary, err := pusher.Push(t.Context(), PushRequest{
		Resources: resources.NewResources(plannedRule("rule-1")), MaxConcurrency: 1, DryRun: true, IncludeManaged: true,
	})
	require.NoError(t, err)
	require.Len(t, summary.Planned(), 1)
	require.Equal(t, "created", summary.Planned()[0].Action)
}

// plannedListClient adds List so the pusher can match by natural key.
type plannedListClient struct {
	plannedClient

	remote []unstructured.Unstructured
}

func (c plannedListClient) List(context.Context, resources.Descriptor, metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	return &unstructured.UnstructuredList{Items: c.remote}, nil
}

// A resource whose name is unknown remotely but whose natural key matches is a
// planned update, and the planned target is the remote name, not the requested one.
func TestPusher_DryRun_PlannedNaturalKeyMatchUsesRemoteName(t *testing.T) {
	const group = "plannednk.test.grafana.app"
	gvk := schema.GroupVersionKind{Group: group, Version: "v1", Kind: "PlannedThing"}
	adapter.RegisterNaturalKey(gvk, adapter.SpecFieldKey("name"))

	desc := resources.Descriptor{
		GroupVersion: gvk.GroupVersion(),
		Kind:         gvk.Kind,
		Singular:     "plannedthing",
		Plural:       "plannedthings",
	}
	thing := func(name string) map[string]any {
		return map[string]any{
			"apiVersion": group + "/v1",
			"kind":       gvk.Kind,
			"metadata":   map[string]any{"name": name, "namespace": "default"},
			"spec":       map[string]any{"name": "My Resource"},
		}
	}
	remoteObj := unstructured.Unstructured{Object: thing("remote-uuid")}
	local := resources.MustFromObject(thing("local-uuid"), resources.SourceInfo{Path: "local.json"})

	pusher := NewPusher(plannedListClient{remote: []unstructured.Unstructured{remoteObj}}, fixedRegistry{descs: resources.Descriptors{desc}})
	summary, err := pusher.Push(t.Context(), PushRequest{
		Resources:      resources.NewResources(local),
		MaxConcurrency: 1,
		DryRun:         true,
		IncludeManaged: true,
	})
	require.NoError(t, err)

	planned := summary.Planned()
	require.Len(t, planned, 1)
	require.Equal(t, "updated", planned[0].Action)
	require.Equal(t, "local-uuid", planned[0].RequestedName)
	require.Equal(t, "remote-uuid", planned[0].Name)
}
