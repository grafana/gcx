package gcx.utils_test

import data.gcx.utils

test_resource_is_alert_v0_ignores_non_alerts if {
	resource := {
	    "kind": "Folder",
	    "apiVersion": "folder.grafana.app/v1beta1",
	    "metadata": {"name": "sandbox"},
	    "spec": {"title": "Sandbox"}
	}

	not utils.resource_is_alert_v0(resource)
}

testresource_is_alert_v0_recognizes_AlertRule_v0alpha1 if {
	resource := {
	    "kind": "AlertRule",
	    "apiVersion": "rules.alerting.grafana.app/v0alpha1",
	    "metadata": {"name": "test-rule"},
	    "spec": {"does not": "matter"}
	}

	utils.resource_is_alert_v0(resource)
}
