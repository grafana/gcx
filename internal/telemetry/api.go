package telemetry

import (
	"encoding/json"
	"slices"
	"sort"
	"strings"
	"sync/atomic"
)

// This file reduces `gcx api` requests to their anonymous usage shape. The
// api command is a raw passthrough, so its usage signal lives in values (the
// PATH argument, the request body) that the privacy invariant forbids
// sending. Every function here therefore maps those values onto closed
// vocabularies baked into the binary: HTTP methods onto the fixed verb list,
// paths onto known route templates, datasource types onto Grafana-published
// plugin IDs. Anything that does not match is recorded as "other". Raw paths
// and bodies are never stored and never leave the process.
//
// The vocabularies themselves (route templates, API groups, k8s versions and
// resources, datasource plugin IDs) live in api_vocabulary.go.

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
//     A query-route body that yields no types (missing, oversized, or not a
//     well-formed query request) is recorded as "other".
func RecordAPIRequest(method, rawPath string, body []byte) {
	r := &APIRequest{
		Method: knownHTTPMethod(method),
		Route:  normalizeAPIRoute(rawPath),
	}
	if isDatasourceQueryRoute(r.Route) {
		// "other" rather than empty when extraction fails, so an empty field
		// always means "not a query route" and extraction health stays
		// measurable in the wild.
		if r.DatasourceTypes = datasourceTypes(body); r.DatasourceTypes == "" {
			r.DatasourceTypes = otherValue
		}
	}
	apiRequest.Store(r)
}

// IsKnownHTTPMethod reports whether method is on the fixed HTTP verb list.
func IsKnownHTTPMethod(method string) bool {
	return slices.Contains(httpMethods, method)
}

// HTTPMethods returns a copy of the fixed HTTP verb list, for building
// user-facing messages. A copy so the vocabulary itself stays immutable.
func HTTPMethods() []string {
	return slices.Clone(httpMethods)
}

// knownHTTPMethod returns method if it is on the shared verb list, else "".
// The api command validates against the same list before running; the
// recheck keeps this package the last line of defense before the event.
func knownHTTPMethod(method string) string {
	if IsKnownHTTPMethod(method) {
		return method
	}
	return ""
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
	if !knownAPIGroup(group) || !knownK8sVersions[version] {
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
	if !knownK8sResources[rest[0]] {
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

// allowedDatasourceType maps a datasource plugin type onto the closed
// vocabulary: the type itself when it is on one of the two fixed
// Grafana-published lists, else "other". Everything not listed is redacted,
// including public third-party plugins, because no membership test built
// from the value itself can distinguish a public community plugin from a
// customer's private one.
func allowedDatasourceType(t string) string {
	if coreDatasourceTypes[t] || grafanaDatasourceTypes[t] {
		return t
	}
	return otherValue
}

// maxQueryBodyBytes caps the body size datasourceTypes will parse. Query
// request bodies are small; anything larger is skipped rather than parsed.
const maxQueryBodyBytes = 1 << 20

// maxDatasourceTypes caps how many distinct types one event can carry. Real
// query requests name one type, a mixed-datasource panel a handful, so the
// cap exists only to bound the field size against a pathological body. 8 is
// an arbitrary value comfortably above anything organic.
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
