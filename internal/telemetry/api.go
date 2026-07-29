package telemetry

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"sync/atomic"
)

// This file reduces `gcx api` requests to their anonymous usage shape. The
// api command is a raw passthrough, so its usage signal lives in values (the
// PATH argument, the request body) that the privacy invariant forbids
// sending. Every function here therefore maps those values onto closed
// vocabularies baked into the binary: HTTP methods onto the fixed verb list,
// paths onto known route templates, datasource types onto Grafana-authored
// plugin IDs. Anything that does not match is recorded as "other". Raw paths
// and bodies are never stored and never leave the process.

// otherValue is recorded whenever a value falls outside its closed
// vocabulary. A growing share of "other" is the signal to extend a table,
// never to loosen the filtering.
const otherValue = "other"

// APIRequest is the sanitized usage detail for one `gcx api` invocation.
// Every field holds a closed-vocabulary value produced by RecordAPIRequest.
type APIRequest struct {
	Method          string
	Route           string
	DatasourceTypes string
}

//nolint:gochecknoglobals // written once per process from the api command's RunE.
var apiRequest atomic.Pointer[APIRequest]

// CurrentAPIRequest returns the sanitized api-command detail recorded for
// this invocation, or nil when the api command did not record any.
func CurrentAPIRequest() *APIRequest {
	return apiRequest.Load()
}

// RecordAPIRequest reduces one `gcx api` request to its usage shape and
// records it for the usage event:
//
//   - method is kept only when it is one of the fixed HTTP verbs.
//   - rawPath is matched against the route template table. The recorded
//     route is the matching template with placeholder segments, or "other".
//     The raw path is never recorded.
//   - body is inspected only when the route is a datasource query route, and
//     only to extract queries[].datasource.type. Each type is filtered
//     through the datasource allowlist. Nothing else in the body is used.
func RecordAPIRequest(method, rawPath string, body []byte) {
	r := &APIRequest{
		Method: knownHTTPMethod(method),
		Route:  normalizeAPIRoute(rawPath),
	}
	if isDatasourceQueryRoute(r.Route) {
		r.DatasourceTypes = datasourceTypes(body)
	}
	apiRequest.Store(r)
}

// knownHTTPMethod returns method if it is a valid HTTP verb, else "". The
// api command validates this before running; the recheck keeps this file
// self-contained as the last line of defense before the event.
func knownHTTPMethod(method string) string {
	switch method {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS", "TRACE":
		return method
	}
	return ""
}

// apiRouteTemplates is the closed vocabulary of classic-API routes the usage
// event can name, first match wins. "{x}" matches exactly one segment and is
// emitted verbatim in place of the real value; "{rest}" is only valid as the
// last element and matches one or more remaining segments. The table is
// curated, not exhaustive: unmatched paths are recorded as "other", and the
// share of "other" tells us when the table is worth growing.
//
//nolint:gochecknoglobals
var apiRouteTemplates = []string{
	"/api/health",
	"/api/frontend/settings",
	"/api/search",
	"/api/ds/query",
	"/api/dashboards/db",
	"/api/dashboards/home",
	"/api/dashboards/import",
	"/api/dashboards/tags",
	"/api/dashboards/uid/{uid}",
	"/api/dashboards/uid/{uid}/versions",
	"/api/dashboards/uid/{uid}/versions/{version}",
	"/api/dashboards/uid/{uid}/permissions",
	"/api/folders",
	"/api/folders/{uid}",
	"/api/folders/{uid}/permissions",
	"/api/datasources",
	"/api/datasources/uid/{uid}",
	"/api/datasources/uid/{uid}/health",
	"/api/datasources/uid/{uid}/resources/{rest}",
	"/api/datasources/proxy/uid/{uid}/{rest}",
	"/api/datasources/name/{name}",
	"/api/datasources/{id}",
	"/api/annotations",
	"/api/annotations/tags",
	"/api/annotations/{id}",
	"/api/alertmanager/{rest}",
	"/api/ruler/{rest}",
	"/api/prometheus/{rest}",
	"/api/v1/provisioning/{rest}",
	"/api/plugins",
	"/api/plugins/{id}/settings",
	"/api/plugins/{id}/health",
	"/api/org",
	"/api/org/users",
	"/api/org/preferences",
	"/api/user",
	"/api/user/preferences",
	"/api/users/{rest}",
	"/api/orgs/{rest}",
	"/api/teams/search",
	"/api/teams/{rest}",
	"/api/serviceaccounts/search",
	"/api/serviceaccounts/{rest}",
	"/api/access-control/{rest}",
	"/api/library-elements/{rest}",
	"/api/playlists/{rest}",
	"/api/snapshots/{rest}",
	"/api/short-urls",
	"/api/admin/{rest}",
}

