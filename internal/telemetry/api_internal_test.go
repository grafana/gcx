package telemetry

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeAPIRoute(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "exact route", path: "/api/health", want: "/api/health"},
		{name: "ds query", path: "/api/ds/query", want: "/api/ds/query"},
		{name: "uid replaced", path: "/api/dashboards/uid/abC-123xy", want: "/api/dashboards/uid/{uid}"},
		{name: "trailing slash", path: "/api/folders/", want: "/api/folders"},
		{name: "no leading slash", path: "api/health", want: "/api/health"},
		{name: "query string stripped", path: "/api/search?query=my+secret+dashboard&limit=10", want: "/api/search"},
		{name: "fragment stripped", path: "/api/health#frag", want: "/api/health"},
		{name: "value never surfaces", path: "/api/dashboards/uid/user@example.com", want: "/api/dashboards/uid/{uid}"},
		{name: "datasource name replaced", path: "/api/datasources/name/Customer%20Postgres", want: "/api/datasources/name/{name}"},
		{name: "datasource proxy tail collapsed", path: "/api/datasources/proxy/uid/abc123/api/v1/query_range", want: "/api/datasources/proxy/uid/{uid}/{rest}"},
		{name: "specific wins over rest", path: "/api/teams/search", want: "/api/teams/search"},
		{name: "rest catches deeper paths", path: "/api/teams/42/members", want: "/api/teams/{rest}"},
		{name: "unknown api route", path: "/api/some-internal-endpoint/abc", want: "other"},
		{name: "extra segments do not match", path: "/api/health/deep/er", want: "other"},
		{name: "non-api path", path: "/logout", want: "other"},
		{name: "empty path", path: "", want: "other"},
		{name: "root path", path: "/", want: "other"},

		{name: "k8s namespaced with name", path: "/apis/dashboard.grafana.app/v1beta1/namespaces/stacks-123456/dashboards/my-dash",
			want: "/apis/dashboard.grafana.app/v1beta1/namespaces/{namespace}/dashboards/{name}"},
		{name: "k8s namespaced list", path: "/apis/folder.grafana.app/v1/namespaces/default/folders",
			want: "/apis/folder.grafana.app/v1/namespaces/{namespace}/folders"},
		{name: "k8s subresource collapses into name", path: "/apis/dashboard.grafana.app/v1/namespaces/default/dashboards/my-dash/dto",
			want: "/apis/dashboard.grafana.app/v1/namespaces/{namespace}/dashboards/{name}"},
		{name: "k8s query connect route", path: "/apis/query.grafana.app/v0alpha1/namespaces/stacks-123/query",
			want: "/apis/query.grafana.app/v0alpha1/namespaces/{namespace}/query"},
		{name: "k8s datasource group passes ds allowlist", path: "/apis/prometheus.datasource.grafana.app/v0alpha1/namespaces/default/connections",
			want: "/apis/prometheus.datasource.grafana.app/v0alpha1/namespaces/{namespace}/connections"},
		{name: "k8s private datasource group redacted", path: "/apis/acmecorp-internal-datasource.datasource.grafana.app/v0alpha1/namespaces/default/connections",
			want: "other"},
		{name: "k8s unknown group redacted", path: "/apis/acmecorp-billing-app.grafana.app/v1/namespaces/default/things", want: "other"},
		{name: "k8s bad version shape", path: "/apis/dashboard.grafana.app/latest/namespaces/default/dashboards", want: "other"},
		{name: "k8s resource with digits rejected", path: "/apis/dashboard.grafana.app/v1/namespaces/default/abc123", want: "other"},
		{name: "k8s too short", path: "/apis/dashboard.grafana.app/v1", want: "other"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, normalizeAPIRoute(tt.path))
		})
	}
}

// No template output may echo path input: everything variable must have been
// replaced by a placeholder or collapsed to "other". This sweeps the whole
// table against a path built from recognizably fake values.
func TestNormalizeAPIRouteNeverEchoesValues(t *testing.T) {
	const marker = "zz9secret9zz"
	for _, tmpl := range apiRouteTemplates {
		path := strings.NewReplacer(
			"{uid}", marker, "{id}", marker, "{name}", marker,
			"{version}", marker, "{rest}", marker+"/"+marker,
		).Replace(tmpl)
		got := normalizeAPIRoute(path)
		assert.NotContains(t, got, marker, "template %q leaked a path value", tmpl)
	}
}

