# METADATA
# description: Dashboards should not be editable.
# related_resources:
#  - ref: https://github.com/grafana/gcx/blob/main/docs/reference/linter-rules/dashboard/uneditable-dashboard.md
#    description: documentation
# custom:
#  severity: warning
package gcx.rules.dashboard.idiomatic["uneditable-dashboard"]

import data.gcx.result
import data.gcx.utils

# Dashboard v1
report contains violation if {
	utils.resource_is_dashboard_v1(input)

	input.spec.editable != false

	violation := result.fail(rego.metadata.chain(), "dashboard is editable")
}

# Dashboard v2
report contains violation if {
	utils.resource_is_dashboard_v2(input)

	input.spec.editable != false

	violation := result.fail(rego.metadata.chain(), "dashboard is editable")
}