// normalizeAPIRoute maps a raw request path onto the route vocabulary:
// a template from apiRouteTemplates, a templated /apis route, or "other".
// The query string and fragment are discarded before matching so their
// values can never influence, or appear in, the result.
func normalizeAPIRoute(rawPath string) string {
	path := rawPath
	if i := strings.IndexAny(path, "?#"); i >= 0 {
		path = path[:i]
	}
	segs := splitPath(path)
	if len(segs) == 0 {
		return otherValue
	}
	switch segs[0] {
	case "api":
		for _, tmpl := range apiRouteTemplates {
			if matchRouteTemplate(splitPath(tmpl), segs) {
				return tmpl
			}
		}
	case "apis":
		return k8sAPIRoute(segs)
	}
	return otherValue
}

// splitPath splits a URL path into its non-empty segments.
func splitPath(path string) []string {
	var segs []string
	for s := range strings.SplitSeq(path, "/") {
		if s != "" {
			segs = append(segs, s)
		}
	}
	return segs
}

// matchRouteTemplate reports whether the path segments match the template
// segments. Template literals must match exactly, "{rest}" (last element
// only) matches one or more remaining segments, and any other "{x}"
// placeholder matches exactly one segment of any value.
func matchRouteTemplate(tmpl, path []string) bool {
	for i, t := range tmpl {
		if t == "{rest}" && i == len(tmpl)-1 {
			return len(path) > i
		}
		if i >= len(path) {
			return false
		}
		if strings.HasPrefix(t, "{") && strings.HasSuffix(t, "}") {
			continue
		}
		if t != path[i] {
			return false
		}
	}
	return len(tmpl) == len(path)
}

// knownAPIGroups is the closed vocabulary of Grafana app-platform API groups
// the usage event can name. Groups are recorded only from this list (plus
// the datasource-group rule in knownAPIGroup) because app plugins register
// their own groups, whose names could identify a customer's private plugin.
//
//nolint:gochecknoglobals
var knownAPIGroups = map[string]bool{
	"dashboard.grafana.app":              true,
	"folder.grafana.app":                 true,
	"query.grafana.app":                  true,
	"provisioning.grafana.app":           true,
	"iam.grafana.app":                    true,
	"playlist.grafana.app":               true,
	"preferences.grafana.app":            true,
	"shorturl.grafana.app":               true,
	"secret.grafana.app":                 true,
	"rules.alerting.grafana.app":         true,
	"notifications.alerting.grafana.app": true,
}

// knownAPIGroup reports whether group may be recorded verbatim: a core group
// from knownAPIGroups, or a datasource API group whose plugin label passes
// the same allowlist as datasource types.
func knownAPIGroup(group string) bool {
	if knownAPIGroups[group] {
		return true
	}
	if label, ok := strings.CutSuffix(group, ".datasource.grafana.app"); ok {
		return allowedDatasourceType(label) != otherValue
	}
	return false
}

// k8sVersionShape matches Kubernetes-style API versions (v1, v1beta1,
// v0alpha1). Anything else in the version position is not API shape.
var k8sVersionShape = regexp.MustCompile(`^v[0-9]+((alpha|beta)[0-9]+)?$`)

// k8sResourceShape matches Kubernetes-style resource names (lowercase
// letters and hyphens). By construction of /apis routes the segment in this
// position is a resource type defined by server code, not user data; the
// shape check guards against malformed paths smuggling a value through.
var k8sResourceShape = regexp.MustCompile(`^[a-z][a-z-]*$`)

