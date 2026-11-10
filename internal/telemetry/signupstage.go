package telemetry

// The complete wire vocabulary for Event.SignupStage: the steps of the login
// pipeline signup runs, in order. They name what gcx itself reached; none of
// them says which Grafana Cloud page a person saw in the browser.
const (
	signupStagePreflight   = "preflight"   // checking flags, config and destination
	signupStageCredentials = "credentials" // checking that OAuth credentials can be stored
	signupStageBrowser     = "browser"     // waiting for the browser login
	signupStageCallback    = "callback"    // the browser's answer for this login ended the wait
	signupStageValidate    = "validate"    // credentials obtained; checking the connection
	signupStagePersist     = "persist"     // saving the connection
	signupStageComplete    = "complete"    // the connection was saved

	// SignupStageUnknown is a captured stage outside this vocabulary.
	SignupStageUnknown = "unknown"
)

// SignupStageLabel clamps a captured stage to the wire vocabulary: empty
// stays empty (the field is omitted), a listed value passes through, and
// anything else becomes SignupStageUnknown.
func SignupStageLabel(stage string) string {
	switch stage {
	case "":
		return ""
	case signupStagePreflight, signupStageCredentials, signupStageBrowser, signupStageCallback,
		signupStageValidate, signupStagePersist, signupStageComplete, SignupStageUnknown:
		return stage
	default:
		return SignupStageUnknown
	}
}

// SignupStageLabels returns every value Event.SignupStage can carry.
func SignupStageLabels() []string {
	return []string{
		signupStagePreflight, signupStageCredentials, signupStageBrowser, signupStageCallback,
		signupStageValidate, signupStagePersist, signupStageComplete, SignupStageUnknown,
	}
}
