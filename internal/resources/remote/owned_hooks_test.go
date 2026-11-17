package remote_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/resources/remote"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// An owned adapter deliberately has no ordinary read implementation: any
// accidental existence, natural-key, or configuration probe panics in these tests.
type ownedHookAdapter struct {
	adapter.ResourceAdapter

	mu             sync.Mutex
	plans          int
	planned        []*resources.Resource
	dispatched     []unstructured.Unstructured
	reject         bool
	planErr        error
	validationErr  error
	selections     int
	selected       []unstructured.Unstructured
	selectionErr   error
	deleted        []string
	pullPreflights int
}

func (a *ownedHookAdapter) NewPushPreflight(_ context.Context, selections []adapter.PushSelection) (adapter.PushPreflight, error) {
	a.plans++
	if a.planErr != nil {
		return nil, a.planErr
	}
	for _, s := range selections {
		a.planned = append(a.planned, s.Items...)
	}
	return func(_ context.Context, r *resources.Resource) error {
		if a.reject {
			return errors.New("duplicate target")
		}
		annotations := r.Object.GetAnnotations()
		if annotations == nil {
			annotations = make(map[string]string)
		}
		annotations["test.example/resolved"] = r.Annotations()["test.example/id"]
		r.Object.SetAnnotations(annotations)
		return nil
	}, nil
}
func (a *ownedHookAdapter) PushEnvelope(_ context.Context, obj *unstructured.Unstructured, opts metav1.CreateOptions) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.dispatched = append(a.dispatched, *obj.DeepCopy())
	if len(opts.DryRun) > 0 {
		return a.validationErr
	}
	return nil
}
func (a *ownedHookAdapter) SelectDelete(_ context.Context, _ resources.Filter, manifests []unstructured.Unstructured) ([]unstructured.Unstructured, error) {
	a.selections++
	if a.selectionErr != nil {
		return nil, a.selectionErr
	}
	if manifests == nil {
		return a.selected, nil
	}
	out := make([]unstructured.Unstructured, len(manifests))
	for i, m := range manifests {
		out[i] = *m.DeepCopy()
		out[i].SetName(m.GetAnnotations()["test.example/id"])
	}
	return out, nil
}
func (a *ownedHookAdapter) Delete(_ context.Context, name string, opts metav1.DeleteOptions) error {
	if len(opts.DryRun) > 0 {
		return adapter.ErrDryRunUnverified
	}
	a.deleted = append(a.deleted, name)
	return nil
}
func (a *ownedHookAdapter) NewPullPreflight(_ []adapter.PullSelection) adapter.PullPreflight {
	a.pullPreflights++
	return func(context.Context, resources.Filter, unstructured.Unstructured) error {
		return errors.New("export-only preflight")
	}
}
func hookResource(desc resources.Descriptor, name, id string) *resources.Resource {
	obj := &unstructured.Unstructured{Object: map[string]any{"apiVersion": desc.GroupVersion.String(), "kind": desc.Kind, "metadata": map[string]any{"name": name}, "spec": map[string]any{"title": "test title"}}}
	obj.SetAnnotations(map[string]string{"test.example/id": id})
	return resources.MustFromUnstructured(obj)
}
func hookRouter(desc resources.Descriptor, a *ownedHookAdapter) *adapter.ResourceClientRouter {
	return adapter.NewResourceClientRouter(nil, map[schema.GroupVersionKind]adapter.Factory{desc.GroupVersionKind(): func(context.Context) (adapter.ResourceAdapter, error) { return a, nil }})
}

type warningOutcomeError struct{}

func (warningOutcomeError) Error() string { return "unverified" }
func (warningOutcomeError) Unwrap() error { return adapter.ErrDryRunUnverified }
func (warningOutcomeError) OperationWarnings() []string {
	return []string{"spec.mode: unfamiliar value custom; known values: standard"}
}

