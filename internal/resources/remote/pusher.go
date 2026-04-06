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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// PushRegistry is a registry of resources that can be pushed to Grafana.
type PushRegistry interface {
	SupportedResources() resources.Descriptors
}

// PushLister is an optional interface that PushClient implementations may satisfy
// to enable listing resources on the target. Used for natural-key matching during
// cross-stack push operations.
type PushLister interface {
	List(ctx context.Context, desc resources.Descriptor, opts metav1.ListOptions) (*unstructured.UnstructuredList, error)
}

// PushClient is a client that can push resources to Grafana.
type PushClient interface {
	Create(
		ctx context.Context, desc resources.Descriptor, obj *unstructured.Unstructured, opts metav1.CreateOptions,
	) (*unstructured.Unstructured, error)

	Update(
		ctx context.Context, desc resources.Descriptor, obj *unstructured.Unstructured, opts metav1.UpdateOptions,
	) (*unstructured.Unstructured, error)

	Get(
		ctx context.Context, desc resources.Descriptor, name string, opts metav1.GetOptions,
	) (*unstructured.Unstructured, error)
}

// Pusher takes care of pushing resources to Grafana API.
type Pusher struct {
	client   PushClient
	registry PushRegistry
}

// NewDefaultPusher creates a new Pusher.
// It uses a ResourceClientRouter that delegates to provider adapters for provider-backed
// resource types, and falls back to the default namespaced dynamic client for native resources.
func NewDefaultPusher(ctx context.Context, cfg config.NamespacedRESTConfig) (*Pusher, error) {
	dynamicClient, err := dynamic.NewDefaultNamespacedClient(cfg)
	if err != nil {
		return nil, err
	}

	registry, err := discovery.NewDefaultRegistry(ctx, cfg)
	if err != nil {
		return nil, err
	}

	router := buildRouter(dynamicClient, registry)

	return NewPusher(router, registry), nil
}

// NewPusher creates a new Pusher.
func NewPusher(client PushClient, registry PushRegistry) *Pusher {
	return &Pusher{
		client:   client,
		registry: registry,
	}
}

// PushRequest is a request for pushing resources to Grafana.
type PushRequest struct {
	// A list of resources to push.
	Resources *resources.Resources

	// Processors to apply to resources before pushing them.
	Processors []Processor

	// The maximum number of concurrent pushes.
	MaxConcurrency int

	// Whether the operation should stop upon encountering an error.
	StopOnError bool

	// If set to true, the pusher will use the server-side dry-run feature to simulate the push operations.
	// This will not actually create or update any resources,
	// but will ensure the requests are valid and perform server-side validations.
	DryRun bool

	// Disable log emission for push failures. Callers will have to rely on the OperationSummary
	// returned by the Push() function to explore and report failures.
	NoPushFailureLog bool

	// Whether to include resources managed by other tools.
	IncludeManaged bool
}

// Push pushes resources to Grafana.
// It pushes folders first (respecting parent-child hierarchy), then other resources.
// This ensures that parent folders are created before their children,
// and all folders are created before other resources that depend on them.
func (p *Pusher) Push(ctx context.Context, request PushRequest) (*OperationSummary, error) {
	summary := &OperationSummary{}
	supported := p.supportedDescriptors()

	if request.MaxConcurrency < 1 {
		request.MaxConcurrency = 1
	}

	// Create natural key cache for cross-stack matching.
	var lister PushLister
	if l, ok := p.client.(PushLister); ok {
		lister = l
	}
	nkCache := newNaturalKeyCache(lister)

	// Phase 1: Push folders in hierarchical order
	if err := p.pushFolders(ctx, request, supported, summary, nkCache); err != nil {
		return summary, err
	}

	// If all resources were folders, we're done
	if summary.SuccessCount()+summary.FailedCount() >= request.Resources.Len() {
		return summary, nil
	}

	// Phase 2: Push all other (non-folder) resources
	if err := request.Resources.ForEachConcurrently(
		ctx, request.MaxConcurrency, func(ctx context.Context, res *resources.Resource) error {
			if res.IsFolder() {
				return nil
			}

			return p.pushSingleResource(ctx, res, supported, summary, request, nkCache)
		},
	); err != nil {
		return summary, err
	}

	return summary, nil
}

