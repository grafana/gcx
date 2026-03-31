package fail

// Exit code taxonomy for gcx.
//
// These codes let scripts and CI pipelines distinguish between error
// categories without parsing stderr text.
//
// Codes marked [RESERVED] or [PLANNED] are defined for forward compatibility
// but are not yet emitted by any converter.
const (
	ExitSuccess             = 0 // Command completed successfully
	ExitGeneralError        = 1 // Unclassified/unexpected error (default)
	ExitUsageError          = 2 // Bad flags, invalid selectors, missing args [RESERVED]
	ExitAuthFailure         = 3 // HTTP 401/403, missing/invalid credentials
	ExitPartialFailure      = 4 // Some resources succeeded, others failed [RESERVED]
	ExitCancelled           = 5 // User cancelled (SIGINT) or context.Canceled
	ExitVersionIncompatible = 6 // Grafana version < 12
)
