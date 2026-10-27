package gcx.external

import data.gcx.rules

lint.violations = report

# External violations
report contains violation if {
    some violation in input.externalViolations

    not rules.rule_disabled(violation.resource_type, violation.category, violation.rule)
}

