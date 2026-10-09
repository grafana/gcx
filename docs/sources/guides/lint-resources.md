---
title: Lint resources
labels:
  products:
    - cloud
    - enterprise
    - oss
description: Use gcx to lint resources.
keywords:
  - gcx
  - Grafana CLI
  - observability as code
  - telemetry
weight: 1
---

# Lint resources with `gcx`

Use the Grafana CLI linter to verify that resources such as dashboards or comply with good practices and environment-specific policies.

## Use the linter

Lint resources with:

```shell
gcx dev lint run ./resources
```

The following applies:

- Directories are recursively explored and 
- All [built-in rules](https://github.com/grafana/gcx/tree/main/internal/linter/bundle/gcx/rules) are enabled by default.

### Configure linter rules

For a finer control, you can configure the linter rules:

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

You can modify the severity level of a rule by updating the `custom.severity` annotation. Valid values are `warning` and `error`.

## Define custom linting rules

Custom and built-in rules are defined in [Rego](https://www.openpolicyagent.org/docs/policy-language), the policy language used by [Open Policy Agent (OPA)](https://www.openpolicyagent.org/). This ensures that resources comply with policies specific to your environment.

You can scaffold new custom rules with `gcx`:

```shell
# Creates a new "dashboard" linter rule in the current directory:
gcx dev lint new dashboard custom-rule
```

This generates a file with the bootstrapped rule:

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

See the existing [Built-in rules](https://github.com/grafana/gcx/tree/main/internal/linter/bundle/gcx/rules) for inspiration when writing custom ones.

### Rules for other resources

You can create rules for other resource types than dashboards, or in categories other than `idiomatic`:

```shell
# Creates a new "alertrule" linter rule, categorized under "bug":
gcx dev lint new alertrule custom-rule -c bug
```