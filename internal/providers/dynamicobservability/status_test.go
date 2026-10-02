package dynamicobservability //nolint:testpackage // Tests exercise unexported status and mutation helpers.

import (
	"context"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/resources"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestProbeStatusRejectsOldGenerationAndStaleReports(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	p := probe{}
	p.Metadata.Name = "checkout"
	p.Metadata.Generation = 2
	p.Status.Nodes = []probeNode{
		{NodeName: "current", ObservedGeneration: 2, CheckedAt: now.Format(time.RFC3339), Phase: "Attached", Targets: []probeTarget{{Phase: "Attached"}}},
		{NodeName: "old", ObservedGeneration: 1, CheckedAt: now.Format(time.RFC3339), Phase: "Attached", Targets: []probeTarget{{Phase: "Attached"}}},
		{NodeName: "stale", ObservedGeneration: 2, CheckedAt: now.Add(-3 * time.Minute).Format(time.RFC3339), Phase: "Attached"},
	}
	got := probeStatus(p, now)
	if got.Phase != "Pending" || got.AttachedTargets != 1 {
		t.Fatalf("status = %+v, want Pending and one current attached target", got)
	}
	if got.Nodes[1].EffectivePhase != "Pending" || got.Nodes[2].EffectivePhase != "Unknown" {
		t.Fatalf("node phases = %+v, want old Pending and stale Unknown", got.Nodes)
	}
}

func TestAgentHealthUsesHeartbeatAndReadiness(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	a := nodeAgent{}
	a.Spec.LastSeen = now.Add(-61 * time.Second).Format(time.RFC3339)
	a.Spec.Ready = true
	a.Spec.RuntimeReady = true
	a.Spec.CacheSynced = true
	a.Spec.SourceWatchConnected = true
	if got := agentHealth(a, now); got.Connection != "Disconnected" || got.Health != "Disconnected" {
		t.Fatalf("health = %+v, want Disconnected", got)
	}
	a.Spec.LastSeen = now.Format(time.RFC3339)
	if got := agentHealth(a, now); got.Connection != "Connected" || got.Health != "Ready" {
		t.Fatalf("health = %+v, want Connected and Ready", got)
	}
}

type pausedClient struct {
	object  *unstructured.Unstructured
	updates int
}

func (c *pausedClient) Get(context.Context, resources.Descriptor, string, metav1.GetOptions) (*unstructured.Unstructured, error) {
	return c.object.DeepCopy(), nil
}
func (c *pausedClient) List(context.Context, resources.Descriptor, metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	return &unstructured.UnstructuredList{}, nil
}
func (c *pausedClient) Update(_ context.Context, _ resources.Descriptor, obj *unstructured.Unstructured, _ metav1.UpdateOptions) (*unstructured.Unstructured, error) {
	c.updates++
	c.object = obj.DeepCopy()
	return c.object, nil
}

func TestSetPausedPreservesRulesetAndSkipsNoop(t *testing.T) {
	c := &pausedClient{object: &unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{"name": "checkout", "resourceVersion": "4"},
		"spec":     map[string]any{"title": "Checkout", "rules": []any{map[string]any{"name": "entry"}}},
		"status":   map[string]any{"nodes": []any{map[string]any{"phase": "Attached"}}},
	}}}
	changed, err := setPaused(context.Background(), c, resources.Descriptor{}, "checkout", true)
	if err != nil || !changed || c.updates != 1 {
		t.Fatalf("changed=%v updates=%d err=%v", changed, c.updates, err)
	}
	if title, _, _ := unstructured.NestedString(c.object.Object, "spec", "title"); title != "Checkout" {
		t.Fatalf("title changed to %q", title)
	}
	if _, found, _ := unstructured.NestedSlice(c.object.Object, "status", "nodes"); !found {
		t.Fatal("status nodes were removed")
	}
	changed, err = setPaused(context.Background(), c, resources.Descriptor{}, "checkout", true)
	if err != nil || changed || c.updates != 1 {
		t.Fatalf("no-op changed=%v updates=%d err=%v", changed, c.updates, err)
	}
}