func TestOwnedPushBatchBeforeProbes(t *testing.T) {
	for _, reject := range []bool{false, true} {
		t.Run(map[bool]string{false: "dispatch", true: "all duplicate targets refused"}[reject], func(t *testing.T) {
			desc := dashboardDescriptor()
			a := &ownedHookAdapter{reject: reject}
			input := resources.NewResources()
			input.AddDistinct(hookResource(desc, "same-name", "id-1"))
			input.AddDistinct(hookResource(desc, "same-name", "id-2"))
			summary, err := remote.NewPusher(hookRouter(desc, a), &mockPushRegistry{supportedResources: resources.Descriptors{desc}}).Push(t.Context(), remote.PushRequest{Resources: input, MaxConcurrency: 2})
			require.NoError(t, err)
			require.Equal(t, 1, a.plans)
			require.Len(t, a.planned, 2)
			if reject {
				require.Equal(t, 2, summary.FailedCount())
				require.Empty(t, a.dispatched)
				return
			}
			require.Equal(t, 2, summary.SuccessCount())
			require.Len(t, a.dispatched, 2)
			for _, obj := range a.dispatched {
				require.Equal(t, obj.GetAnnotations()["test.example/id"], obj.GetAnnotations()["test.example/resolved"])
				require.Equal(t, "same-name", obj.GetName())
			}
		})
	}
}
func TestOwnedDryRunSkipsPlanningAndRetainsWarnings(t *testing.T) {
	desc := dashboardDescriptor()
	a := &ownedHookAdapter{validationErr: warningOutcomeError{}}
	summary, err := remote.NewPusher(hookRouter(desc, a), &mockPushRegistry{supportedResources: resources.Descriptors{desc}}).Push(t.Context(), remote.PushRequest{Resources: resources.NewResources(hookResource(desc, "same-name", "id-1")), DryRun: true})
	require.NoError(t, err)
	require.Zero(t, a.plans)
	require.Equal(t, 1, summary.SkippedCount())
	require.Zero(t, summary.SuccessCount())
	require.Zero(t, summary.FailedCount())
	require.Equal(t, warningOutcomeError{}.OperationWarnings(), summary.Warnings())
}
func TestOwnedDeleteSelectionBypassesExportsAndCollisionChecks(t *testing.T) {
	desc := dashboardDescriptor()
	a := &ownedHookAdapter{selected: []unstructured.Unstructured{hookResource(desc, "id-1", "id-1").Object, hookResource(desc, "id-2", "id-2").Object}}
	dest := resources.NewResources()
	summary, err := remote.NewPuller(hookRouter(desc, a), &mockPullRegistry{descriptors: resources.Descriptors{desc}}).Pull(t.Context(), remote.PullRequest{Filters: resources.Filters{{Descriptor: desc, Type: resources.FilterTypeMultiple, ResourceUIDs: []string{"id-1", "id-2"}}}, Resources: dest, IdentityOnlyDelete: true})
	require.NoError(t, err)
	require.Equal(t, 2, summary.SuccessCount())
	require.Equal(t, 2, dest.Len())
	require.Equal(t, 1, a.selections)
	require.Zero(t, a.pullPreflights)
}
func TestOwnedFileDeleteResolvesAllBeforeWrites(t *testing.T) {
	for _, fail := range []bool{false, true} {
		for _, dryRun := range []bool{false, true} {
			t.Run(map[bool]string{false: "resolved", true: "stale id"}[fail]+map[bool]string{false: " apply", true: " dry-run"}[dryRun], func(t *testing.T) {
				desc := dashboardDescriptor()
				a := &ownedHookAdapter{}
				if fail {
					a.selectionErr = errors.New("annotated id not found")
				}
				input := resources.NewResources()
				input.AddDistinct(hookResource(desc, "same-name", "id-1"))
				input.AddDistinct(hookResource(desc, "same-name", "id-2"))
				summary, err := remote.NewDeleterWithClient(hookRouter(desc, a), &mockPushRegistry{supportedResources: resources.Descriptors{desc}}).Delete(t.Context(), remote.DeleteRequest{Resources: input, ResolveLocalIdentities: true, DryRun: dryRun})
				require.NoError(t, err)
				require.Equal(t, 1, a.selections)
				switch {
				case fail:
					require.Equal(t, 2, summary.FailedCount())
					require.Empty(t, a.deleted)
				case dryRun:
					require.Equal(t, 2, summary.SkippedCount())
					require.Empty(t, a.deleted)
				default:
					require.Equal(t, 2, summary.SuccessCount())
					require.ElementsMatch(t, []string{"id-1", "id-2"}, a.deleted)
				}
			})
		}
	}
}
func TestOwnedDeleteUnavailableSelectionPolicy(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		t.Run(map[bool]string{false: "all", true: "explicit"}[explicit], func(t *testing.T) {
			desc := dashboardDescriptor()
			a := &ownedHookAdapter{selectionErr: adapter.ErrUnavailable}
			var filters resources.Filters
			if explicit {
				filters = resources.Filters{{Descriptor: desc, Type: resources.FilterTypeAll}}
			}
			summary, err := remote.NewPuller(hookRouter(desc, a), &mockPullRegistry{descriptors: resources.Descriptors{desc}}).Pull(t.Context(), remote.PullRequest{Filters: filters, Resources: resources.NewResources(), IdentityOnlyDelete: true, StopOnError: true})
			if explicit {
				require.ErrorIs(t, err, adapter.ErrUnavailable)
				require.Zero(t, summary.SkippedCount())
			} else {
				require.NoError(t, err)
				require.Equal(t, 1, summary.SkippedCount())
			}
		})
	}
}

