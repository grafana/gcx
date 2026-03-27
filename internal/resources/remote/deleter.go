package remote

import (
	"context"
	"fmt"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/logs"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/discovery"
	"github.com/grafana/gcx/internal/resources/dynamic"
	"github.com/grafana/grafana-app-sdk/logging"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
func NewDeleter(ctx context.Context, cfg config.NamespacedRESTConfig) (*Deleter, error) {
	dynamicClient, err := dynamic.NewDefaultNamespacedClient(cfg)
	if err != nil {
		return nil, err
	}

	registry, err := discovery.NewDefaultRegistry(ctx, cfg)
	if err != nil {
		return nil, err
	}

	router := buildRouter(dynamicClient, registry)

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
}

func (deleter *Deleter) Delete(ctx context.Context, request DeleteRequest) (*OperationSummary, error) {
	summary := &OperationSummary{}
	supported := deleter.supportedDescriptors()

	if request.MaxConcurrency < 1 {
		request.MaxConcurrency = 1
	}

	err := request.Resources.ForEachConcurrently(ctx, request.MaxConcurrency,
		func(ctx context.Context, res *resources.Resource) error {
			name := res.Name()
			gvk := res.GroupVersionKind()

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
				summary.RecordFailure(res, err)
				if request.StopOnError {
					return err
				}

				logger.Warn("Failed to delete resource", logs.Err(err))
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
