package gcx.rules.dashboard.bug["target-valid-promql_test"]

import data.gcx.utils

import data.gcx.rules.dashboard.bug["target-valid-promql"] as rule

_test_v1_panel(query) := {
	"id": 4,
	"fieldConfig": {
		"defaults": {
			"color": {"fixedColor": "green", "mode": "fixed"},
			"unit": "s",
		},
		"overrides": [],
	},
	"options": {
		"colorMode": "value",
		"wideLayout": true,
	},
	"targets": [{
		"datasource": {
			"type": "prometheus",
			"uid": "grafanacloud-prom",
		},
		"expr": query,
		"format": "time_series",
		"instant": true,
		"legendFormat": "__auto",
		"range": false,
		"refId": "A",
	}],
	"title": "Uptime",
	"type": "stat",
}

_test_v2_panel(query) := {
	"kind": "Panel",
	"spec": {
		"data": {
			"kind": "QueryGroup",
			"spec": {
				"queries": [
					{
						"kind": "PanelQuery",
						"spec": {
							"hidden": false,
							"query": {
								"datasource": {"name": "grafanacloud-prom"},
								"group": "prometheus",
								"kind": "DataQuery",
								"spec": {
									"editorMode": "code",
									"expr": query,
									"legendFormat": "{{instance}} - {{chip}}",
									"range": true,
								},
								"version": "v0",
							},
							"refId": "A",
						},
					},
				],
				"queryOptions": {},
				"transformations": [],
			},
		},
		"id": 1,
		"title": "Temperatures",
		"vizConfig": {
			"group": "timeseries",
			"kind": "VizConfig",
			"spec": {
				"fieldConfig": {
					"defaults": {
						"color": {"mode": "palette-classic"},
						"custom": {"axisBorderShow": false},
						"thresholds": {
							"mode": "absolute",
							"steps": [
								{
									"color": "green",
									"value": 0,
								},
								{
									"color": "red",
									"value": 80,
								},
							],
						},
						"unit": "celsius",
					},
					"overrides": [],
				},
				"options": {
					"legend": {
						"calcs": ["lastNotNull"],
						"displayMode": "list",
						"placement": "bottom",
						"showLegend": true,
					},
					"tooltip": {
						"hideZeros": false,
						"mode": "single",
						"sort": "none",
					},
				},
			},
		},
	},
}

test_dashboard_v1_with_no_panels if {
	resource := {
		"kind": "Dashboard",
		"apiVersion": "dashboard.grafana.app/v1beta1",
		"metadata": {"name": "test-dashboard"},
		"spec": {"panels": []},
	}

	report := rule.report with input as resource

	assert_reports_match(report, set())
}

test_dashboard_v1_with_valid_panel if {
	resource := {
		"kind": "Dashboard",
		"apiVersion": "dashboard.grafana.app/v1beta1",
		"metadata": {"name": "test-dashboard"},
		"spec": {
			"panels": [
				_test_v1_panel("time()-process_start_time_seconds{job=\"integrations/forgejo\"}"),
			],
		},
	}

	report := rule.report with input as resource

	assert_reports_match(report, set())
}

test_dashboard_v1_with_invalid_panel if {
	resource := {
		"kind": "Dashboard",
		"apiVersion": "dashboard.grafana.app/v1beta1",
		"metadata": {"name": "test-dashboard"},
		"spec": {"panels": [_test_v1_panel("invalid promql")]},
	}

	report := rule.report with input as resource

	assert_reports_match(report, {{
		"category": "bug",
		"description": "Checks that Prometheus targets defined in dashboard panels use valid PromQL queries.",
		"details": "panel 4, query 'A': invalid promql\n1:9: parse error: unexpected identifier \"promql\"",
		"related_resources": [{"description": "documentation", "ref": "https://github.com/grafana/gcx/blob/main/docs/reference/linter-rules/dashboard/target-valid-promql.md"}],
		"resource_type": "dashboard",
		"rule": "target-valid-promql",
		"severity": "error",
	}})
}

test_dashboard_v2_with_no_panels if {
	resource := {
		"kind": "Dashboard",
		"apiVersion": "dashboard.grafana.app/v2beta1",
		"metadata": {"name": "test-dashboard"},
		"spec": {"elements": {}},
	}

	report := rule.report with input as resource

	assert_reports_match(report, set())
}

test_dashboard_v2_with_valid_panel if {
	resource := {
		"kind": "Dashboard",
		"apiVersion": "dashboard.grafana.app/v2beta1",
		"metadata": {"name": "test-dashboard"},
		"spec": {
			"elements": {
				"panel-1": _test_v2_panel("avg(node_hwmon_temp_celsius{instance=~\"$instance\", chip=~\"platform.+\"}) by (instance, chip)"),
			},
		},
	}

	report := rule.report with input as resource

	assert_reports_match(report, set())
}

test_dashboard_v2_with_invalid_panel if {
	resource := {
		"kind": "Dashboard",
		"apiVersion": "dashboard.grafana.app/v2beta1",
		"metadata": {"name": "test-dashboard"},
		"spec": {"elements": {"panel-1": _test_v2_panel("invalid promql")}},
	}

	report := rule.report with input as resource

	assert_reports_match(report, {{
		"category": "bug",
		"description": "Checks that Prometheus targets defined in dashboard panels use valid PromQL queries.",
		"details": "panel 'panel-1', query 'A': invalid promql\n1:9: parse error: unexpected identifier \"promql\"",
		"related_resources": [{"description": "documentation", "ref": "https://github.com/grafana/gcx/blob/main/docs/reference/linter-rules/dashboard/target-valid-promql.md"}],
		"resource_type": "dashboard",
		"rule": "target-valid-promql",
		"severity": "error",
	}})
}

