package gcxerrors

// Summary vocabulary for DetailedError values built by the error converters in
// cmd/gcx/fail. Each constant is one row of the "Summary vocabulary" table in
// docs/design/errors.md; tests in cmd/gcx/fail keep the two in sync and reject
// any converter summary that is not one of these constants. Service,
// datasource, operation and other specifics belong in Details.
const (
	SummaryInvalidCommandUsage       = "Invalid command usage"
	SummaryInvalidConfiguration      = "Invalid configuration"
	SummaryAuthenticationFailed      = "Authentication failed"
	SummaryKeychainLocked            = "Keychain locked"
	SummaryKeychainUnavailable       = "Keychain unavailable"
	SummaryCredentialStoreRestricted = "OS credential store access is restricted" //nolint:gosec // G101: summary text, not a credential
	SummaryAuthorizationFailed       = "Authorization failed"
	SummaryResourceNotFound          = "Resource not found"
	SummaryResourceConflict          = "Resource conflict"
	SummaryInvalidStackRequest       = "Invalid stack request"
	SummaryInvalidQuery              = "Invalid query"
	SummaryUnsupportedGrafanaVersion = "Unsupported Grafana version"
	SummaryPartialFailure            = "Partial failure"
	SummaryNetworkError              = "Network error"
	SummaryAPIError                  = "API error"
	SummaryEndpointNotAvailable      = "Endpoint not available"
	SummaryOperationCancelled        = "Operation cancelled"
	SummaryFileNotFound              = "File not found"
	SummaryInvalidPath               = "Invalid path"
	SummaryFileAccessDenied          = "File access denied"
	SummaryUnexpectedError           = "Unexpected error"
)

// Summaries returns every summary in the converter vocabulary, in table order.
// It returns a new slice on each call.
func Summaries() []string {
	return []string{
		SummaryInvalidCommandUsage,
		SummaryInvalidConfiguration,
		SummaryAuthenticationFailed,
		SummaryKeychainLocked,
		SummaryKeychainUnavailable,
		SummaryCredentialStoreRestricted,
		SummaryAuthorizationFailed,
		SummaryResourceNotFound,
		SummaryResourceConflict,
		SummaryInvalidStackRequest,
		SummaryInvalidQuery,
		SummaryUnsupportedGrafanaVersion,
		SummaryPartialFailure,
		SummaryNetworkError,
		SummaryAPIError,
		SummaryEndpointNotAvailable,
		SummaryOperationCancelled,
		SummaryFileNotFound,
		SummaryInvalidPath,
		SummaryFileAccessDenied,
		SummaryUnexpectedError,
	}
}
