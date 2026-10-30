package remote

import (
	"context"
	"errors"
	"fmt"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/logs"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/resources/discovery"
	"github.com/grafana/gcx/internal/resources/dynamic"
	"github.com/grafana/grafana-app-sdk/logging"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// DeleteClient is a client that can delete resources from Grafana.
type DeleteClient interface {
	Delete(ctx context.Context, desc resources.Descriptor, name string, opts metav1.DeleteOptions) error
}

// Deleter takes care of deleting resources from Grafana.
type Deleter struct {
	client   DeleteClient
	registry PushRegistry
}

// NewDeleter creates a new Deleter.
// It uses a ResourceClientRouter that delegates to provider adapters for provider-backed
// resource types, and falls back to the default namespaced dynamic client for native resources.
// The dynamic fallback is wrapped in a dry-run guard (configured by opts) so --dry-run never
// silently deletes a resource whose API ignores server-side dryRun.
func NewDeleter(ctx context.Context, cfg config.NamespacedRESTConfig, guard GuardConfig) (*Deleter, error) {
	dynamicClient, err := dynamic.NewDefaultNamespacedClient(cfg)
	if err != nil {
		return nil, err
	}

	guarded := newGuardedRetryDynamicClient(dynamicClient, guard)

	registry, err := discovery.NewDefaultRegistry(ctx, cfg)
	if err != nil {
		return nil, err
	}

	router := buildRouter(guarded, registry)

	return &Deleter{
		client:   router,
		registry: registry,
	}, nil
}

// NewDeleterWithClient creates a new Deleter with the given client and registry.
// This is primarily useful for testing.
func NewDeleterWithClient(client DeleteClient, registry PushRegistry) *Deleter {
	return &Deleter{
		client:   client,
		registry: registry,
	}
}

// DeleteRequest is a request for deleting resources from Grafana.
type DeleteRequest struct {
	// A list of resources to delete.
	Resources *resources.Resources

	// The maximum number of concurrent pushes.
	MaxConcurrency int

	// Whether the operation should stop upon encountering an error.
	StopOnError bool

	// If set to true, the deleter will simulate the delete operations.
	DryRun bool

	// ResolveLocalIdentities selects strict identities from complete local envelopes
	// before any delete. Selector-based callers already performed this selection.
	ResolveLocalIdentities bool
}

type deleteSelectionClient interface {
	SelectDelete(ctx context.Context, filter resources.Filter, manifests []unstructured.Unstructured) ([]unstructured.Unstructured, bool, error)
}

func (deleter *Deleter) Delete(ctx context.Context, request DeleteRequest) (*OperationSummary, error) {
	summary := &OperationSummary{}
	supported := deleter.supportedDescriptors()

	if request.MaxConcurrency < 1 {
		request.MaxConcurrency = 1
	}
	if request.ResolveLocalIdentities {
		selected, err := deleter.selectLocalIdentities(ctx, request, supported, summary)
		if err != nil {
			return summary, err
		}
		request.Resources = selected
	}

	err := request.Resources.ForEachConcurrently(ctx, request.MaxConcurrency,
		func(ctx context.Context, res *resources.Resource) error {
			name := res.Name()
			// Collapse alternate API groups onto their canonical, statically-registered
			// GVK so a fetched object carrying a per-plugin group — e.g.
			// prometheus.datasource.grafana.app/v0alpha1 — routes to the right
			// adapter. A no-op when no normalizer matches.
			gvk := resources.NormalizeGVK(res.GroupVersionKind())

			logger := logging.FromContext(ctx).With(
				"gvk", gvk,
				"name", name,
			)

			desc, ok := supported[gvk]
			if !ok {
				if request.StopOnError {
					return fmt.Errorf("resource not supported by the API: %s/%s", gvk, name)
				}

				logger.Warn("Skipping resource not supported by the API")
				return nil
			}

			if err := deleter.deleteResource(ctx, desc, res, request.DryRun); err != nil {
				// The guard blocked a dry-run delete against an API that ignores server-side
				// dryRun. Record it as skipped (not a failure) and keep going, like the puller.
				if errors.Is(err, errReadOnlyNative) {
					summary.RecordSkipped("read-only")
					logger.Info(err.Error())
					return nil
				}
				if errors.Is(err, errDryRunUnverified) {
					summary.RecordSkipped("not server-verified")
					logger.Info("Dry-run: deletion not verified (server-side dry-run unsupported); no delete sent")
					return nil
				}

				summary.RecordFailure(res, err)
				if request.StopOnError {
					return err
				}

				logger.Warn("Failed to delete resource", "source", res.SourcePath(), logs.Err(err))
				return nil
			}

			logger.Info("Resource deleted")
			summary.RecordSuccess()
			return nil
		},
	)
	if err != nil {
		return summary, err
	}

	return summary, nil
}

func (deleter *Deleter) deleteResource(ctx context.Context, descriptor resources.Descriptor, res *resources.Resource, dryRun bool) error {
	if err := adapter.CheckMutation(descriptor, "delete"); err != nil {
		return err
	}
	if resources.IsReadOnlyNative(&res.Object) {
		return fmt.Errorf("%w: %w", errReadOnlyNative, resources.ReadOnlyNativeError(&res.Object))
	}
	if res.Name() == "" && resources.HasServerDerivedName(res.GroupVersionKind()) {
		field, command := "name", "time-intervals"
		if res.Kind() == "TemplateGroup" {
			field, command = "title", "template-groups"
		}
		title, _, _ := unstructured.NestedString(res.Object.Object, "spec", field)
		if title == "" {
			title = "<title>"
		}
		return fmt.Errorf("%s %q has no metadata.name; delete it with 'gcx alert %s delete %q' or use a pulled manifest with its real API name", res.Kind(), title, command, title)
	}
	var dryRunOpts []string
	if dryRun {
		dryRunOpts = []string{"All"}
	}

	return deleter.client.Delete(ctx, descriptor, res.Name(), metav1.DeleteOptions{
		DryRun: dryRunOpts,
	})
}

func (deleter *Deleter) supportedDescriptors() map[schema.GroupVersionKind]resources.Descriptor {
	supported := deleter.registry.SupportedResources()

	supportedDescriptors := make(map[schema.GroupVersionKind]resources.Descriptor)
	for _, sup := range supported {
		supportedDescriptors[sup.GroupVersionKind()] = sup
	}

	return supportedDescriptors
}

// selectLocalIdentities retains each authored envelope through owned selection.
// Selection failures cannot allow any manifest in the failed kind to be deleted.
func (deleter *Deleter) selectLocalIdentities(ctx context.Context, request DeleteRequest, supported map[schema.GroupVersionKind]resources.Descriptor, summary *OperationSummary) (*resources.Resources, error) {
	client, ok := deleter.client.(deleteSelectionClient)
	if !ok {
		return request.Resources, nil
	}
	grouped := make(map[schema.GroupVersionKind][]*resources.Resource)
	_ = request.Resources.ForEach(func(res *resources.Resource) error {
		gvk := resources.NormalizeGVK(res.GroupVersionKind())
		grouped[gvk] = append(grouped[gvk], res)
		return nil
	})
	selected := resources.NewResources()
	for gvk, sources := range grouped {
		desc, exists := supported[gvk]
		if !exists || adapter.CheckMutation(desc, "delete") != nil {
			for _, source := range sources {
				selected.AddDistinct(source)
			}
			continue
		}
		manifests := make([]unstructured.Unstructured, len(sources))
		for i, source := range sources {
			manifests[i] = source.ToUnstructured()
		}
		items, handled, err := client.SelectDelete(ctx, resources.Filter{Type: resources.FilterTypeAll, Descriptor: desc}, manifests)
		if !handled {
			for _, source := range sources {
				selected.AddDistinct(source)
			}
			continue
		}
		if err != nil {
			for _, source := range sources {
				summary.RecordFailure(source, err)
			}
			if request.StopOnError {
				return selected, err
			}
			continue
		}
		for i := range items {
			res, err := resources.FromUnstructured(&items[i])
			if err != nil {
				return selected, err
			}
			if len(items) == len(sources) {
				res.SetSource(sources[i].Source)
			}
			selected.Add(res)
		}
	}
	return selected, nil
}
