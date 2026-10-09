package remote

import (
	"context"
	"errors"
	"log/slog"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/logs"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/resources/discovery"
	"github.com/grafana/gcx/internal/resources/dynamic"
	"github.com/grafana/grafana-app-sdk/logging"
	"golang.org/x/sync/errgroup"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// defaultMaxConcurrentListRequests bounds the per-resource-type fan-out in Pull
// (one List/Get per filter) by default, matching the fan-out cap used elsewhere
// in the resources pipeline. Override via WithMaxConcurrentListRequests.
const defaultMaxConcurrentListRequests = 10

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
	client                    PullClient
	registry                  PullRegistry
	maxConcurrentListRequests int
}

// PullerOption configures a Puller.
type PullerOption func(*Puller)

// WithMaxConcurrentListRequests overrides the default fan-out cap (10) used by
// Pull to bound concurrent per-resource-type List/Get requests.
func WithMaxConcurrentListRequests(n int) PullerOption {
	return func(p *Puller) {
		p.maxConcurrentListRequests = n
	}
}

// NewDefaultPullerWithRegistry creates a Puller reusing an already-built discovery
// registry, avoiding a redundant discovery index build + adapter.RegisterAll when
// the caller has already constructed one.
// It uses a ResourceClientRouter that delegates to provider adapters for provider-backed
// resource types, and falls back to the default versioned dynamic client for native resources.
func NewDefaultPullerWithRegistry(
	restConfig config.NamespacedRESTConfig, registry *discovery.Registry, opts ...PullerOption,
) (*Puller, error) {
	dynamicClient, err := dynamic.NewDefaultVersionedClient(restConfig)
	if err != nil {
		return nil, err
	}

	router := buildRouter(dynamicClient, registry)

	return NewPuller(router, registry, opts...), nil
}

// NewPuller creates a new Puller.
func NewPuller(client PullClient, registry PullRegistry, opts ...PullerOption) *Puller {
	p := &Puller{
		client:                    client,
		registry:                  registry,
		maxConcurrentListRequests: defaultMaxConcurrentListRequests,
	}

	for _, opt := range opts {
		opt(p)
	}

	return p
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
	pullCtx := ctx
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
	errg.SetLimit(p.maxConcurrentListRequests)
	partialRes := make([][]unstructured.Unstructured, len(filters))

	for idx, filt := range filters {
		errg.Go(func() error {
			var err error
			partialRes[idx], err = p.pullFilter(ctx, req, filt, summary)
			return err
		})
	}

	if err := errg.Wait(); err != nil {
		return summary, err
	}

	// Adapter checks run while fetched batches still retain every identity.
	var preflight adapter.PullPreflight
	if client, ok := p.client.(interface {
		NewPullPreflight(ctx context.Context, selections []adapter.PullSelection) (adapter.PullPreflight, error)
	}); ok {
		selections := make([]adapter.PullSelection, len(filters))
		for idx, filter := range filters {
			selections[idx] = adapter.PullSelection{Filter: filter, Items: partialRes[idx]}
		}
		var err error
		preflight, err = client.NewPullPreflight(pullCtx, selections)
		if err != nil {
			return summary, err
		}
	}

	req.Resources.Clear()
	for idx, r := range partialRes {
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

			if preflight != nil {
				if err := preflight(pullCtx, filters[idx], item); err != nil {
					logger.Warn("Failed resource identity preflight", logs.Err(err))
					summary.RecordFailure(res, err)
					if req.StopOnError {
						return summary, err
					}
					continue
				}
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

// pullFilter fetches one selection and records partial coverage before the
// caller processes or inserts any of its items.
func (p *Puller) pullFilter(ctx context.Context, req PullRequest, filt resources.Filter, summary *OperationSummary) ([]unstructured.Unstructured, error) {
	logger := logging.FromContext(ctx)
	var result []unstructured.Unstructured

	switch filt.Type {
	case resources.FilterTypeAll:
		res, err := p.client.List(ctx, filt.Descriptor, metav1.ListOptions{Limit: req.Limit})
		var partial adapter.PartialReadError
		if errors.As(err, &partial) {
			if recordErr := recordPartialRead(ctx, summary, partial, req.StopOnError); recordErr != nil {
				return nil, recordErr
			}
			if res != nil {
				result = res.Items
			}
			return result, nil
		}
		if err != nil {
			switch {
			case isUnsupportedResourceType(err):
				// 404/405 = sub-resource that can't be listed; skip silently
				// regardless of StopOnError — these are never actionable.
				logger.Debug("Skipping unsupported resource type", logs.Err(err), slog.String("cmd", filt.String()))
				summary.RecordSkipped()
			case req.StopOnError:
				return nil, err
			default:
				logger.Warn("Could not pull resources", logs.Err(err), slog.String("cmd", filt.String()))
				summary.RecordFailure(nil, err)
			}
		} else {
			if res.GetContinue() != "" {
				summary.RecordTruncated()
			}
			result = res.Items
		}
	case resources.FilterTypeMultiple:
		res, err := p.client.GetMultiple(ctx, filt.Descriptor, filt.ResourceUIDs, metav1.GetOptions{})
		var partial adapter.PartialReadError
		if errors.As(err, &partial) {
			if recordErr := recordPartialRead(ctx, summary, partial, req.StopOnError); recordErr != nil {
				return nil, recordErr
			}
			result = res
			return result, nil
		}
		if err != nil {
			switch {
			case isUnsupportedResourceType(err):
				// 404/405 = sub-resource that can't be listed; skip silently
				// regardless of StopOnError — these are never actionable.
				logger.Debug("Skipping unsupported resource type", logs.Err(err), slog.String("cmd", filt.String()))
				summary.RecordSkipped()
			case req.StopOnError:
				return nil, err
			default:
				logger.Warn("Could not pull resources", logs.Err(err), slog.String("cmd", filt.String()))
				summary.RecordFailure(nil, err)
			}
		} else {
			result = res
		}
	case resources.FilterTypeSingle:
		res, err := p.client.Get(ctx, filt.Descriptor, filt.ResourceUIDs[0], metav1.GetOptions{})
		if err != nil {
			if req.StopOnError {
				return nil, err
			}
			logger.Warn("Could not pull resource", logs.Err(err), slog.String("cmd", filt.String()))
			summary.RecordFailure(nil, err)
		} else {
			result = []unstructured.Unstructured{*res}
		}
	}
	return result, nil
}

// recordPartialRead preserves adapter-provided per-item failures and skips.
func recordPartialRead(ctx context.Context, summary *OperationSummary, partial adapter.PartialReadError, stopOnError bool) error {
	failures := partial.ReadFailures()
	for _, failure := range failures {
		var failed *resources.Resource
		if failure.Resource != nil {
			var err error
			failed, err = resources.FromUnstructured(failure.Resource)
			if err != nil {
				return err
			}
		}
		summary.RecordFailure(failed, failure.Err)
		logging.FromContext(ctx).Warn("Could not pull resource", logs.Err(failure.Err))
	}
	for range partial.SkippedReads() {
		summary.RecordSkipped()
	}
	if stopOnError && len(failures) > 0 {
		return partial
	}
	return nil
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
