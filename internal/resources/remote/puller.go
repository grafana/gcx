package remote

import (
	"context"
	"log/slog"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/logs"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/discovery"
	"github.com/grafana/gcx/internal/resources/dynamic"
	"github.com/grafana/grafana-app-sdk/logging"
	"golang.org/x/sync/errgroup"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// PullClient is a client that can pull resources from Grafana.
type PullClient interface {
	Get(
		ctx context.Context, desc resources.Descriptor, name string, opts metav1.GetOptions,
	) (*unstructured.Unstructured, error)

	GetMultiple(
		ctx context.Context, desc resources.Descriptor, names []string, opts metav1.GetOptions,
	) ([]unstructured.Unstructured, error)

	List(
		ctx context.Context, desc resources.Descriptor, opts metav1.ListOptions,
	) (*unstructured.UnstructuredList, error)
}

// PullRegistry is a registry of resources that can be pulled from Grafana.
type PullRegistry interface {
	PreferredResources() resources.Descriptors
}

// Puller is a command that pulls resources from Grafana.
type Puller struct {
	client   PullClient
	registry PullRegistry
}

// NewDefaultPuller creates a new Puller.
// It uses a ResourceClientRouter that delegates to provider adapters for provider-backed
// resource types, and falls back to the default versioned dynamic client for native resources.
func NewDefaultPuller(ctx context.Context, restConfig config.NamespacedRESTConfig) (*Puller, error) {
	dynamicClient, err := dynamic.NewDefaultVersionedClient(restConfig)
	if err != nil {
		return nil, err
	}

	registry, err := discovery.NewDefaultRegistry(ctx, restConfig)
	if err != nil {
		return nil, err
	}

	router := buildRouter(dynamicClient, registry)

	return NewPuller(router, registry), nil
}

// NewPuller creates a new Puller.
func NewPuller(client PullClient, registry PullRegistry) *Puller {
	return &Puller{
		client:   client,
		registry: registry,
	}
}

// PullRequest is a request for pulling resources from Grafana.
type PullRequest struct {
	// Which resources to pull.
	Filters resources.Filters

	// Processors to apply to resources after they are pulled.
	Processors []Processor

	// Destination list for the pulled resources.
	Resources *resources.Resources

	// Whether to include resources managed by other tools.
	ExcludeManaged bool

	// Whether the operation should stop upon encountering an error.
	StopOnError bool

	// Limit caps the number of items returned per resource type. Zero means no limit.
	// Use Limit=1 for introspection operations (e.g. --json ? field discovery) to
	// avoid triggering a full list operation.
	Limit int64
}

// Pull pulls resources from Grafana.
func (p *Puller) Pull(ctx context.Context, req PullRequest) (*OperationSummary, error) {
	summary := &OperationSummary{}
	filters := req.Filters

	// If no filters are provided, we need to pull all available resources.
	if filters.IsEmpty() {
		// When pulling all resources, we need to use preferred versions.
		preferred := p.registry.PreferredResources()

		filters = make(resources.Filters, 0, len(preferred))
		for _, r := range preferred {
			filters = append(filters, resources.Filter{
				Type:       resources.FilterTypeAll,
				Descriptor: r,
			})
		}
	}

	logger := logging.FromContext(ctx)
	logger.Debug("Pulling resources")

	errg, ctx := errgroup.WithContext(ctx)
	partialRes := make([][]unstructured.Unstructured, len(filters))

	for idx, filt := range filters {
		errg.Go(func() error {
			switch filt.Type {
			case resources.FilterTypeAll:
				res, err := p.client.List(ctx, filt.Descriptor, metav1.ListOptions{Limit: req.Limit})
				if err != nil {
					switch {
					case isUnsupportedResourceType(err):
						// 404/405 = sub-resource that can't be listed; skip silently
						// regardless of StopOnError — these are never actionable.
						logger.Debug("Skipping unsupported resource type", logs.Err(err), slog.String("cmd", filt.String()))
						summary.RecordSkipped()
					case req.StopOnError:
						return err
					default:
						logger.Warn("Could not pull resources", logs.Err(err), slog.String("cmd", filt.String()))
						summary.RecordFailure(nil, err)
					}
				} else {
					if res.GetContinue() != "" {
						summary.RecordTruncated()
					}
					partialRes[idx] = res.Items
				}
			case resources.FilterTypeMultiple:
				res, err := p.client.GetMultiple(ctx, filt.Descriptor, filt.ResourceUIDs, metav1.GetOptions{})
				if err != nil {
					switch {
					case isUnsupportedResourceType(err):
						// 404/405 = sub-resource that can't be listed; skip silently
						// regardless of StopOnError — these are never actionable.
						logger.Debug("Skipping unsupported resource type", logs.Err(err), slog.String("cmd", filt.String()))
						summary.RecordSkipped()
					case req.StopOnError:
						return err
					default:
						logger.Warn("Could not pull resources", logs.Err(err), slog.String("cmd", filt.String()))
						summary.RecordFailure(nil, err)
					}
				} else {
					partialRes[idx] = res
				}
			case resources.FilterTypeSingle:
				res, err := p.client.Get(ctx, filt.Descriptor, filt.ResourceUIDs[0], metav1.GetOptions{})
				if err != nil {
					if req.StopOnError {
						return err
					}
					logger.Warn("Could not pull resource", logs.Err(err), slog.String("cmd", filt.String()))
					summary.RecordFailure(nil, err)
				} else {
					partialRes[idx] = []unstructured.Unstructured{*res}
				}
			}
			return nil
		})
	}

	if err := errg.Wait(); err != nil {
		return summary, err
	}

	req.Resources.Clear()
	for _, r := range partialRes {
		for _, item := range r {
			res, err := resources.FromUnstructured(&item)
			if err != nil {
				return summary, err
			}

			// TODO: this should be replaced by a more generic mechanism,
			// e.g. label & annotation filters.
			if !res.IsManaged() && req.ExcludeManaged {
				continue
			}

			if err := p.process(res, req.Processors); err != nil {
				if req.StopOnError {
					return summary, err
				}

				logger.Warn("Failed to process resource", logs.Err(err))
				summary.RecordFailure(res, err)
			} else {
				req.Resources.Add(res)
				summary.RecordSuccess()
			}
		}
	}

	return summary, nil
}

// isUnsupportedResourceType reports whether a LIST/GET error indicates that the
// resource type is registered in API discovery but does not actually support the
// requested operation. These are common for datasource sub-resources (connections,
// queryconvert) and other internal Grafana types.
//
// 404 (Not Found) and 405 (Method Not Allowed) are treated as "not listable" and
// silently skipped. Other status codes (403, 500, 503, …) are still actionable
// and reported as warnings.
func isUnsupportedResourceType(err error) bool {
	return apierrors.IsNotFound(err) || apierrors.IsMethodNotSupported(err)
}

func (p *Puller) process(res *resources.Resource, processors []Processor) error {
	for _, processor := range processors {
		if err := processor.Process(res); err != nil {
			return err
		}
	}

	return nil
}
