package gcx.rules.dashboard.idiomatic["uneditable-dashboard_test"]

import data.gcx.utils

import data.gcx.rules.dashboard.idiomatic["uneditable-dashboard"] as rule

test_dashboard_v1_with_non_editable_is_accepted if {
	resource := {
	    "kind": "Dashboard",
	    "apiVersion": "dashboard.grafana.app/v1beta1",
	    "metadata": {"name": "test-dashboard"},
	    "spec": {"editable": false}
	}

	report := rule.report with input as resource

	assert_reports_match(report, set())
}

test_dashboard_v1_with_editable_is_rejected if {
	resource := {
	    "kind": "Dashboard",
	    "apiVersion": "dashboard.grafana.app/v1beta1",
	    "metadata": {"name": "test-dashboard"},
	    "spec": {"editable": true}
	}

	r := rule.report with input as resource

	assert_reports_match(r, {{
	    "category": "idiomatic",
	    "description": "Dashboards should not be editable.",
	    "details": "dashboard is editable",
	    "related_resources": [{"description": "documentation", "ref": "https://github.com/grafana/gcx/blob/main/docs/reference/linter-rules/dashboard/uneditable-dashboard.md"}],
	    "resource_type": "dashboard",
	    "rule": "uneditable-dashboard",
	    "severity": "warning",
	}})
}

test_dashboard_v2_with_non_editable_is_accepted if {
	resource := {
	    "kind": "Dashboard",
	    "apiVersion": "dashboard.grafana.app/v2beta1",
	    "metadata": {"name": "test-dashboard"},
	    "spec": {"editable": false}
	}

	report := rule.report with input as resource

	assert_reports_match(report, set())
}

test_dashboard_v2_with_editable_is_rejected if {
	resource := {
	    "kind": "Dashboard",
	    "apiVersion": "dashboard.grafana.app/v2beta1",
	    "metadata": {"name": "test-dashboard"},
	    "spec": {"editable": true}
	}

	r := rule.report with input as resource

	assert_reports_match(r, {{
	    "category": "idiomatic",
	    "description": "Dashboards should not be editable.",
	    "details": "dashboard is editable",
	    "related_resources": [{"description": "documentation", "ref": "https://github.com/grafana/gcx/blob/main/docs/reference/linter-rules/dashboard/uneditable-dashboard.md"}],
	    "resource_type": "dashboard",
	    "rule": "uneditable-dashboard",
	    "severity": "warning",
	}})
}