// pushFolders pushes folder resources in hierarchical order (parent before child).
// Folders are grouped by dependency level and pushed level-by-level.
// All folders at the same level can be pushed concurrently.
func (p *Pusher) pushFolders(
	ctx context.Context,
	request PushRequest,
	supported map[schema.GroupVersionKind]resources.Descriptor,
	summary *OperationSummary,
	nkCache *naturalKeyCache,
) error {
	// Collect all folder resources
	var folders []*resources.Resource
	_ = request.Resources.ForEach(func(res *resources.Resource) error {
		if res.IsFolder() {
			folders = append(folders, res)
		}
		return nil
	})

	// Sort folders by dependency levels (parent folders before children)
	folderLevels, err := SortFoldersByDependency(folders)
	if err != nil {
		return err
	}

	// Push folders level by level
	// All folders at the same level can be pushed concurrently
	for _, levelFolders := range folderLevels {
		levelResources := resources.NewResources(levelFolders...)
		if err := levelResources.ForEachConcurrently(
			ctx, request.MaxConcurrency, func(ctx context.Context, res *resources.Resource) error {
				return p.pushSingleResource(ctx, res, supported, summary, request, nkCache)
			},
		); err != nil {
			return err
		}
	}

	return nil
}

// pushSingleResource pushes a single resource and handles common error scenarios.
func (p *Pusher) pushSingleResource(
	ctx context.Context,
	res *resources.Resource,
	supported map[schema.GroupVersionKind]resources.Descriptor,
	summary *OperationSummary,
	request PushRequest,
	nkCache *naturalKeyCache,
) error {
	name := res.Name()
	gvk := res.GroupVersionKind()

	logger := logging.FromContext(ctx).With(
		"gvk", gvk,
		"name", name,
		"dryRun", request.DryRun,
	)

	desc, ok := supported[gvk]
	if !ok {
		err := fmt.Errorf("resource not supported by the API: %s/%s", gvk, name)
		summary.RecordFailure(res, err)

		if request.StopOnError {
			return err
		}

		if !request.NoPushFailureLog {
			logger.Warn("Skipping resource not supported by the API")
		}
		return nil
	}

	for _, processor := range request.Processors {
		if err := processor.Process(res); err != nil {
			summary.RecordFailure(res, err)

			if request.StopOnError {
				return err
			}

			if !request.NoPushFailureLog {
				logger.Warn("Failed to process resource", logs.Err(err))
			}

			return nil
		}
	}

	if !res.IsManaged() && !request.IncludeManaged {
		logger.Info(fmt.Sprintf("Skipping resource managed by %s", res.GetManagerKind()))
		return nil
	}

	if err := p.upsertResource(ctx, desc, name, res, request.DryRun, logger, nkCache); err != nil {
		summary.RecordFailure(res, err)

		if request.StopOnError {
			return err
		}

		if !request.NoPushFailureLog {
			logger.Warn("Failed to push resource", logs.Err(err))
		}
		return nil
	}

	logger.Info("Resource pushed")
	summary.RecordSuccess()
	return nil
}

func (p *Pusher) upsertResource(
	ctx context.Context, desc resources.Descriptor, name string, src *resources.Resource, dryRun bool, log logging.Logger, nkCache *naturalKeyCache,
) error {
	var dryRunOpts []string
	if dryRun {
		dryRunOpts = []string{"All"}
	}

	// Check if the resource already exists.
	existing, err := p.client.Get(ctx, desc, name, metav1.GetOptions{})
	if err == nil {
		obj := src.ToUnstructured()

		// Copy the resourceVersion from the existing resource so the API accepts the update.
		obj.SetResourceVersion(existing.GetResourceVersion())

		if _, err := p.client.Update(ctx, desc, &obj, metav1.UpdateOptions{
			DryRun: dryRunOpts,
		}); err != nil {
			return err
		}

		log.Info("Resource updated")
		return nil
	}

	// If the error is not a NotFound, it's an unexpected API error — surface it.
	if !apierrors.IsNotFound(err) {
		return err
	}

	// Resource does not exist — try natural key matching for cross-stack push.
	obj := src.ToUnstructured()

	// The metadata.name may be a server-generated ID from a different stack.
	// Look for a resource with the same content identity.
	remoteName, rv, found, nkErr := findByNaturalKey(ctx, nkCache, desc, &obj)
	if nkErr != nil {
		return nkErr
	}
	if found {
		obj.SetName(remoteName)
		obj.SetResourceVersion(rv)
		if _, err := p.client.Update(ctx, desc, &obj, metav1.UpdateOptions{
			DryRun: dryRunOpts,
		}); err != nil {
			return err
		}
		log.Info("Resource updated via natural key match", "remoteName", remoteName)
		return nil
	}

	// No natural key match — create as new resource.
	if _, err := p.client.Create(ctx, desc, &obj, metav1.CreateOptions{
		DryRun: dryRunOpts,
	}); err != nil {
		return err
	}

	log.Info("Resource created")
	return nil
}

func (p *Pusher) supportedDescriptors() map[schema.GroupVersionKind]resources.Descriptor {
	supported := p.registry.SupportedResources()

	supportedDescriptors := make(map[schema.GroupVersionKind]resources.Descriptor)
	for _, sup := range supported {
		supportedDescriptors[sup.GroupVersionKind()] = sup
	}

	return supportedDescriptors
}
