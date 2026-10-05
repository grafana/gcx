package sandbox

import (
	"fmt"
	"net/http"
	pathpkg "path"
	"slices"
	"strings"
)

// Access is how much an HTTP request may do. Pass RequireAccess(level) as
// Invocation.Authorize to cap an invocation at level.
//
// The rule mirrors the Grafana Assistant gcx proxy: the HTTP method decides,
// except for POSTs to endpoints that only read, such as datasource queries.
type Access int

const (
	// AccessRead permits GET, HEAD and OPTIONS, plus POSTs to read-shaped
	// endpoints such as datasource queries.
	AccessRead Access = iota
	// AccessWrite additionally permits POST, PUT and PATCH.
	AccessWrite
	// AccessDelete additionally permits DELETE and any other method.
	AccessDelete
)

func (a Access) String() string {
	switch a {
	case AccessRead:
		return "read-only"
	case AccessWrite:
		return "read-write"
	case AccessDelete:
		return "full"
	default:
		return fmt.Sprintf("Access(%d)", int(a))
	}
}

// RequireAccess returns an authorizer, for Invocation.Authorize, that
// refuses any request needing more than level.
func RequireAccess(level Access) func(*http.Request) error {
	return func(req *http.Request) error {
		if need := RequiredAccess(req.Method, req.URL.Path); need > level {
			return fmt.Errorf("access denied: %s %s needs %s access, this invocation is %s", req.Method, req.URL.Path, need, level)
		}
		return nil
	}
}

// RequiredAccess classifies a request by method and URL path. Unknown
// methods need full access.
func RequiredAccess(method, path string) Access {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, "":
		return AccessRead
	case http.MethodPost:
		if readShapedPOST(path) {
			return AccessRead
		}
		return AccessWrite
	case http.MethodPut, http.MethodPatch:
		return AccessWrite
	default:
		return AccessDelete
	}
}

// readShapedPOST reports whether path is an endpoint that uses POST only to
// carry a query or a dry-run body. Matches are exact suffixes or RPC method
// names, so a write endpoint never matches by accident.
func readShapedPOST(path string) bool {
	// A matched suffix must not be able to reach another endpoint once the
	// server normalizes the path.
	if strings.Contains(path, "..") {
		return false
	}

	switch {
	// Unified datasource query API and its legacy equivalent.
	case strings.Contains(path, "/apis/query.grafana.app/") && strings.HasSuffix(path, "/query"),
		strings.HasSuffix(path, "/api/ds/query"):
		return true

	// Alerting notification history.
	case strings.Contains(path, "/apis/historian.alerting.grafana.app/") &&
		(strings.HasSuffix(path, "/notification/query") || strings.HasSuffix(path, "/notifications/queryalerts")):
		return true

	// Tempo trace diff through the datasource proxy.
	case strings.Contains(path, "/api/datasources/proxy/") && strings.HasSuffix(path, "/api/v2/traces/diff"):
		return true

	// Athena schema browsing through datasource resources.
	case strings.Contains(path, "/api/datasources/uid/") && hasAnySuffix(path,
		"/resources/catalogs", "/resources/databases", "/resources/tables", "/resources/columns"):
		return true

	// Agent Observability conversation search.
	case strings.HasSuffix(path, "/api/plugins/grafana-agento11y-app/resources/query/conversations/search"):
		return true

	// Knowledge Graph (asserts) searches, summaries and validate-only calls.
	case strings.Contains(path, "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v1/") && hasAnySuffix(path,
		"/entity_type/count", "/assertions/entity-metric", "/assertions/llm-summary", "/assertion/source-metrics",
		"/search", "/search/assertions", "/search/sample", "/search/cypher", "/alert-inspection",
		"/config/disabled-alerts-validate", "/config/model-rules-validate", "/config/prom-rules-validate-sync"):
		return true

	// Adaptive Metrics rule check (validation only).
	case strings.HasSuffix(path, "/aggregations/check-rules"):
		return true
	}

	// Connect/twirp-style RPC APIs send every call as POST; allow only the
	// read methods.
	service, method := rpcMethod(path)
	return slices.Contains(readRPCMethods(service), method)
}

// readRPCMethods lists the read-only methods of an RPC service gcx calls,
// by fully qualified service name.
func readRPCMethods(service string) []string {
	switch service {
	// Pyroscope, via the datasource proxy.
	case "querier.v1.QuerierService":
		return []string{
			"SelectMergeStacktraces", "SelectMergeSpanProfile", "SelectMergeProfile", "SelectSeries",
			"SelectHeatmap", "GetProfileStats", "ProfileTypes", "LabelNames", "LabelValues", "Series",
		}
	// Fleet Management and Instrumentation Hub, via the collector app proxy.
	case "pipeline.v1.PipelineService":
		return []string{"ListPipelines", "GetPipeline"}
	case "collector.v1.CollectorService":
		return []string{"ListCollectors", "GetCollector"}
	case "tenant.v1.TenantService":
		return []string{"GetLimits"}
	case "instrumentation.v1.InstrumentationService":
		return []string{"GetAppInstrumentation", "GetK8SInstrumentation"}
	case "discovery.v1.DiscoveryService":
		return []string{"RunK8sDiscovery", "RunK8sMonitoring"}
	// IRM incidents, via the IRM app's resources.
	case "IncidentsService":
		return []string{"GetIncident", "QueryIncidentPreviews"}
	case "ActivityService":
		return []string{"QueryActivity"}
	case "SeveritiesService":
		return []string{"GetOrgSeverities"}
	case "IncidentContextService":
		return []string{"QueryIncidentContext"}
	case "IntegrationService":
		return []string{"GetHookRuns"}
	}
	return nil
}

// rpcMethod splits an RPC path's last segment(s) into service and method,
// accepting both ".../pkg.Service/Method" (Connect) and ".../Service.Method"
// (IRM) forms.
func rpcMethod(path string) (string, string) {
	dir, last := pathpkg.Split(path)
	if svc, m, ok := strings.Cut(last, "."); ok && !strings.Contains(m, ".") && readRPCMethods(svc) != nil {
		// IRM form: Service.Method as the last segment.
		return svc, m
	}
	return pathpkg.Base(strings.TrimSuffix(dir, "/")), last
}

func hasAnySuffix(s string, suffixes ...string) bool {
	for _, suffix := range suffixes {
		if strings.HasSuffix(s, suffix) {
			return true
		}
	}
	return false
}
