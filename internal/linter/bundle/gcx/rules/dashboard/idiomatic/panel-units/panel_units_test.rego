package gcx.rules.dashboard.idiomatic["panel-units_test"]

import data.gcx.utils

import data.gcx.rules.dashboard.idiomatic["panel-units"] as rule

_test_v1_panel(unit) := {
	"id": 4,
	"datasource": {
		"type": "prometheus",
		"uid": "grafanacloud-prom",
	},
	"fieldConfig": {
		"defaults": {
			"color": {"fixedColor": "green", "mode": "fixed"},
			"unit": unit,
		},
		"overrides": [],
	},
	"options": {
		"colorMode": "value",
		"wideLayout": true,
	},
	"targets": [{
		"expr": "time()-process_start_time_seconds{job=\"integrations/forgejo\"}",
		"format": "time_series",
		"instant": true,
		"legendFormat": "__auto",
		"range": false,
		"refId": "A",
	}],
	"title": "Uptime",
	"type": "stat",
}

_test_v2_panel(unit) := {
	"kind": "Panel",
	"spec": {
		"data": {
			"kind": "QueryGroup",
			"spec": {
				"queries": [{
					"kind": "PanelQuery",
					"spec": {
						"...": true,
						"refId": "A",
					},
				}],
				"queryOptions": {},
				"transformations": [],
			},
		},
		"description": "",
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
						"unit": unit,
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

test_dashboard_v1_with_valid_unit if {
	resource := {
		"kind": "Dashboard",
		"apiVersion": "dashboard.grafana.app/v1beta1",
		"metadata": {"name": "test-dashboard"},
		"spec": {"panels": [_test_v1_panel("s")]},
	}

	report := rule.report with input as resource

	assert_reports_match(report, set())
}

test_dashboard_v1_with_invalid_unit if {
	resource := {
		"kind": "Dashboard",
		"apiVersion": "dashboard.grafana.app/v1beta1",
		"metadata": {"name": "test-dashboard"},
		"spec": {"panels": [_test_v1_panel("invalid")]},
	}

	report := rule.report with input as resource

	assert_reports_match(report, {{
		"category": "idiomatic",
		"description": "Panels should use valid units.",
		"details": "panel 4 uses invalid unit 'invalid'",
		"related_resources": [{"description": "documentation", "ref": "https://github.com/grafana/gcx/blob/main/docs/reference/linter-rules/dashboard/panel-units.md"}],
		"resource_type": "dashboard",
		"rule": "panel-units",
		"severity": "warning",
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

test_dashboard_v2_with_valid_unit if {
	resource := {
		"kind": "Dashboard",
		"apiVersion": "dashboard.grafana.app/v2beta1",
		"metadata": {"name": "test-dashboard"},
		"spec": {"elements": {"panel-1": _test_v2_panel("celsius")}},
	}

	report := rule.report with input as resource

	assert_reports_match(report, set())
}

test_dashboard_v2_with_invalid_unit if {
	resource := {
		"kind": "Dashboard",
		"apiVersion": "dashboard.grafana.app/v2beta1",
		"metadata": {"name": "test-dashboard"},
		"spec": {"elements": {"panel-1": _test_v2_panel("invalid")}},
	}

	report := rule.report with input as resource

	assert_reports_match(report, {{
		"category": "idiomatic",
		"description": "Panels should use valid units.",
		"details": "panel 'panel-1' uses invalid unit 'invalid'",
		"related_resources": [{"description": "documentation", "ref": "https://github.com/grafana/gcx/blob/main/docs/reference/linter-rules/dashboard/panel-units.md"}],
		"resource_type": "dashboard",
		"rule": "panel-units",
		"severity": "warning",
	}})
}
