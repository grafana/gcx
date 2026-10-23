package gcx.external

import data.gcx.result

lint.violations = report

# External violations
report contains violation if {
    some violation in input.externalViolations

    not result.rule_disabled(violation.resource_type, violation.category, violation.rule)
}

