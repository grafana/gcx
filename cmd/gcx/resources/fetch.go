package resources

import (
	"context"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/discovery"
	"github.com/grafana/gcx/internal/resources/remote"
)

type FetchRequest struct {
	Config             config.NamespacedRESTConfig
	StopOnError        bool
	ExcludeManaged     bool
	ExpectSingleTarget bool
	Processors         []remote.Processor
	// Limit caps the number of items per resource type. Zero means no limit.
	// Set to 1 for field discovery (--json ?) to avoid full list operations.
	Limit int64
}

type FetchResponse struct {
	Resources      resources.Resources
	Filters        resources.Filters
	IsSingleTarget bool
	PullSummary    *remote.OperationSummary
}

func FetchResources(ctx context.Context, opts FetchRequest, args []string) (*FetchResponse, error) {
	sels, err := resources.ParseSelectors(args)
	if err != nil {
		return nil, err
	}

	if opts.ExpectSingleTarget && !sels.IsSingleTarget() {
		return nil, fail.DetailedError{
			Summary: "Invalid resource selector",
			Details: "Expected a resource selector targeting a single resource. Example: dashboard/some-dashboard",
		}
	}

	reg, err := discovery.NewDefaultRegistry(ctx, opts.Config)
	if err != nil {
		return nil, err
	}

	filters, err := reg.MakeFilters(discovery.MakeFiltersOptions{
		Selectors:            sels,
		PreferredVersionOnly: true,
	})
	if err != nil {
		return nil, err
	}

	pull, err := remote.NewDefaultPuller(ctx, opts.Config)
	if err != nil {
		return nil, err
	}

	res := FetchResponse{
		Filters:        filters,
		IsSingleTarget: sels.IsSingleTarget(),
	}

	req := remote.PullRequest{
		Filters:        filters,
		Resources:      &res.Resources,
		Processors:     opts.Processors,
		ExcludeManaged: opts.ExcludeManaged,
		StopOnError:    opts.StopOnError || sels.IsSingleTarget(),
		Limit:          opts.Limit,
	}

	summary, err := pull.Pull(ctx, req)
	if err != nil {
		return nil, err
	}

	res.PullSummary = summary

	return &res, nil
}
