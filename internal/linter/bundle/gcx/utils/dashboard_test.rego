package gcx.utils_test

import data.gcx.utils

test_resource_is_dashboard_v1_ignores_non_dashboards if {
	resource := {
	    "kind": "Folder",
	    "apiVersion": "folder.grafana.app/v1beta1",
	    "metadata": {"name": "sandbox"},
	    "spec": {"title": "Sandbox"}
	}

	not utils.resource_is_dashboard_v1(resource)
}

test_resource_is_dashboard_v1_recognizes_Dashboard_v0alpha1 if {
	resource := {
	    "kind": "Dashboard",
	    "apiVersion": "dashboard.grafana.app/v0alpha1",
	    "metadata": {"name": "test-dashboard"},
	    "spec": {"does not": "matter"}
	}

	utils.resource_is_dashboard_v1(resource)
}

test_resource_is_dashboard_v1_recognizes_Dashboard_v1beta1 if {
	resource := {
	    "kind": "Dashboard",
	    "apiVersion": "dashboard.grafana.app/v1beta1",
	    "metadata": {"name": "test-dashboard"},
	    "spec": {"does not": "matter"}
	}

	utils.resource_is_dashboard_v1(resource)
}

test_resource_is_dashboard_v1_recognizes_DashboardWithAccessInfo_v0alpha1 if {
	resource := {
	    "kind": "DashboardWithAccessInfo",
	    "apiVersion": "dashboard.grafana.app/v0alpha1",
	    "metadata": {"name": "test-dashboard"},
	    "spec": {"does not": "matter"}
	}

	utils.resource_is_dashboard_v1(resource)
}

test_resource_is_dashboard_v1_recognizes_DashboardWithAccessInfo_v1beta1 if {
	resource := {
	    "kind": "DashboardWithAccessInfo",
	    "apiVersion": "dashboard.grafana.app/v1beta1",
	    "metadata": {"name": "test-dashboard"},
	    "spec": {"does not": "matter"}
	}

	utils.resource_is_dashboard_v1(resource)
}

test_resource_is_dashboard_v2_ignores_non_dashboards if {
	resource := {
	    "kind": "Folder",
	    "apiVersion": "folder.grafana.app/v1beta1",
	    "metadata": {"name": "sandbox"},
	    "spec": {"title": "Sandbox"}
	}

	not utils.resource_is_dashboard_v2(resource)
}

test_resource_is_dashboard_v2_ignores_Dashboard_v0alpha1 if {
	resource := {
	    "kind": "Dashboard",
	    "apiVersion": "dashboard.grafana.app/v0alpha1",
	    "metadata": {"name": "test-dashboard"},
	    "spec": {"does not": "matter"}
	}

	not utils.resource_is_dashboard_v2(resource)
}

test_resource_is_dashboard_v2_ignores_Dashboard_v1beta1 if {
	resource := {
	    "kind": "Dashboard",
	    "apiVersion": "dashboard.grafana.app/v1beta1",
	    "metadata": {"name": "test-dashboard"},
	    "spec": {"does not": "matter"}
	}

	not utils.resource_is_dashboard_v2(resource)
}

test_resource_is_dashboard_v2_recognizes_Dashboard_v2beta1 if {
	resource := {
	    "kind": "Dashboard",
	    "apiVersion": "dashboard.grafana.app/v2beta1",
	    "metadata": {"name": "test-dashboard"},
	    "spec": {"does not": "matter"}
	}

	utils.resource_is_dashboard_v2(resource)
}

test_resource_is_dashboard_v2_recognizes_DashboardWithAccessInfo_v2beta1 if {
	resource := {
	    "kind": "DashboardWithAccessInfo",
	    "apiVersion": "dashboard.grafana.app/v2beta1",
	    "metadata": {"name": "test-dashboard"},
	    "spec": {"does not": "matter"}
	}

	utils.resource_is_dashboard_v2(resource)
}
