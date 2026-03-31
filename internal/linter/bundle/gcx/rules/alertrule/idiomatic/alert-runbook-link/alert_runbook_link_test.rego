package gcx.rules.alertrule.idiomatic["alert-runbook-link_test"]

import data.gcx.utils

import data.gcx.rules.alertrule.idiomatic["alert-runbook-link"] as rule

test_alert_v0_with_runbook_is_accepted if {
	resource := {
	    "kind": "AlertRule",
	    "apiVersion": "rules.alerting.grafana.app/v0alpha1",
	    "metadata": {"name": "test-rule"},
	    "spec": {
	        "annotations": {
                "summary": "Clock not synchronising.",
                "runbook_url": "https://run.book"
	        },
	        "...": true
        }
	}

	report := rule.report with input as resource

	assert_reports_match(report, set())
}

test_alert_v0_with_no_runbook_is_rejected if {
	resource := {
	    "kind": "AlertRule",
	    "apiVersion": "rules.alerting.grafana.app/v0alpha1",
	    "metadata": {"name": "test-rule"},
	    "spec": {
	        "annotations": {
                "summary": "Clock not synchronising.",
	        },
	        "...": true
        }
	}

	report := rule.report with input as resource

	assert_reports_match(report, {{
	    "category": "idiomatic",
	    "description": "Alerts should have a runbook.",
	    "details": "alert rule has no runbook",
	    "related_resources": [],
	    "resource_type": "alertrule",
	    "rule": "alert-runbook-link",
	    "severity": "warning",
	}})
}

test_alert_v0_with_no_annotations_is_rejected if {
	resource := {
	    "kind": "AlertRule",
	    "apiVersion": "rules.alerting.grafana.app/v0alpha1",
	    "metadata": {"name": "test-rule"},
	    "spec": {
	        "...": true
        }
	}

	report := rule.report with input as resource

	assert_reports_match(report, {{
	    "category": "idiomatic",
	    "description": "Alerts should have a runbook.",
	    "details": "alert rule has no runbook",
	    "related_resources": [],
	    "resource_type": "alertrule",
	    "rule": "alert-runbook-link",
	    "severity": "warning",
	}})
}