func TestKnownHTTPMethod(t *testing.T) {
	assert.Equal(t, "DELETE", knownHTTPMethod("DELETE"))
	assert.Empty(t, knownHTTPMethod("YOLO"))
	assert.Empty(t, knownHTTPMethod(""))
}

func TestAllowedDatasourceType(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "prometheus", want: "prometheus"},
		{in: "postgres", want: "postgres"},
		{in: "grafana-postgresql-datasource", want: "grafana-postgresql-datasource"},
		{in: "grafana-clickhouse-datasource", want: "grafana-clickhouse-datasource"},
		// Public community plugin: still redacted, because ID shape cannot
		// distinguish it from a private plugin.
		{in: "marcusolsson-json-datasource", want: "other"},
		{in: "acmecorp-secret-datasource", want: "other"},
		{in: "grafana-UPPER-datasource", want: "other"},
		{in: "grafana-" + strings.Repeat("x", 100), want: "other"},
		{in: "", want: "other"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			assert.Equal(t, tt.want, allowedDatasourceType(tt.in))
		})
	}
}

func TestDatasourceTypes(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "single query",
			body: `{"queries":[{"refId":"A","datasource":{"type":"prometheus","uid":"abc"},"expr":"up"}]}`,
			want: "prometheus",
		},
		{
			name: "mixed types deduped and sorted",
			body: `{"queries":[
				{"datasource":{"type":"loki","uid":"l1"}},
				{"datasource":{"type":"prometheus","uid":"p1"}},
				{"datasource":{"type":"prometheus","uid":"p2"}}]}`,
			want: "loki,prometheus",
		},
		{
			name: "private plugin becomes other",
			body: `{"queries":[{"datasource":{"type":"acmecorp-internal-datasource","uid":"x"}}]}`,
			want: "other",
		},
		{
			name: "legacy string reference is a name, becomes other",
			body: `{"queries":[{"datasource":"My Customer Postgres"}]}`,
			want: "other",
		},
		{
			name: "missing datasource becomes other",
			body: `{"queries":[{"refId":"A"}]}`,
			want: "other",
		},
		{name: "not json", body: `SELECT * FROM secrets`, want: ""},
		{name: "no queries", body: `{"from":"now-1h","to":"now"}`, want: ""},
		{name: "empty body", body: ``, want: ""},
		{name: "oversized body skipped", body: `{"queries":[` + strings.Repeat(" ", maxQueryBodyBytes) + `]}`, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, datasourceTypes([]byte(tt.body)))
		})
	}
}

// End to end over the exact shape agents send to /api/ds/query: only the
// plugin type may come out, never the uid, the SQL, or the time range.
func TestRecordAPIRequest(t *testing.T) {
	t.Cleanup(func() { apiRequest.Store(nil) })

	body := `{
	  "from":"now-1h","to":"now",
	  "queries":[{"refId":"A","datasource":{"type":"grafana-postgresql-datasource","uid":"postgres-customers"},
	              "rawSql":"SELECT email, full_name, address FROM customers","format":"table"}]}`
	RecordAPIRequest("POST", "/api/ds/query", []byte(body))

	got := CurrentAPIRequest()
	assert.Equal(t, &APIRequest{
		Method:          "POST",
		Route:           "/api/ds/query",
		DatasourceTypes: "grafana-postgresql-datasource",
	}, got)
}

func TestRecordAPIRequestIgnoresBodyOffQueryRoutes(t *testing.T) {
	t.Cleanup(func() { apiRequest.Store(nil) })

	// Same body posted to a non-query route: the body must not be inspected.
	body := `{"queries":[{"datasource":{"type":"prometheus","uid":"secret"}}]}`
	RecordAPIRequest("POST", "/api/folders", []byte(body))

	got := CurrentAPIRequest()
	assert.Equal(t, &APIRequest{Method: "POST", Route: "/api/folders"}, got)
}
