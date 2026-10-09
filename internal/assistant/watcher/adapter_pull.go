package watcher

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/grafana/gcx/internal/assistant/watchers"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"golang.org/x/sync/errgroup"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func (a *manifestAdapter) NewPullPreflight(selections []adapter.PullSelection) adapter.PullPreflight {
	preflight := newPullPreflight(a.client, selections)
	return preflight.check
}

// GetMultiple retains successful reference reads with explicit partial coverage.
func (a *manifestAdapter) GetMultiple(ctx context.Context, references []string, opts metav1.GetOptions) ([]unstructured.Unstructured, error) {
	items := make([]*unstructured.Unstructured, len(references))
	failures := make([]error, len(references))
	group, readCtx := errgroup.WithContext(ctx)
	group.SetLimit(10)
	for idx, reference := range references {
		group.Go(func() error {
			item, err := a.Get(readCtx, reference, opts)
			if err != nil {
				if readCtx.Err() != nil {
					return readCtx.Err()
				}
				failures[idx] = fmt.Errorf("pull Watcher reference %q: %w", reference, err)
			} else {
				items[idx] = item
			}
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result := make([]unstructured.Unstructured, 0, len(items))
	partial := &referenceReadError{}
	for idx, item := range items {
		if item != nil {
			result = append(result, *item)
		}
		if failures[idx] != nil {
			partial.failures = append(partial.failures, adapter.ReadFailure{Err: failures[idx]})
		}
	}
	if len(partial.failures) > 0 {
		return result, partial
	}
	return result, nil
}

type referenceReadError struct{ failures []adapter.ReadFailure }

func (e *referenceReadError) Error() string {
	parts := make([]string, 0, 1+len(e.failures))
	parts = append(parts, fmt.Sprintf("%d Watcher reference(s) failed to read", len(e.failures)))
	for _, failure := range e.failures {
		parts = append(parts, failure.Err.Error())
	}
	return strings.Join(parts, "; ")
}
func (e *referenceReadError) ReadFailures() []adapter.ReadFailure { return e.failures }
func (e *referenceReadError) SkippedReads() int                   { return 0 }
func (e *referenceReadError) Unwrap() []error {
	errs := make([]error, len(e.failures))
	for idx, failure := range e.failures {
		errs[idx] = failure.Err
	}
	return errs
}

// pullPreflight retains the identity-only index for one pull. It
// checks candidates across both visible archive partitions before the generic
// identity-keyed collection can replace fetched objects.
type pullPreflight struct {
	client   *watchers.Client
	loaded   bool
	index    map[string][]Candidate
	err      error
	selected map[string][]Candidate
}

func newPullPreflight(client *watchers.Client, selections []adapter.PullSelection) pullPreflight {
	p := pullPreflight{client: client, selected: make(map[string][]Candidate)}
	seen := make(map[string]bool)
	for _, selection := range selections {
		for _, item := range selection.Items {
			if item.GroupVersionKind() != WatcherDescriptor().GroupVersionKind() {
				continue
			}
			id := item.GetAnnotations()[WatcherIDAnnotation]
			if seen[id] {
				continue
			}
			seen[id] = true
			title, _, _ := unstructured.NestedString(item.Object, "spec", "title")
			name := item.GetName()
			p.selected[name] = append(p.selected[name], Candidate{ID: id, Title: title, Name: name})
		}
	}
	return p
}

func (p *pullPreflight) check(ctx context.Context, filter resources.Filter, item unstructured.Unstructured) error {
	if p.client == nil || filter.Descriptor.GroupVersionKind() != WatcherDescriptor().GroupVersionKind() {
		return nil
	}
	id := item.GetAnnotations()[WatcherIDAnnotation]
	explicitID := id != "" && slices.Contains(filter.ResourceUIDs, id)
	if len(p.selected[item.GetName()]) < 2 && (filter.Type == resources.FilterTypeSingle || explicitID) {
		// A single selection resolves names through the adapter and explicit
		// server IDs deliberately remain usable even when their slug collides.
		return nil
	}
	if !p.loaded {
		p.index, p.err = IdentityIndex(ctx, p.client)
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
		return CollisionError(item.GetName(), candidates)
	}
	return nil
}