func TestOwnedBatchPlanningFailureRetainsOtherKinds(t *testing.T) {
	desc := dashboardDescriptor()
	other := desc
	other.GroupVersion.Group = "unrelated.example"
	failed := &ownedHookAdapter{planErr: errors.New("identity collection unavailable")}
	good := &ownedHookAdapter{}
	router := adapter.NewResourceClientRouter(nil, map[schema.GroupVersionKind]adapter.Factory{
		desc.GroupVersionKind():  func(context.Context) (adapter.ResourceAdapter, error) { return failed, nil },
		other.GroupVersionKind(): func(context.Context) (adapter.ResourceAdapter, error) { return good, nil },
	})
	input := resources.NewResources(hookResource(desc, "first", "id-1"), hookResource(other, "second", "id-2"))
	summary, err := remote.NewPusher(router, &mockPushRegistry{supportedResources: resources.Descriptors{desc, other}}).Push(t.Context(), remote.PushRequest{Resources: input})
	require.NoError(t, err)
	require.Equal(t, 1, summary.SuccessCount())
	require.Equal(t, 1, summary.FailedCount())
	require.Empty(t, failed.dispatched)
	require.Len(t, good.dispatched, 1)
}

func TestOwnedDeleteSelectionCancellationIsNotBatchFailure(t *testing.T) {
	for _, cause := range []error{context.Canceled, errors.New("interrupt signal received")} {
		t.Run(cause.Error(), func(t *testing.T) {
			ctx, cancel := context.WithCancelCause(t.Context())
			cancel(cause)
			desc := dashboardDescriptor()
			a := &ownedHookAdapter{selectionErr: cause}
			summary, err := remote.NewPuller(hookRouter(desc, a), &mockPullRegistry{}).Pull(ctx, remote.PullRequest{
				Filters:   resources.Filters{{Descriptor: desc, Type: resources.FilterTypeAll}},
				Resources: resources.NewResources(), IdentityOnlyDelete: true,
			})
			require.ErrorIs(t, err, cause)
			require.Zero(t, summary.FailedCount())
			require.Empty(t, summary.Failures())
		})
	}
}
