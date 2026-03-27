# METADATA
# description: Panels should have a title and description.
# related_resources:
#  - ref: https://github.com/grafana/gcx/blob/main/docs/reference/linter-rules/dashboard/panel-title-description.md
#    description: documentation
# custom:
#  severity: warning
package gcx.rules.dashboard.idiomatic["panel-title-description"]

import data.gcx.result
import data.gcx.utils

# Dashboard v1 – titles
report contains violation if {
	utils.resource_is_dashboard_v1(input)

	panels := utils.dashboard_v1_panels(input)
	invalid_panels := [panels[i] | object.get(panels[i], ["title"], "") == ""]

	some i
	invalid_panels[i]

	violation := result.fail(rego.metadata.chain(), sprintf("panel %d has no title", [invalid_panels[i].id]))
}

# Dashboard v1 – descriptions
report contains violation if {
	utils.resource_is_dashboard_v1(input)

	panels := utils.dashboard_v1_panels(input)
	invalid_panels := [panels[i] | object.get(panels[i], ["description"], "") == ""]

	some i
	invalid_panels[i]

	violation := result.fail(rego.metadata.chain(), sprintf("panel %d has no description", [invalid_panels[i].id]))
}

# Dashboard v2 – titles
report contains violation if {
	utils.resource_is_dashboard_v2(input)

	panels := utils.dashboard_v2_panels(input)
	invalid_panels := [panels[i] | object.get(panels[i].object.spec, ["title"], "") == ""]

	some i
	invalid_panels[i]

	violation := result.fail(rego.metadata.chain(), sprintf("panel '%s' has no title", [invalid_panels[i].id]))
}

# Dashboard v2 – descriptions
report contains violation if {
	utils.resource_is_dashboard_v2(input)

	panels := utils.dashboard_v2_panels(input)
	invalid_panels := [panels[i] | object.get(panels[i].object.spec, ["description"], "") == ""]

	some i
	invalid_panels[i]

	violation := result.fail(rego.metadata.chain(), sprintf("panel '%s' has no description", [invalid_panels[i].id]))
}
