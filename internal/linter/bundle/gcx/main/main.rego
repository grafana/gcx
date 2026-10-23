package gcx.main

import data.gcx.result

lint.violations = report

report contains violation if {
	not is_object(input)

	violation := {
		"category": "error",
		"rule": "invalid-input",
		"severity": "error",
		"description": "provided input must be a JSON document",
	}
}

# Built-in rules
report contains violation if {
    some resource, category, rule

    not result.rule_disabled(resource, category, rule)

    some violation in data.gcx.rules[resource][category][rule].report
}

# Custom rules
report contains violation if {
    some resource, category, rule

    not result.rule_disabled(resource, category, rule)

    some violation in data.custom.gcx.rules[resource][category][rule].report
}
