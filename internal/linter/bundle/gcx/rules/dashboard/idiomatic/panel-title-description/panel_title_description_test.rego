package gcx.rules.dashboard.idiomatic["panel-title-description_test"]

import data.gcx.utils

import data.gcx.rules.dashboard.idiomatic["panel-title-description"] as rule

_test_v1_panel(title, description) := {
	"id": 4,
	"datasource": {
		"type": "prometheus",
		"uid": "grafanacloud-prom",
	},
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
		"expr": "time()-process_start_time_seconds{job=\"integrations/forgejo\"}",
		"format": "time_series",
		"instant": true,
		"legendFormat": "__auto",
		"range": false,
		"refId": "A",
	}],
	"title": title,
	"description": description,
	"type": "stat",
}

_test_v2_panel(title, description) := {
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
		"id": 1,
		"title": title,
		"description": description,
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
		"spec": {"panels": [_test_v1_panel("some title", "some description")]},
	}

	report := rule.report with input as resource

	assert_reports_match(report, set())
}

test_dashboard_v1_with_no_title_description if {
	resource := {
		"kind": "Dashboard",
		"apiVersion": "dashboard.grafana.app/v1beta1",
		"metadata": {"name": "test-dashboard"},
		"spec": {"panels": [_test_v1_panel("", "")]},
	}

	report := rule.report with input as resource

	assert_reports_match(report, {{
		"category": "idiomatic",
		"description": "Panels should have a title and description.",
		"details": "panel 4 has no description",
		"related_resources": [{"description": "documentation", "ref": "https://github.com/grafana/gcx/blob/main/docs/reference/linter-rules/dashboard/panel-title-description.md"}],
		"resource_type": "dashboard",
		"rule": "panel-title-description",
		"severity": "warning",
	}, {
        "category": "idiomatic",
        "description": "Panels should have a title and description.",
        "details": "panel 4 has no title",
        "related_resources": [{"description": "documentation", "ref": "https://github.com/grafana/gcx/blob/main/docs/reference/linter-rules/dashboard/panel-title-description.md"}],
        "resource_type": "dashboard",
        "rule": "panel-title-description",
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

test_dashboard_v2_with_valid_panel if {
	resource := {
		"kind": "Dashboard",
		"apiVersion": "dashboard.grafana.app/v2beta1",
		"metadata": {"name": "test-dashboard"},
		"spec": {"elements": {"panel-1": _test_v2_panel("some title", "some description")}},
	}

	report := rule.report with input as resource

	assert_reports_match(report, set())
}

test_dashboard_v2_with_invalid_panel if {
	resource := {
		"kind": "Dashboard",
		"apiVersion": "dashboard.grafana.app/v2beta1",
		"metadata": {"name": "test-dashboard"},
		"spec": {"elements": {"panel-1": _test_v2_panel("", "")}},
	}

	report := rule.report with input as resource

	assert_reports_match(report, {{
		"category": "idiomatic",
		"description": "Panels should have a title and description.",
		"details": "panel 'panel-1' has no description",
		"related_resources": [{"description": "documentation", "ref": "https://github.com/grafana/gcx/blob/main/docs/reference/linter-rules/dashboard/panel-title-description.md"}],
		"resource_type": "dashboard",
		"rule": "panel-title-description",
		"severity": "warning",
	}, {
        "category": "idiomatic",
        "description": "Panels should have a title and description.",
        "details": "panel 'panel-1' has no title",
        "related_resources": [{"description": "documentation", "ref": "https://github.com/grafana/gcx/blob/main/docs/reference/linter-rules/dashboard/panel-title-description.md"}],
        "resource_type": "dashboard",
        "rule": "panel-title-description",
        "severity": "warning",
    }})
}
