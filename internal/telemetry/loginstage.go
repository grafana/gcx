package telemetry

// The complete wire vocabulary for Event.LoginStage, in pipeline order. The
// stages name what gcx itself reached; none of them says which Grafana Cloud
// page a person saw in the browser.
const (
	loginStagePreflight   = "preflight"   // checking flags, config and destination
	loginStageCredentials = "credentials" // checking that OAuth credentials can be stored
	loginStageBrowser     = "browser"     // waiting for the browser login
	loginStageCallback    = "callback"    // the browser returned to gcx with this login's state
	loginStageValidate    = "validate"    // credentials obtained; checking the connection
	loginStagePersist     = "persist"     // saving the connection
	loginStageComplete    = "complete"    // the connection was saved

	// LoginStageUnknown is a captured stage outside this vocabulary.
	LoginStageUnknown = "unknown"
)

// LoginStageLabel clamps a captured login stage to the wire vocabulary: empty
// stays empty (the field is omitted), a listed value passes through, and
// anything else becomes LoginStageUnknown.
func LoginStageLabel(stage string) string {
	switch stage {
	case "":
		return ""
	case loginStagePreflight, loginStageCredentials, loginStageBrowser, loginStageCallback,
		loginStageValidate, loginStagePersist, loginStageComplete, LoginStageUnknown:
		return stage
	default:
		return LoginStageUnknown
	}
}

// LoginStageLabels returns every value Event.LoginStage can carry.
func LoginStageLabels() []string {
	return []string{
		loginStagePreflight, loginStageCredentials, loginStageBrowser, loginStageCallback,
		loginStageValidate, loginStagePersist, loginStageComplete, LoginStageUnknown,
	}
}
