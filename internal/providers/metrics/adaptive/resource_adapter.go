package metrics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	adaptiveauth "github.com/grafana/gcx/internal/auth/adaptive"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	RuleAPIVersion = "adaptive-metrics.ext.grafana.app/v1alpha1"
	RuleKind       = "AggregationRule"
)

//nolint:gochecknoglobals // Static descriptor used in registration pattern.
var ruleDescriptorVar = resources.Descriptor{
	GroupVersion: schema.GroupVersion{
		Group:   "adaptive-metrics.ext.grafana.app",
		Version: "v1alpha1",
	},
	Kind:     RuleKind,
	Singular: "aggregationrule",
	Plural:   "aggregationrules",
}

// RuleDescriptor returns the resource descriptor for adaptive metrics aggregation rules.
func RuleDescriptor() resources.Descriptor { return ruleDescriptorVar }

// RuleSchema returns a JSON Schema for the AggregationRule resource type.
func RuleSchema() json.RawMessage {
	return adapter.SchemaFromType[MetricRule](RuleDescriptor())
}

// RuleExample returns an example AggregationRule manifest as JSON.
func RuleExample() json.RawMessage {
	example := map[string]any{
		"apiVersion": RuleAPIVersion,
		"kind":       RuleKind,
		"metadata":   map[string]any{"name": "my-metric"},
		"spec": map[string]any{
			"metric":       "my-metric",
			"match_type":   "exact",
			"drop_labels":  []string{"pod", "container"},
			"keep_labels":  []string{"job", "instance"},
			"aggregations": []string{"sum", "count"},
		},
	}
	b, err := json.Marshal(example)
	if err != nil {
		panic(fmt.Sprintf("adaptive/metrics: failed to marshal rule example: %v", err))
	}
	return b
}

func newAdaptiveMetricsClient(ctx context.Context, loader *providers.ConfigLoader) (*Client, error) {
	signalAuth, err := adaptiveauth.ResolveSignalAuth(ctx, loader, "metrics")
	if err != nil {
		return nil, err
	}
	return NewClient(signalAuth.BaseURL, signalAuth.TenantID, signalAuth.APIToken, signalAuth.HTTPClient), nil
}

// etagManager manages the global ETag required by the Adaptive Metrics rules API.
// All mutations must include the current ETag via If-Match. The manager lazily
// fetches the ETag on first mutation and retries once on 412 Precondition Failed.
// A mutex serializes concurrent mutations because the API's global ETag model means
// each successful mutation produces a new ETag that the next mutation must use.
type etagManager struct {
	client  *Client
	segment string

	mu   sync.Mutex
	etag string
}

// ensureETag fetches and caches the ETag if not already set.
// Must be called with em.mu held.
func (em *etagManager) ensureETag(ctx context.Context) error {
	if em.etag != "" {
		return nil
	}
	_, etag, err := em.client.ListRules(ctx, em.segment)
	if err != nil {
		return fmt.Errorf("fetch rules ETag: %w", err)
	}
	em.etag = etag
	return nil
}

// withETag acquires the mutex, ensures the ETag is set, calls fn with the current
// ETag, and updates the cached ETag on success. On ErrPreconditionFailed it
// re-fetches the ETag once and retries.
//
// fn should return the new ETag on success (empty string signals invalidation, e.g. delete).
func (em *etagManager) withETag(ctx context.Context, fn func(etag string) (string, error)) error {
	em.mu.Lock()
	defer em.mu.Unlock()

	if err := em.ensureETag(ctx); err != nil {
		return err
	}

	newEtag, err := fn(em.etag)
	if errors.Is(err, ErrPreconditionFailed) {
		// ETag is stale — re-fetch and retry once.
		_, freshEtag, listErr := em.client.ListRules(ctx, em.segment)
		if listErr != nil {
			return fmt.Errorf("re-fetch ETag after 412: %w", listErr)
		}
		em.etag = freshEtag
		newEtag, err = fn(em.etag)
	}
	if err != nil {
		return err
	}
	em.etag = newEtag
	return nil
}

func (em *etagManager) list(ctx context.Context) ([]MetricRule, error) {
	em.mu.Lock()
	defer em.mu.Unlock()
	rules, etag, err := em.client.ListRules(ctx, em.segment)
	if err != nil {
		return nil, err
	}
	em.etag = etag
	return rules, nil
}

func (em *etagManager) get(ctx context.Context, name string) (*MetricRule, error) {
	rule, err := em.client.GetRule(ctx, name, em.segment)
	if err != nil {
		return nil, err
	}
	return &rule, nil
}

func (em *etagManager) create(ctx context.Context, item *MetricRule) (*MetricRule, error) {
	if err := em.withETag(ctx, func(etag string) (string, error) {
		return em.client.CreateRule(ctx, *item, etag, em.segment)
	}); err != nil {
		return nil, err
	}
	return item, nil
}

func (em *etagManager) update(ctx context.Context, _ string, item *MetricRule) (*MetricRule, error) {
	if err := em.withETag(ctx, func(etag string) (string, error) {
		return em.client.UpdateRule(ctx, *item, etag, em.segment)
	}); err != nil {
		return nil, err
	}
	return item, nil
}

func (em *etagManager) delete(ctx context.Context, name string) error {
	return em.withETag(ctx, func(etag string) (string, error) {
		// Delete returns no ETag — returning "" invalidates the cached ETag so
		// the next mutation re-fetches a fresh one.
		return "", em.client.DeleteRule(ctx, name, etag, em.segment)
	})
}

// NewRuleTypedCRUD creates a TypedCRUD for adaptive metrics aggregation rules.
func NewRuleTypedCRUD(ctx context.Context, loader *providers.ConfigLoader, segment string) (*adapter.TypedCRUD[MetricRule], error) {
	client, err := newAdaptiveMetricsClient(ctx, loader)
	if err != nil {
		return nil, err
	}
	em := &etagManager{client: client, segment: segment}
	crud := &adapter.TypedCRUD[MetricRule]{
		ListFn:   em.list,
		GetFn:    em.get,
		CreateFn: em.create,
		UpdateFn: em.update,
		DeleteFn: em.delete,
		ValidateFn: func(ctx context.Context, items []*MetricRule) error {
			rules := make([]MetricRule, len(items))
			for i, r := range items {
				rules[i] = *r
			}
			errs, vErr := client.ValidateRules(ctx, rules, segment)
			if vErr != nil {
				return vErr
			}
			if len(errs) > 0 {
				return fmt.Errorf("validation: %s", strings.Join(errs, "; "))
			}
			return nil
		},
		Namespace:  "default",
		Descriptor: ruleDescriptorVar,
	}
	return crud, nil
}

// NewRuleAdapterFactory returns an adapter.Factory for adaptive metrics aggregation rules.
func NewRuleAdapterFactory(loader *providers.ConfigLoader) adapter.Factory {
	return func(ctx context.Context) (adapter.ResourceAdapter, error) {
		crud, err := NewRuleTypedCRUD(ctx, loader, "")
		if err != nil {
			return nil, err
		}
		return crud.AsAdapter(), nil
	}
}
