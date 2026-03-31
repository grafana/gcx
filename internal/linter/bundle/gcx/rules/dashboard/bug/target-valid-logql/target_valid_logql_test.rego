package gcx.rules.dashboard.bug["target-valid-logql_test"]

import data.gcx.utils

import data.gcx.rules.dashboard.bug["target-valid-logql"] as rule

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
			"type": "loki",
			"uid": "grafanacloud-logs",
		},
		"expr": query,
		"queryType": "range",
		"editorMode": "code",
		"direction": "backward",
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
							"query": {
								"kind": "DataQuery",
								"version": "v0",
								"group": "loki",
								"datasource": {"name": "grafanacloud-logs"},
								"spec": {
									"expr": query,
									"queryType": "range",
									"editorMode": "code",
									"direction": "backward"
								}
							},
							"refId": "A",
							"hidden": false
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
								{"color": "green", "value": 0},
								{"color": "red", "value": 80},
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
				_test_v1_panel("{cluster=\"homelab\"}"),
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
		"spec": {"panels": [_test_v1_panel("invalid logql")]},
	}

	report := rule.report with input as resource

	assert_reports_match(report, {{
		"category": "bug",
		"description": "Checks that Loki targets defined in dashboard panels use valid LogQL queries.",
		"details": "invalid logql\nparse error at line 1, col 1: syntax error: unexpected IDENTIFIER",
		"related_resources": [{"description": "documentation", "ref": "https://github.com/grafana/gcx/blob/main/docs/reference/linter-rules/dashboard/target-valid-logql.md"}],
		"resource_type": "dashboard",
		"rule": "target-valid-logql",
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
				"panel-1": _test_v2_panel("{cluster=\"homelab\"}"),
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
		"spec": {"elements": {"panel-1": _test_v2_panel("invalid logql")}},
	}

	report := rule.report with input as resource

	assert_reports_match(report, {{
		"category": "bug",
		"description": "Checks that Loki targets defined in dashboard panels use valid LogQL queries.",
		"details": "invalid logql\nparse error at line 1, col 1: syntax error: unexpected IDENTIFIER",
		"related_resources": [{"description": "documentation", "ref": "https://github.com/grafana/gcx/blob/main/docs/reference/linter-rules/dashboard/target-valid-logql.md"}],
		"resource_type": "dashboard",
		"rule": "target-valid-logql",
		"severity": "error",
	}})
}
