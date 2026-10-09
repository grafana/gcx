package telemetry

// This file holds every closed vocabulary the api-command usage detail can
// draw from. These tables ARE the privacy boundary: RecordAPIRequest (api.go)
// only ever emits values listed here, placeholders, or "other". Growing a
// table is safe and routine (a growing share of "other" is the signal to do
// it); anything that turns a table into a pattern or shape rule is not, see
// the privacy invariant on Event.

// httpMethods is the closed vocabulary of HTTP verbs. Shared by the api
// command's flag validation (IsKnownHTTPMethod, HTTPMethods) and
// knownHTTPMethod so the two can never drift: a verb the command accepts is
// always one the usage event can record. Unexported, like every table here,
// so no caller can grow a vocabulary at runtime.
//
//nolint:gochecknoglobals
var httpMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS", "TRACE"}

// apiRouteTemplates is the closed vocabulary of classic-API routes the usage
// event can name, first match wins. "{x}" matches exactly one segment and is
// emitted verbatim in place of the real value; "{rest}" is only valid as the
// last element and matches one or more remaining segments, so collection
// roots need their own bare entry ahead of the "{rest}" line. The table is
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
	"/api/datasources/proxy/{id}/{rest}",
	"/api/datasources/name/{name}",
	"/api/datasources/{id}",
	"/api/annotations",
	"/api/annotations/graphite",
	"/api/annotations/tags",
	"/api/annotations/{id}",
	"/api/alertmanager/{rest}",
	"/api/ruler/{rest}",
	"/api/prometheus/{rest}",
	"/api/v1/provisioning/alert-rules",
	"/api/v1/provisioning/alert-rules/{rest}",
	"/api/v1/provisioning/contact-points",
	"/api/v1/provisioning/contact-points/{rest}",
	"/api/v1/provisioning/policies",
	"/api/v1/provisioning/policies/{rest}",
	"/api/v1/provisioning/mute-timings",
	"/api/v1/provisioning/mute-timings/{rest}",
	"/api/v1/provisioning/templates",
	"/api/v1/provisioning/templates/{rest}",
	"/api/v1/provisioning/{rest}",
	"/api/plugins",
	"/api/plugins/{id}/settings",
	"/api/plugins/{id}/health",
	"/api/org",
	"/api/org/users",
	"/api/org/preferences",
	"/api/user",
	"/api/user/preferences",
	"/api/users",
	"/api/users/{rest}",
	"/api/orgs",
	"/api/orgs/{rest}",
	"/api/teams",
	"/api/teams/search",
	"/api/teams/{rest}",
	"/api/serviceaccounts",
	"/api/serviceaccounts/search",
	"/api/serviceaccounts/{rest}",
	"/api/access-control/{rest}",
	"/api/library-elements",
	"/api/library-elements/{rest}",
	"/api/playlists",
	"/api/playlists/{rest}",
	"/api/snapshots",
	"/api/snapshots/{rest}",
	"/api/short-urls",
	"/api/admin/{rest}",
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
	"iam.grafana.app":                    true,
	"notifications.alerting.grafana.app": true,
	"playlist.grafana.app":               true,
	"preferences.grafana.app":            true,
	"provisioning.grafana.app":           true,
	"query.grafana.app":                  true,
	"rules.alerting.grafana.app":         true,
	"secret.grafana.app":                 true,
	"shorturl.grafana.app":               true,
}

// knownK8sVersions is the closed vocabulary of Kubernetes-style API versions
// the usage event can name. A finite list rather than a shape check: the
// version position is user-typed, so an unlisted value is recorded as
// "other" like any other unknown, never verbatim.
//
//nolint:gochecknoglobals
var knownK8sVersions = map[string]bool{
	"v0alpha1": true,
	"v0beta1":  true,
	"v1":       true,
	"v1alpha1": true,
	"v1beta1":  true,
	"v2":       true,
	"v2alpha1": true,
	"v2beta1":  true,
}

// knownK8sResources is the closed vocabulary of resource names the usage
// event can name in the /apis resource position, across the allowlisted
// groups. A finite list rather than a shape check: the segment is
// user-typed, so an unlisted value drops the whole route to "other", never
// through verbatim. Curated like the route table; grow it when the share of
// "other" says it is worth it.
//
//nolint:gochecknoglobals
var knownK8sResources = map[string]bool{
	"alertrules":      true,
	"connections":     true,
	"dashboards":      true,
	"datasources":     true,
	"folders":         true,
	"jobs":            true,
	"keepers":         true,
	"playlists":       true,
	"preferences":     true,
	"query":           true,
	"queryconvert":    true,
	"receivers":       true,
	"recordingrules":  true,
	"repositories":    true,
	"routingtrees":    true,
	"search":          true,
	"securevalues":    true,
	"serviceaccounts": true,
	"shorturls":       true,
	"ssosettings":     true,
	"stars":           true,
	"teams":           true,
	"templategroups":  true,
	"timeintervals":   true,
	"users":           true,
}

