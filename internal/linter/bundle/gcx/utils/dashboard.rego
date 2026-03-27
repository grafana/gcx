package gcx.utils

resource_is_dashboard_v1(resource) if {
	resource.kind in {"Dashboard", "DashboardWithAccessInfo"}
	resource.apiVersion in {"dashboard.grafana.app/v0alpha1", "dashboard.grafana.app/v1beta1"}
}

dashboard_v1_panels(dashboard) := [panel | panel := dashboard.spec.panels[i]; panel.type != "row"]

dashboard_v1_variables(dashboard) := object.get(dashboard.spec, ["templating", "list"], [])

resource_is_dashboard_v2(resource) if {
	resource.kind in {"Dashboard", "DashboardWithAccessInfo"}
	resource.apiVersion in {"dashboard.grafana.app/v2beta1"}
}

dashboard_v2_panels(dashboard) := [{"id": i, "object": panel} | panel := dashboard.spec.elements[i]; panel.kind == "Panel"]

dashboard_v2_variables(dashboard) := object.get(dashboard.spec, ["variables"], [])
