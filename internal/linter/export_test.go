package linter

import "github.com/open-policy-agent/opa/v1/ast"

// RestrictedCapabilities exposes the unexported restrictedCapabilities helper for black-box tests.
func RestrictedCapabilities() *ast.Capabilities {
	return restrictedCapabilities()
}
