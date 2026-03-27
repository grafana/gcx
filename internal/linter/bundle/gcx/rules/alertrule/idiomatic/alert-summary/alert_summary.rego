# METADATA
# description: Alerts must have a summary.
# custom:
#  severity: error
package gcx.rules.alertrule.idiomatic["alert-summary"]

import data.gcx.result
import data.gcx.utils

report contains violation if {
	utils.resource_is_alert_v0(input)

	object.get(input.spec, ["annotations", "summary"], "") == ""

	violation := result.fail(rego.metadata.chain(), "alert rule has no summary")
}