// coreDatasourceTypes lists the core datasource plugin IDs that predate the
// grafana- publisher prefix. Grafana-published, from the fixed public list.
//
//nolint:gochecknoglobals
var coreDatasourceTypes = map[string]bool{
	"alertmanager":  true,
	"cloudwatch":    true,
	"dashboard":     true,
	"elasticsearch": true,
	"grafana":       true,
	"graphite":      true,
	"influxdb":      true,
	"jaeger":        true,
	"loki":          true,
	"mixed":         true,
	"mssql":         true,
	"mysql":         true,
	"opentsdb":      true,
	"postgres":      true,
	"prometheus":    true,
	"stackdriver":   true,
	"tempo":         true,
	"testdata":      true,
	"zipkin":        true,
}

// grafanaDatasourceTypes lists the Grafana-published datasource plugin IDs
// from the public catalog. Generated 2026-09-02 from
// https://grafana.com/api/plugins?typeCode=datasource&orgSlug=grafana, minus
// the entries already in coreDatasourceTypes.
// A finite list rather than a grafana- prefix rule so no user-typed value
// can ever pass: a plugin missing here is undercounted as "other" until the
// list is regenerated, which is the safe direction to fail.
//
// Not every ID here carries the grafana- prefix. The five community plugins
// Grafana has taken over keep their original publisher prefix while being
// published by Grafana, which is exactly why the prefix cannot be the rule.
//
//nolint:gochecknoglobals
var grafanaDatasourceTypes = map[string]bool{
	"dlopes7-appdynamics-datasource":         true,
	"grafana-adobeanalytics-datasource":      true,
	"grafana-amazonprometheus-datasource":    true,
	"grafana-astradb-datasource":             true,
	"grafana-athena-datasource":              true,
	"grafana-atlassianstatuspage-datasource": true,
	"grafana-aurora-datasource":              true,
	"grafana-azure-data-explorer-datasource": true,
	"grafana-azure-monitor-datasource":       true,
	"grafana-azurecosmosdb-datasource":       true,
	"grafana-azuredevops-datasource":         true,
	"grafana-azureprometheus-datasource":     true,
	"grafana-bigquery-datasource":            true,
	"grafana-catchpoint-datasource":          true,
	"grafana-clickhouse-datasource":          true,
	"grafana-cloudflare-datasource":          true,
	"grafana-cockroachdb-datasource":         true,
	"grafana-cube-datasource":                true,
	"grafana-databricks-datasource":          true,
	"grafana-datadog-datasource":             true,
	"grafana-drone-datasource":               true,
	"grafana-dynamodb-datasource":            true,
	"grafana-dynatrace-datasource":           true,
	"grafana-falconlogscale-datasource":      true,
	"grafana-github-datasource":              true,
	"grafana-gitlab-datasource":              true,
	"grafana-googlesheets-datasource":        true,
	"grafana-honeycomb-datasource":           true,
	"grafana-ibmdb2-datasource":              true,
	"grafana-iot-sitewise-datasource":        true,
	"grafana-jenkins-datasource":             true,
	"grafana-jira-datasource":                true,
	"grafana-logicmonitor-datasource":        true,
	"grafana-looker-datasource":              true,
	"grafana-mock-datasource":                true,
	"grafana-mongodb-datasource":             true,
	"grafana-mqtt-datasource":                true,
	"grafana-netlify-datasource":             true,
	"grafana-newrelic-datasource":            true,
	"grafana-odbc-datasource":                true,
	"grafana-opensearch-datasource":          true,
	"grafana-oracle-datasource":              true,
	"grafana-pagerduty-datasource":           true,
	"grafana-postgresql-datasource":          true,
	"grafana-pyroscope-datasource":           true,
	"grafana-redshift-datasource":            true,
	"grafana-salesforce-datasource":          true,
	"grafana-saphana-datasource":             true,
	"grafana-sentry-datasource":              true,
	"grafana-servicenow-datasource":          true,
	"grafana-snowflake-datasource":           true,
	"grafana-solarwinds-datasource":          true,
	"grafana-splunk-datasource":              true,
	"grafana-splunk-monitoring-datasource":   true,
	"grafana-strava-datasource":              true,
	"grafana-sumologic-datasource":           true,
	"grafana-testdata-datasource":            true,
	"grafana-timestream-datasource":          true,
	"grafana-vercel-datasource":              true,
	"grafana-wavefront-datasource":           true,
	"grafana-x-ray-datasource":               true,
	"grafana-yugabyte-datasource":            true,
	"grafana-zendesk-datasource":             true,
	"marcusolsson-json-datasource":           true,
	"marcusolsson-static-datasource":         true,
	"volkovlabs-rss-datasource":              true,
	"yesoreyeram-infinity-datasource":        true,
}
