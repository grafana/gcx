package httputils

const (
	// AppSourceHeader identifies the application that originated a Grafana
	// Assistant request. The Assistant backend records it as the source of usage
	// events and falls back to "assistant" when it is unset.
	AppSourceHeader = "X-App-Source"

	// AppSourceCLI must stay "cli": the Assistant backend enables CLI behavior
	// only for this exact value.
	AppSourceCLI = "cli"
)
