package shared

// DefaultMaxConcurrency is the default upper bound on parallel operations for
// batch I/O (errgroup limits, --max-concurrent style flags, and library
// fallbacks for an unset limit).
const DefaultMaxConcurrency = 10
