package custom.pkg.rules.dashboard.security.valid_rule

# METADATA
# title: Valid custom rule using only safe builtins
# description: Demonstrates a rule that uses only allowed builtins.

deny contains msg if {
	input.kind == "Dashboard"
	title := input.spec.title
	lower(title) == title
	msg := "dashboard title must not be all lowercase"
}
