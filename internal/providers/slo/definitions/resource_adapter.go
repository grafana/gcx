package definitions

import (
	"context"

	"github.com/grafana/gcx/internal/resources/adapter"
)

// SloResource declares the SLO definitions resource type end-to-end for
// adapter.NewProvider: identity (GVK), registration metadata, and client
// constructor. Declaring NaturalKey folds in
// RegisterNaturalKey(gvk, SpecFieldKey("name")) and declaring URLTemplate
// folds in the deeplink registration — neither needs a separate init().
// Schema and Example are derived from Slo/the value below rather than
// hand-written. A function (not a package-level var) to avoid a mutable
// shared global.
func SloResource() adapter.Resource[Slo] {
	return adapter.Resource[Slo]{
		Group:   "slo.ext.grafana.app",
		Version: "v1alpha1",
		Kind:    "SLO",

		NaturalKey:  "name",
		URLTemplate: "/a/grafana-slo-app/slo/{name}",
		StripFields: []string{"uuid", "readOnly"},

		Example: &Slo{
			UUID:        "my-slo",
			Name:        "HTTP Availability",
			Description: "Tracks HTTP request success rate",
			Query: Query{
				Type: "freeform",
				Freeform: &FreeformQuery{
					Query: `sum(rate(http_requests_total{status!~"5.."}[5m])) / sum(rate(http_requests_total[5m]))`,
				},
			},
			Objectives: []Objective{{Value: 0.995, Window: "28d"}},
			Labels:     []Label{{Key: "team", Value: "platform"}},
		},

		NewClient: newAdapterClient,
	}
}

// newAdapterClient is SloResource's NewClient implementation. It builds the
// SLO client directly from ClientDeps.HTTP, constructing no transport of its
// own. The returned *Client implements the five capability interfaces (see
// the compile-time guards in client.go), so the capability seam wires all
// five verbs.
func newAdapterClient(_ context.Context, deps adapter.ClientDeps) (any, error) {
	return newClientFromDeps(deps), nil
}
