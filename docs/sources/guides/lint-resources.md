---
title: Lint resources
---

Grafana CLI offers a linter that can be used to verify that resources – dashboards, alerts, … —
comply with good practices and environment-specific policies.

## Using the linter

Resources can be linted using:

```shell
gcx dev lint run ./resources
```

Directories are recursively explored and all [built-in rules](../reference/linter-rules/index.md)
are enabled by default.

For a finer control, the rules used to lint resources can be configured:

```shell
# Disable all rules for a resource type:
gcx dev lint run --disable-resource dashboard ./resources

# Disable all rules in a category:
gcx dev lint run --disable-category idiomatic ./resources

# Disable specific rules:
gcx dev lint run --disable uneditable-dashboard --disable panel-title-description ./resources

# Enable rules for specific resource types:
gcx dev lint run --disable-all --enable-resource dashboard ./resources

# Enable only some categories:
gcx dev lint run --disable-all --enable-category idiomatic ./resources

# Enable only specific rules:
gcx dev lint run --disable-all --enable uneditable-dashboard ./resources
```

## Define custom linting rules

Custom and built-in rules are defined in [Rego](https://www.openpolicyagent.org/docs/policy-language),
the policy language used by [Open Policy Agent (OPA)](https://www.openpolicyagent.org/).

They can be extremely useful to make sure that resources comply with policies
specific to your environment.

New custom rules can be scaffolded with `gcx`:

```shell
# Creates a new "dashboard" linter rule in the current directory:
gcx dev lint new dashboard custom-rule
```

As a result, a file with the bootstrapped rule is generated:

```rego
# METADATA
# description: Briefly describe the rule here.
# custom:
#  severity: warning
package custom.gcx.rules.dashboard.idiomatic["custom-rule"]

import data.gcx.result
import data.gcx.utils

# Dashboard v1
report contains violation if {
	utils.resource_is_dashboard_v1(input)

	input.spec.timezone != "utc"

	violation := result.fail(rego.metadata.chain(), sprintf("timezone is '%s', expected 'utc'", input.spec.timezone))
}

# Dashboard v2
report contains violation if {
	utils.resource_is_dashboard_v2(input)

	input.spec.timeSettings.timezone != "utc"

	violation := result.fail(rego.metadata.chain(), sprintf("timezone is '%s', expected 'utc'", input.spec.timeSettings.timezone))
}
```

[Built-in rules](https://github.com/grafana/gcx/tree/main/internal/linter/bundle/gcx/rules)
can be a good source of inspiration when writing custom ones.

The severity level of a rule can be changed by updating the `custom.severity` annotation.
Valid values are `warning` and `error`.

Rules can also be created for other resource types than dashboards, or in other
categories than the `idiomatic` one:

```shell
# Creates a new "alertrule" linter rule, categorized under "bug":
gcx dev lint new alertrule custom-rule -c bug
```