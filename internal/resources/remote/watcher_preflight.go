package remote

import (
	"context"
	"fmt"
	"slices"

	"github.com/grafana/gcx/internal/assistant/watcher"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/logs"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/grafana-app-sdk/logging"
	"golang.org/x/sync/errgroup"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func isWatcherFilter(filter resources.Filter) bool {
	return filter.Descriptor.GroupVersionKind() == watcher.WatcherDescriptor().GroupVersionKind()
}

// pullWatcherReferences retains successful selections when a different name
// cannot be resolved. Router.GetMultiple is atomic; using it here would discard
// unaffected Watchers before the collision preflight can inspect the batch.
func (p *Puller) pullWatcherReferences(ctx context.Context, filter resources.Filter, stopOnError bool, summary *OperationSummary) ([]unstructured.Unstructured, error) {
	items := make([]*unstructured.Unstructured, len(filter.ResourceUIDs))
	group, readCtx := errgroup.WithContext(ctx)
	group.SetLimit(defaultMaxConcurrentListRequests)
	for idx, reference := range filter.ResourceUIDs {
		group.Go(func() error {
			item, err := p.client.Get(readCtx, filter.Descriptor, reference, metav1.GetOptions{})
			if err != nil {
				err = fmt.Errorf("pull Watcher reference %q: %w", reference, err)
				summary.RecordFailure(nil, err)
				if stopOnError {
					return err
				}
				logging.FromContext(ctx).Warn("Could not pull Watcher", logs.Err(err))
				return nil
			}
			items[idx] = item
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}
	result := make([]unstructured.Unstructured, 0, len(items))
	for _, item := range items {
		if item != nil {
			result = append(result, *item)
		}
	}
	return result, nil
}

// watcherPullPreflight retains the identity-only index for one pull. The
// Watcher package owns enumeration and ambiguity semantics; this shared glue
// checks fetched objects before the identity-keyed collection can replace them.
type watcherPullPreflight struct {
	config   *config.NamespacedRESTConfig
	loaded   bool
	index    map[string][]watcher.Candidate
	err      error
	selected map[string][]watcher.Candidate
}

func newWatcherPullPreflight(cfg *config.NamespacedRESTConfig, batches [][]unstructured.Unstructured) watcherPullPreflight {
	p := watcherPullPreflight{config: cfg, selected: make(map[string][]watcher.Candidate)}
	seen := make(map[string]bool)
	for _, batch := range batches {
		for _, item := range batch {
			if item.GroupVersionKind() != watcher.WatcherDescriptor().GroupVersionKind() {
				continue
			}
			id := item.GetAnnotations()[watcher.WatcherIDAnnotation]
			if seen[id] {
				continue
			}
			seen[id] = true
			title, _, _ := unstructured.NestedString(item.Object, "spec", "title")
			name := item.GetName()
			p.selected[name] = append(p.selected[name], watcher.Candidate{ID: id, Title: title, Name: name})
		}
	}
	return p
}

func (p *watcherPullPreflight) check(ctx context.Context, filter resources.Filter, item unstructured.Unstructured) error {
	if p.config == nil || !isWatcherFilter(filter) {
		return nil
	}
	id := item.GetAnnotations()[watcher.WatcherIDAnnotation]
	explicitID := id != "" && slices.Contains(filter.ResourceUIDs, id)
	if len(p.selected[item.GetName()]) < 2 && (filter.Type == resources.FilterTypeSingle || explicitID) {
		// A single selection resolves names through the adapter and explicit
		// server IDs deliberately remain usable even when their slug collides.
		return nil
	}
	if !p.loaded {
		p.index, p.err = watcher.IdentityIndexForConfig(ctx, *p.config)
		p.loaded = true
	}
	if p.err != nil {
		return p.err
	}
	candidates := p.index[item.GetName()]
	if len(candidates) < 2 {
		candidates = p.selected[item.GetName()]
	}
	if len(candidates) > 1 {
		return watcher.CollisionError(item.GetName(), candidates)
	}
	return nil
}
