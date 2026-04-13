package retry

// Exported aliases for black-box tests.
//
//nolint:gochecknoglobals // Test-only exports for black-box test package.
var (
	DefaultMaxRetries    = defaultMaxRetries
	DefaultMinBackoff    = defaultMinBackoff
	DefaultMaxBackoff    = defaultMaxBackoff
	DefaultMaxRetryAfter = defaultMaxRetryAfter
	ParseRetryAfter      = parseRetryAfter
	IsIdempotent         = isIdempotent
)
