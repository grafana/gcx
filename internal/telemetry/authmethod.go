package telemetry

// The complete wire vocabulary for Event.GrafanaAuthMethod. Absence is the
// seventh state and carries its own meaning — no Grafana context was ever
// selected, or several different methods were used in one invocation — so
// there is deliberately no "none" or "mixed" value.
const (
	// AuthMethodAnonymous is a valid selection with no credential material:
	// the invocation talked (or would have talked) to Grafana without
	// authenticating. It is a different fact from AuthMethodUnknown.
	AuthMethodAnonymous = "anonymous"
	// AuthMethodUnknown is a selection that ran and could not classify the
	// method, such as an unsupported configured auth-method value. A known
	// method whose credential is merely invalid reports the method instead.
	AuthMethodUnknown = "unknown"

	authMethodOAuth = "oauth"
	authMethodToken = "token"
	authMethodBasic = "basic"
	authMethodMTLS  = "mtls"
)

// GrafanaAuthMethodLabel clamps a captured auth method to the wire
// vocabulary: empty stays empty (the field is omitted), a listed value passes
// through, and anything else becomes AuthMethodUnknown. The capture side
// already writes only decided values, but this clamp is deliberately
// independent of it: no future refactor of the selector can leak an
// arbitrary string onto the wire through this field.
func GrafanaAuthMethodLabel(method string) string {
	switch method {
	case "":
		return ""
	case authMethodOAuth, authMethodToken, authMethodBasic, authMethodMTLS,
		AuthMethodAnonymous, AuthMethodUnknown:
		return method
	default:
		return AuthMethodUnknown
	}
}

// GrafanaAuthMethodLabels returns every value Event.GrafanaAuthMethod can
// carry, for tests and for validating the vocabulary against the receiver.
func GrafanaAuthMethodLabels() []string {
	return []string{
		authMethodOAuth, authMethodToken, authMethodBasic, authMethodMTLS,
		AuthMethodAnonymous, AuthMethodUnknown,
	}
}
