package gcx.main

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

    not _rule_disabled(data.internal, resource, category, rule)

    some violation in data.gcx.rules[resource][category][rule].report
}

# Custom rules
report contains violation if {
    some resource, category, rule

    not _rule_disabled(data.internal, resource, category, rule)

    some violation in data.custom.gcx.rules[resource][category][rule].report
}

# Enabled/disabled rules
_rule_disabled(params, resource, _, _) if {
	resource in params.disabled_resources
}

_rule_disabled(params, _, category, _) if {
	category in params.disabled_categories
}

_rule_disabled(params, _, _, rule) if {
    rule in params.disabled_rules
}

_rule_disabled(params, resource, category, rule) if {
	params.disable_all == true
	not resource in params.enabled_resources
	not category in params.enabled_categories
	not rule in params.enabled_rules
}
