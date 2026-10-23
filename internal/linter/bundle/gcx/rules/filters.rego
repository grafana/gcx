package gcx.result

# Enabled/disabled rules
rule_disabled(resource, _, _) if {
	resource in data.internal.disabled_resources
}

rule_disabled(_, category, _) if {
	category in data.internal.disabled_categories
}

rule_disabled(_, _, rule) if {
    rule in data.internal.disabled_rules
}

rule_disabled(resource, category, rule) if {
	data.internal.disable_all == true
	not resource in data.internal.enabled_resources
	not category in data.internal.enabled_categories
	not rule in data.internal.enabled_rules
}
