# METADATA
# description: Checks that Prometheus targets defined in dashboard panels use valid PromQL queries.
# related_resources:
#  - ref: https://github.com/grafana/gcx/blob/main/docs/reference/linter-rules/dashboard/target-valid-promql.md
#    description: documentation
# custom:
#  severity: error
package gcx.rules.dashboard.bug["target-valid-promql"]

import data.gcx.result
import data.gcx.utils

# Dashboard v1
report contains violation if {
	utils.resource_is_dashboard_v1(input)

	variables := utils.dashboard_v1_variables(input)
	panels := utils.dashboard_v1_panels(input)
	prom_targets := _prometheus_targets_v1(panels)

	queries := [query | query := {
		"panel": prom_targets[i].panel,
		"ref_id": object.get(prom_targets[i].target, "refId", "?"),
		"expr": prom_targets[i].target.expr,
		"result": validate_promql(prom_targets[i].target.expr, variables),
	}]
	invalid_queries := [queries[i] | queries[i].result != ""]

	some i
	invalid_queries[i]

	violation := result.fail(rego.metadata.chain(), sprintf("panel %v, query '%s': %s\n%s", [invalid_queries[i].panel, invalid_queries[i].ref_id, invalid_queries[i].expr, invalid_queries[i].result]))
}

_prometheus_targets_v1(panels) := prom_targets if {
	targets := [{"panel": object.get(panels[i], "id", sprintf("index %d", [i])), "target": target} | target := panels[i].targets[j]]

	# TODO: handle cases where no datasource is defined at the target level
	prom_targets := [target | targets[i].target.datasource.type == "prometheus"; target := targets[i]]
}

# Dashboard v2
report contains violation if {
	utils.resource_is_dashboard_v2(input)

	variables := utils.dashboard_v2_variables(input)
	panels := utils.dashboard_v2_panels(input)
	prom_targets := _prometheus_targets_v2(panels, variables)

	queries := [query | query := {
		"panel": prom_targets[i].panel,
		"ref_id": object.get(prom_targets[i].target.spec, "refId", "?"),
		"expr": prom_targets[i].target.spec.query.spec.expr,
		"result": validate_promql(prom_targets[i].target.spec.query.spec.expr, variables),
	}]
	invalid_queries := [queries[i] | queries[i].result != ""]

	some i
	invalid_queries[i]

	violation := result.fail(rego.metadata.chain(), sprintf("panel '%s', query '%s': %s\n%s", [invalid_queries[i].panel, invalid_queries[i].ref_id, invalid_queries[i].expr, invalid_queries[i].result]))
}

_prometheus_targets_v2(panels, variables) := prom_targets if {
	targets := [{"panel": panels[i].id, "target": target} | target := panels[i].object.spec.data.spec.queries[j]]
	prom_targets := [target | _query_group_v2(targets[i].target, variables) == "prometheus"; target := targets[i]]
}

# Datasource variables identify the plugin actually used to run a query. Saved
# group metadata can be stale after changing the variable's plugin selection.
_query_group_v2(target, variables) := variable.spec.pluginId if {
	some variable in variables
	variable.kind == "DatasourceVariable"
	variable.spec.pluginId != ""
	target.spec.query.datasource.name in {
		sprintf("$%s", [variable.spec.name]),
		sprintf("${%s}", [variable.spec.name]),
		sprintf("[[%s]]", [variable.spec.name]),
	}
} else := target.spec.query.group
