package remote

import (
	"context"
	"time"

	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/grafana-app-sdk/logging"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
)

// conflictRetryClient retries conflicts for TimeInterval and TemplateGroup
// mutations from the dynamic fallback. These kinds can encounter transient
// Alertmanager configuration conflicts. The dry-run guard belongs outside this
// wrapper so blocked mutations never reach it. Requests are replayed unchanged;
// other kinds pass through once, preserving stale resourceVersion conflicts.
type conflictRetryClient struct {
	inner   adapter.DynamicClient
	backoff wait.Backoff
}

var _ adapter.DynamicClient = (*conflictRetryClient)(nil)

func newConflictRetryClient(inner adapter.DynamicClient) *conflictRetryClient {
	return &conflictRetryClient{
		inner: inner,
		backoff: wait.Backoff{
			Steps: 10, Duration: 25 * time.Millisecond, Factor: 1.5,
			Jitter: 1.0, Cap: time.Second,
		},
	}
}

// newGuardedRetryDynamicClient keeps the dry-run guard outside conflict retries:
// blocked requests never reach the retrying client or the API.
func newGuardedRetryDynamicClient(inner adapter.DynamicClient, guard GuardConfig) adapter.DynamicClient {
	return newGuardedDynamicClient(newConflictRetryClient(inner), guard)
}

func (c *conflictRetryClient) onConflict(ctx context.Context, fn func() error) error {
	backoff := c.backoff // Each mutation has its own schedule, including concurrent calls.
	attempt := 0
	// OnError's sleep cannot be interrupted by a context. Let it own conflict
	// classification, attempt limits and preservation of the last error; perform
	// the delays here, where cancellation can interrupt them.
	err := retry.OnError(wait.Backoff{Steps: backoff.Steps}, apierrors.IsConflict, func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if attempt > 0 {
			timer := time.NewTimer(backoff.Step())
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			logging.FromContext(ctx).Debug("Retrying conflicting resource mutation", "attempt", attempt+1)
		}
		attempt++
		return fn()
	})
	// OnError replaces interrupted waits with the last conflict. Cancellation
	// must remain visible to the caller instead of becoming another conflict.
	if err != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

func (c *conflictRetryClient) Create(
	ctx context.Context, desc resources.Descriptor, obj *unstructured.Unstructured, opts metav1.CreateOptions,
) (*unstructured.Unstructured, error) {
	if !resources.HasServerDerivedName(desc.GroupVersionKind()) {
		return c.inner.Create(ctx, desc, obj, opts)
	}
	var result *unstructured.Unstructured
	err := c.onConflict(ctx, func() error {
		var err error
		result, err = c.inner.Create(ctx, desc, obj.DeepCopy(), *opts.DeepCopy())
		return err
	})
	return result, err
}

func (c *conflictRetryClient) Update(
	ctx context.Context, desc resources.Descriptor, obj *unstructured.Unstructured, opts metav1.UpdateOptions,
) (*unstructured.Unstructured, error) {
	if !resources.HasServerDerivedName(desc.GroupVersionKind()) {
		return c.inner.Update(ctx, desc, obj, opts)
	}
	var result *unstructured.Unstructured
	err := c.onConflict(ctx, func() error {
		var err error
		result, err = c.inner.Update(ctx, desc, obj.DeepCopy(), *opts.DeepCopy())
		return err
	})
	return result, err
}

func (c *conflictRetryClient) Delete(ctx context.Context, desc resources.Descriptor, name string, opts metav1.DeleteOptions) error {
	if !resources.HasServerDerivedName(desc.GroupVersionKind()) {
		return c.inner.Delete(ctx, desc, name, opts)
	}
	return c.onConflict(ctx, func() error {
		return c.inner.Delete(ctx, desc, name, *opts.DeepCopy())
	})
}

func (c *conflictRetryClient) Get(
	ctx context.Context, desc resources.Descriptor, name string, opts metav1.GetOptions,
) (*unstructured.Unstructured, error) {
	return c.inner.Get(ctx, desc, name, opts)
}

func (c *conflictRetryClient) GetMultiple(
	ctx context.Context, desc resources.Descriptor, names []string, opts metav1.GetOptions,
) ([]unstructured.Unstructured, error) {
	return c.inner.GetMultiple(ctx, desc, names, opts)
}

func (c *conflictRetryClient) List(
	ctx context.Context, desc resources.Descriptor, opts metav1.ListOptions,
) (*unstructured.UnstructuredList, error) {
	return c.inner.List(ctx, desc, opts)
}
