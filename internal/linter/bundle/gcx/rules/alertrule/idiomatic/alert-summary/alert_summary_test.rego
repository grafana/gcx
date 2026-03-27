package gcx.rules.alertrule.idiomatic["alert-summary_test"]

import data.gcx.utils

import data.gcx.rules.alertrule.idiomatic["alert-summary"] as rule

test_alert_v0_with_summary_is_accepted if {
	resource := {
	    "kind": "AlertRule",
	    "apiVersion": "rules.alerting.grafana.app/v0alpha1",
	    "metadata": {"name": "test-rule"},
	    "spec": {
	        "annotations": {
                "description": "Clock at {{ $labels.instance }} is not synchronising. Ensure NTP is configured on this host.",
                "summary": "Clock not synchronising."
	        },
	        "...": true
        }
	}

	report := rule.report with input as resource

	assert_reports_match(report, set())
}

test_alert_v0_with_no_summary_is_rejected if {
	resource := {
	    "kind": "AlertRule",
	    "apiVersion": "rules.alerting.grafana.app/v0alpha1",
	    "metadata": {"name": "test-rule"},
	    "spec": {
	        "annotations": {
                "description": "Clock at {{ $labels.instance }} is not synchronising. Ensure NTP is configured on this host.",
	        },
	        "...": true
        }
	}

	report := rule.report with input as resource

	assert_reports_match(report, {{
	    "category": "idiomatic",
	    "description": "Alerts must have a summary.",
	    "details": "alert rule has no summary",
	    "related_resources": [],
	    "resource_type": "alertrule",
	    "rule": "alert-summary",
	    "severity": "error",
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
	    "description": "Alerts must have a summary.",
	    "details": "alert rule has no summary",
	    "related_resources": [],
	    "resource_type": "alertrule",
	    "rule": "alert-summary",
	    "severity": "error",
	}})
}
