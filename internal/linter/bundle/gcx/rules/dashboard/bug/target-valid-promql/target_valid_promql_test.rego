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
									"range": true
								},
								"version": "v0"
							},
							"refId": "A"
						}
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
		"details": "invalid promql\n1:9: parse error: unexpected identifier \"promql\"",
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
		"details": "invalid promql\n1:9: parse error: unexpected identifier \"promql\"",
		"related_resources": [{"description": "documentation", "ref": "https://github.com/grafana/gcx/blob/main/docs/reference/linter-rules/dashboard/target-valid-promql.md"}],
		"resource_type": "dashboard",
		"rule": "target-valid-promql",
		"severity": "error",
	}})
}