_test_v2_resource(query, datasource, group, variables) := resource if {
	panel := _test_v2_panel(query)
	data_query := object.union(panel.spec.data.spec.queries[0].spec.query, {"datasource": {"name": datasource}, "group": group})
	panel_query := object.union(panel.spec.data.spec.queries[0], {"spec": {"query": data_query, "refId": "A"}})
	resource := {
		"kind": "Dashboard",
		"apiVersion": "dashboard.grafana.app/v2beta1",
		"spec": {
			"elements": {"panel-1": object.union(panel, {"spec": object.union(panel.spec, {"data": {"spec": {"queries": [panel_query]}}})})},
			"variables": variables,
		},
	}
}

_datasource_variable_language_cases := [
	{"name": "loki variable", "datasource": "$logs", "group": "prometheus", "kind": "DatasourceVariable", "plugin": "loki", "expr": `sum(rate({job="example"} |= "warning" [5m]))`, "count": 0},
	{"name": "legacy loki variable", "datasource": "[[logs]]", "group": "prometheus", "kind": "DatasourceVariable", "plugin": "loki", "expr": `sum(rate({job="example"} |= "warning" [5m]))`, "count": 0},
	{"name": "braced loki variable", "datasource": "${logs}", "group": "prometheus", "kind": "DatasourceVariable", "plugin": "loki", "expr": `sum(rate({job="example"} |= "warning" [5m]))`, "count": 0},
	{"name": "prometheus variable", "datasource": "$logs", "group": "loki", "kind": "DatasourceVariable", "plugin": "prometheus", "expr": "invalid promql", "count": 1},
	{"name": "valid prometheus variable", "datasource": "$logs", "group": "loki", "kind": "DatasourceVariable", "plugin": "prometheus", "expr": "up", "count": 0},
	{"name": "direct datasource", "datasource": "example-prometheus", "group": "prometheus", "kind": "DatasourceVariable", "plugin": "loki", "expr": "invalid promql", "count": 1},
	{"name": "unknown variable", "datasource": "$other", "group": "prometheus", "kind": "DatasourceVariable", "plugin": "loki", "expr": "invalid promql", "count": 1},
	{"name": "other variable kind", "datasource": "$logs", "group": "prometheus", "kind": "QueryVariable", "plugin": "loki", "expr": "invalid promql", "count": 1},
	{"name": "empty plugin", "datasource": "$logs", "group": "prometheus", "kind": "DatasourceVariable", "plugin": "", "expr": "invalid promql", "count": 1},
]

_test_v1_resource(query, datasource, saved_type, variables) := resource if {
	panel := _test_v1_panel(query)
	target := object.union(panel.targets[0], {"datasource": {"type": saved_type, "uid": datasource}})
	resource := {
		"kind": "Dashboard",
		"apiVersion": "dashboard.grafana.app/v1beta1",
		"spec": {
			"panels": [object.union(panel, {"targets": [target]})],
			"templating": {"list": variables},
		},
	}
}

test_dashboard_v1_datasource_variable_language[tc.name] if {
	some tc in _datasource_variable_language_cases
	variable_type := {"DatasourceVariable": "datasource", "QueryVariable": "query"}[tc.kind]
	resource := _test_v1_resource(tc.expr, tc.datasource, tc.group, [{"type": variable_type, "name": "logs", "query": tc.plugin}])

	report := rule.report with input as resource
	count(report) == tc.count
}

test_dashboard_v2_datasource_variable_language[tc.name] if {
	some tc in _datasource_variable_language_cases
	resource := _test_v2_resource(tc.expr, tc.datasource, tc.group, [{"kind": tc.kind, "spec": {"name": "logs", "pluginId": tc.plugin}}])
	report := rule.report with input as resource
	count(report) == tc.count
}

test_dashboard_v1_missing_panel_id_is_still_linted if {
	resource := {
		"kind": "Dashboard",
		"apiVersion": "dashboard.grafana.app/v1beta1",
		"spec": {"panels": [object.remove(_test_v1_panel("invalid promql"), ["id"])]},
	}
	report := rule.report with input as resource
	count(report) == 1
	some violation in report
	startswith(violation.details, "panel index 0, query 'A':")
}

test_dashboard_v2_identical_invalid_queries_keep_both_panels if {
	resource := {
		"kind": "Dashboard",
		"apiVersion": "dashboard.grafana.app/v2beta1",
		"spec": {"elements": {
			"panel-1": _test_v2_panel("invalid promql"),
			"panel-2": _test_v2_panel("invalid promql"),
		}},
	}
	report := rule.report with input as resource
	count(report) == 2
}

test_dashboard_v1_missing_panel_id_does_not_collide_with_real_id if {
	resource := {
		"kind": "Dashboard",
		"apiVersion": "dashboard.grafana.app/v1beta1",
		"spec": {"panels": [
			object.union(_test_v1_panel("invalid promql"), {"id": 1}),
			object.remove(_test_v1_panel("invalid promql"), ["id"]),
		]},
	}
	report := rule.report with input as resource
	count(report) == 2
}
