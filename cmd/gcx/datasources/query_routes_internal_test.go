package datasources

// Structural invariants of the generic query routing tables. These are the
// properties the type system cannot express: that a kind is dispatchable or
// redirect-only but never both, that keys are the normalized kind strings the
// command actually looks up, and that no entry is a usable-looking blank.

import (
	"strings"
	"testing"

	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueryRoutesTablesAreDisjoint(t *testing.T) {
	routes := newQueryRoutes()

	for kind := range routes.dispatch {
		_, alsoRedirects := routes.redirects[kind]
		assert.Falsef(t, alsoRedirects,
			"kind %q is both expression-dispatchable and redirect-only; it must be exactly one", kind)
	}
}

// Keys must be what dsquery.NormalizeKind produces, not raw plugin IDs — a
// route keyed "grafana-pyroscope-datasource" would never be reached.
//
// The fixed-point check alone is weak: NormalizeKind returns anything it does
// not recognize unchanged, so it only catches the plugin IDs already in its
// switch. The shape check carries the rest, because every raw Grafana plugin ID
// this command sees is either "grafana-*" or "*-datasource".
func TestQueryRoutesKeysAreNormalizedKinds(t *testing.T) {
	routes := newQueryRoutes()

	for _, kind := range routes.supportedKinds() {
		assert.Equalf(t, kind, dsquery.NormalizeKind(kind),
			"route key %q is not a normalized kind", kind)
		assert.Falsef(t, strings.HasPrefix(kind, "grafana-"),
			"route key %q looks like a raw plugin ID, not a normalized kind", kind)
		assert.Falsef(t, strings.HasSuffix(kind, "-datasource"),
			"route key %q looks like a raw plugin ID, not a normalized kind", kind)
	}
}

func TestQueryRoutesEntriesAreUsable(t *testing.T) {
	routes := newQueryRoutes()

	require.NotEmpty(t, routes.dispatch)
	require.NotEmpty(t, routes.redirects)

	for kind, dispatch := range routes.dispatch {
		assert.NotNilf(t, dispatch, "kind %q has a nil dispatch handler", kind)
	}

	for kind, message := range routes.redirects {
		assert.NotEmptyf(t, message, "kind %q has an empty redirect message", kind)
	}
}

func TestQueryRoutesUnknownKindResolvesToNeither(t *testing.T) {
	routes := newQueryRoutes()

	_, dispatchable := routes.dispatch["not-a-datasource"]
	assert.False(t, dispatchable)

	_, redirected := routes.redirects["not-a-datasource"]
	assert.False(t, redirected)
}

// The CloudWatch redirect shipped in v1.0.0. Templating it must not reword it.
func TestStructuredQueryRedirectMatchesShippedCloudWatchText(t *testing.T) {
	want := "CloudWatch queries are structured (namespace, metric, dimensions, region, statistic, period); " +
		"the generic `gcx datasources query <uid> <expr>` form can't carry them — " +
		"use `gcx datasources cloudwatch query --namespace ... --metric ... --region ...` instead"

	assert.Equal(t, want, newQueryRoutes().redirects["cloudwatch"])
}

// The supported list is derived, so it cannot drift from the tables again.
func TestQueryRoutesSupportedKindsIsTheSortedUnion(t *testing.T) {
	routes := newQueryRoutes()

	assert.Equal(t,
		[]string{"azuremonitor", "clickhouse", "cloudmonitoring", "cloudwatch", "influxdb", "loki", "postgres", "prometheus", "pyroscope"},
		routes.supportedKinds())

	assert.Len(t, routes.supportedKinds(), len(routes.dispatch)+len(routes.redirects),
		"every routed kind appears exactly once")
	assert.IsIncreasing(t, routes.supportedKinds(), "the list must be deterministic")
}
