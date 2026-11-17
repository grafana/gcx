package gcx.external

import data.gcx.rules

lint.violations = report

# Fatal external violations are always reported
report contains violation if {
    some violation in input.externalViolations

    violation.severity == "fatal"
}

# Other external violations
report contains violation if {
    some violation in input.externalViolations

    not rules.rule_disabled(violation.resource_type, violation.category, violation.rule)
}

