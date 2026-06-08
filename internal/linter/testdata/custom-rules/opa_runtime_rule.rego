package custom.pkg.rules.dashboard.security.opa_runtime_rule

# METADATA
# title: Rule using opa.runtime (disallowed)
# description: This rule uses opa.runtime and should fail to compile.

deny contains msg if {
	rt := opa.runtime()
	rt.config != {}
	msg := "opa runtime inspection"
}
