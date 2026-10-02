package httputils

const (
	// AppSourceHeader identifies the application that originated a Grafana
	// Assistant request. The Assistant backend records it as the source of usage
	// events and falls back to "assistant" when it is unset.
	AppSourceHeader = "X-App-Source"

	// AppSourceCLI is gcx's Assistant app source. It must stay "cli" rather
	// than "gcx": the Assistant backend gates CLI behavior on this exact value
	// (A2A rule set, conversation listing, personal memory, CLI-specific tool
	// modes, triggered investigation visibility), and stored chats and usage
	// analytics are keyed on it. Unlike CallerIDHeader, this header selects
	// behavior, not just attribution.
	AppSourceCLI = "cli"
)
