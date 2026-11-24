package gcxerrors

// Exit code taxonomy for gcx.
//
// These codes let scripts and CI pipelines distinguish between error
// categories without parsing stderr text.
const (
	ExitSuccess             = 0 // Command completed successfully
	ExitGeneralError        = 1 // Unclassified/unexpected error (default)
	ExitUsageError          = 2 // Bad flags, invalid selectors, missing args, unknown commands
	ExitAuthFailure         = 3 // HTTP 401/403, missing/invalid credentials
	ExitPartialFailure      = 4 // Some resources succeeded, others failed (push/pull/delete/validate)
	ExitCancelled           = 5 // User cancelled (SIGINT) or context.Canceled
	ExitVersionIncompatible = 6 // Grafana version < 12
)

// ExitCodeInfo describes an exit code for the CLI help reference.
type ExitCodeInfo struct {
	Code        int
	Name        string
	Description string
	Example     string
}

// ExitCodes returns the exit code reference in ascending code order.
func ExitCodes() []ExitCodeInfo {
	return []ExitCodeInfo{
		{ExitSuccess, "Success", "Command completed without errors.", "A resource listing completes successfully."},
		{ExitGeneralError, "General error", "An unclassified or unexpected error occurred.", "A connection to the Grafana server fails."},
		{ExitUsageError, "Usage error", "Invalid command syntax, flags, arguments, or resource selectors.", "An unknown command or malformed resource selector is supplied."},
		{ExitAuthFailure, "Auth failure", "Authentication or authorization failed (HTTP 401/403), or credentials are missing or invalid.", "An expired token causes Grafana to return HTTP 401."},
		{ExitPartialFailure, "Partial failure", "Some resources or mutation stages succeeded while others failed.", "A resource push saves one dashboard but fails to save another."},
		{ExitCancelled, "Cancelled", "The operation was cancelled by an interrupt or context cancellation.", "Ctrl+C interrupts a running command."},
		{ExitVersionIncompatible, "Version incompatible", "The Grafana server version is older than 12.", "A command detects a Grafana 11 server."},
	}
}