// k8sAPIRoute templates an app-platform path:
// /apis/{group}/{version}[/namespaces/{ns}]/{resource}[/{name}/...].
// The group must pass knownAPIGroup, the namespace is always replaced with
// "{namespace}", and everything after the resource collapses into "{name}"
// (k8s subresource detail is deliberately dropped rather than filtered).
// Any deviation from that shape is recorded as "other".
func k8sAPIRoute(segs []string) string {
	if len(segs) < 4 {
		return otherValue
	}
	group, version := segs[1], segs[2]
	if !knownAPIGroup(group) || !k8sVersionShape.MatchString(version) {
		return otherValue
	}
	out := []string{"", "apis", group, version}
	rest := segs[3:]
	if rest[0] == "namespaces" {
		if len(rest) < 3 {
			return otherValue
		}
		out = append(out, "namespaces", "{namespace}")
		rest = rest[2:]
	}
	if !k8sResourceShape.MatchString(rest[0]) {
		return otherValue
	}
	out = append(out, rest[0])
	if len(rest) > 1 {
		out = append(out, "{name}")
	}
	return strings.Join(out, "/")
}

// isDatasourceQueryRoute reports whether the already-normalized route is a
// datasource query endpoint, the only routes whose body datasourceTypes
// inspects. It takes the template, never the raw path.
func isDatasourceQueryRoute(route string) bool {
	if route == "/api/ds/query" {
		return true
	}
	return strings.HasPrefix(route, "/apis/query.grafana.app/") && strings.HasSuffix(route, "/query")
}

// grafanaPluginIDShape matches plugin IDs in the grafana- publisher
// namespace. The prefix is reserved for Grafana-authored plugins (enforced
// by plugin signature verification), so IDs of this shape name public
// catalog plugins, never a customer's private plugin.
var grafanaPluginIDShape = regexp.MustCompile(`^grafana-[a-z0-9-]+$`)

// coreDatasourceTypes lists the core datasource plugin IDs that predate the
// grafana- publisher prefix. Like the prefix rule, membership means
// "Grafana-authored, from the fixed public list".
//
//nolint:gochecknoglobals
var coreDatasourceTypes = map[string]bool{
	"prometheus": true, "loki": true, "tempo": true, "jaeger": true,
	"zipkin": true, "elasticsearch": true, "graphite": true, "influxdb": true,
	"opentsdb": true, "mysql": true, "mssql": true, "postgres": true,
	"cloudwatch": true, "stackdriver": true, "alertmanager": true,
	"grafana": true, "mixed": true, "dashboard": true, "testdata": true,
}

// allowedDatasourceType maps a datasource plugin type onto the closed
// vocabulary: the type itself when it names a Grafana-authored plugin, else
// "other". Third-party and private plugin IDs are never recorded, even
// though many third-party IDs are public catalog data, because ID shape
// cannot distinguish a public community plugin from a customer's private one.
func allowedDatasourceType(t string) string {
	if coreDatasourceTypes[t] {
		return t
	}
	if len(t) <= 64 && grafanaPluginIDShape.MatchString(t) {
		return t
	}
	return otherValue
}

// maxQueryBodyBytes caps the body size datasourceTypes will parse. Query
// request bodies are small; anything larger is skipped rather than parsed.
const maxQueryBodyBytes = 1 << 20

// maxDatasourceTypes caps how many distinct types one event can carry.
const maxDatasourceTypes = 8

// datasourceTypes extracts the datasource plugin types from a
// /api/ds/query-style request body: the sorted, comma-joined set of
// queries[].datasource.type values, each filtered through
// allowedDatasourceType. Only that one field is used. The datasource uid and
// name, the query text, the time range, and everything else in the body are
// never extracted. Legacy references ("datasource" as a plain string) are a
// datasource name or uid, which is user data, so they are recorded as
// "other". Returns "" when the body is not a well-formed query request.
func datasourceTypes(body []byte) string {
	if len(body) == 0 || len(body) > maxQueryBodyBytes {
		return ""
	}
	var payload struct {
		Queries []struct {
			Datasource json.RawMessage `json:"datasource"`
		} `json:"queries"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || len(payload.Queries) == 0 {
		return ""
	}

	set := make(map[string]bool)
	for _, q := range payload.Queries {
		t := otherValue
		var ref struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(q.Datasource, &ref); err == nil && ref.Type != "" {
			t = allowedDatasourceType(ref.Type)
		}
		set[t] = true
	}

	types := make([]string, 0, len(set))
	for t := range set {
		types = append(types, t)
	}
	sort.Strings(types)
	if len(types) > maxDatasourceTypes {
		types = types[:maxDatasourceTypes]
	}
	return strings.Join(types, ",")
}
