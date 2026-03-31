# METADATA
# description: Checks that Loki targets defined in dashboard panels use valid LogQL queries.
# related_resources:
#  - ref: https://github.com/grafana/gcx/blob/main/docs/reference/linter-rules/dashboard/target-valid-logql.md
#    description: documentation
# custom:
#  severity: error
package gcx.rules.dashboard.bug["target-valid-logql"]

import data.gcx.result
import data.gcx.utils

# Dashboard v1
report contains violation if {
	utils.resource_is_dashboard_v1(input)

	variables := utils.dashboard_v1_variables(input)
	panels := utils.dashboard_v1_panels(input)
	loki_targets := _loki_targets_v1(panels)

	queries := [query | query := {
		"expr": loki_targets[i].expr,
		"result": validate_logql(loki_targets[i].expr, variables),
	}]
	invalid_queries := [queries[i] | queries[i].result != ""]

	some i
	invalid_queries[i]

	violation := result.fail(rego.metadata.chain(), sprintf("%s\n%s", [invalid_queries[i].expr, invalid_queries[i].result]))
}

_loki_targets_v1(panels) := loki_targets if {
	targets := [target | panels[i].targets[j]; target := panels[i].targets[j]]

	# TODO: handle cases where no datasource is defined at the target level
	loki_targets := [target | targets[i].datasource.type == "loki"; target := targets[i]]
}

# Dashboard v2
report contains violation if {
	utils.resource_is_dashboard_v2(input)

	variables := utils.dashboard_v2_variables(input)
	panels := utils.dashboard_v2_panels(input)
	loki_targets := _loki_targets_v2(panels)

	queries := [query | query := {
		"expr": loki_targets[i].spec.query.spec.expr,
		"result": validate_logql(loki_targets[i].spec.query.spec.expr, variables),
	}]
	invalid_queries := [queries[i] | queries[i].result != ""]

	some i
	invalid_queries[i]

	violation := result.fail(rego.metadata.chain(), sprintf("%s\n%s", [invalid_queries[i].expr, invalid_queries[i].result]))
}

_loki_targets_v2(panels) := loki_targets if {
	targets := [target | panels[i].object.spec.data.spec.queries[j]; target := panels[i].object.spec.data.spec.queries[j]]
	loki_targets := [target | targets[i].spec.query.group == "loki"; target := targets[i]]
}
