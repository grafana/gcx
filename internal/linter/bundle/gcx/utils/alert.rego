package gcx.utils

resource_is_alert_v0(resource) if {
	resource.kind in {"AlertRule"}
	resource.apiVersion in {"rules.alerting.grafana.app/v0alpha1"}
}
