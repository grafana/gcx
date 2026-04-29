package gcx.utils

resource_is_dashboard_v1(resource) if {
	resource.kind in {"Dashboard", "DashboardWithAccessInfo"}
	resource.apiVersion in {"dashboard.grafana.app/v0alpha1", "dashboard.grafana.app/v1beta1"}
}

# Top-level panels and panels nested inside (collapsed) rows.
# Grafana stores row children under `row.panels` when the row is collapsed;
# expanded rows keep their children at the top level. Both layouts must be linted.
dashboard_v1_panels(dashboard) := array.concat(
	[panel | some panel in dashboard.spec.panels; panel.type != "row"],
	[panel | some row in dashboard.spec.panels; row.type == "row"; some panel in object.get(row, "panels", [])],
)

dashboard_v1_variables(dashboard) := object.get(dashboard.spec, ["templating", "list"], [])

resource_is_dashboard_v2(resource) if {
	resource.kind in {"Dashboard", "DashboardWithAccessInfo"}
	resource.apiVersion in {"dashboard.grafana.app/v2beta1"}
}

dashboard_v2_panels(dashboard) := [{"id": i, "object": panel} | panel := dashboard.spec.elements[i]; panel.kind == "Panel"]

dashboard_v2_variables(dashboard) := object.get(dashboard.spec, ["variables"], [])
