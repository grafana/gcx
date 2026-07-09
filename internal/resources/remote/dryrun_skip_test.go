package remote //nolint:testpackage // White-box test for the dry-run skip translation.

import (
	"context"
	"testing"

	"github.com/grafana/gcx/internal/resources"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// sentinelPushClient acts like a guarded client: Create/Update return errDryRunUnverified,
// and Get returns NotFound so the pusher takes the create path.
type sentinelPushClient struct{}

func (sentinelPushClient) Get(_ context.Context, desc resources.Descriptor, name string, _ metav1.GetOptions) (*unstructured.Unstructured, error) {
	return nil, apierrors.NewNotFound(desc.GroupVersionResource().GroupResource(), name)
}

func (sentinelPushClient) Create(_ context.Context, _ resources.Descriptor, _ *unstructured.Unstructured, _ metav1.CreateOptions) (*unstructured.Unstructured, error) {
	return nil, errDryRunUnverified
}

func (sentinelPushClient) Update(_ context.Context, _ resources.Descriptor, _ *unstructured.Unstructured, _ metav1.UpdateOptions) (*unstructured.Unstructured, error) {
	return nil, errDryRunUnverified
}

type fixedRegistry struct{ descs resources.Descriptors }

func (r fixedRegistry) SupportedResources() resources.Descriptors { return r.descs }

func TestPusher_DryRunUnverified_RecordedAsSkipped(t *testing.T) {
	reg := fixedRegistry{descs: resources.Descriptors{guardAlertRuleDesc()}}
	pusher := NewPusher(sentinelPushClient{}, reg)

	res := resources.MustFromObject(map[string]any{
		"apiVersion": "rules.alerting.grafana.app/v0alpha1",
		"kind":       "AlertRule",
		"metadata":   map[string]any{"name": "rule-1", "namespace": "default"},
		"spec":       map[string]any{"title": "rule-1"},
	}, resources.SourceInfo{})

	// StopOnError set: a skip must NOT abort or count as a failure.
	summary, err := pusher.Push(t.Context(), PushRequest{
		Resources:      resources.NewResources(res),
		MaxConcurrency: 1,
		StopOnError:    true,
		DryRun:         true,
		IncludeManaged: true,
	})

	require.NoError(t, err)
	require.Equal(t, 0, summary.SuccessCount())
	require.Equal(t, 0, summary.FailedCount())
	require.Equal(t, 1, summary.SkippedCount())
}

type sentinelDeleteClient struct{}

func (sentinelDeleteClient) Delete(_ context.Context, _ resources.Descriptor, _ string, _ metav1.DeleteOptions) error {
	return errDryRunUnverified
}

func TestDeleter_DryRunUnverified_RecordedAsSkipped(t *testing.T) {
	reg := fixedRegistry{descs: resources.Descriptors{guardAlertRuleDesc()}}
	deleter := NewDeleterWithClient(sentinelDeleteClient{}, reg)

	res := resources.MustFromObject(map[string]any{
		"apiVersion": "rules.alerting.grafana.app/v0alpha1",
		"kind":       "AlertRule",
		"metadata":   map[string]any{"name": "rule-1", "namespace": "default"},
		"spec":       map[string]any{"title": "rule-1"},
	}, resources.SourceInfo{})

	summary, err := deleter.Delete(t.Context(), DeleteRequest{
		Resources:      resources.NewResources(res),
		MaxConcurrency: 1,
		StopOnError:    true,
		DryRun:         true,
	})

	require.NoError(t, err)
	require.Equal(t, 0, summary.SuccessCount())
	require.Equal(t, 0, summary.FailedCount())
	require.Equal(t, 1, summary.SkippedCount())
}
