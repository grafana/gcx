package fail

import ifail "github.com/grafana/gcx/internal/fail"

// DetailedError is the structured error type for gcx commands.
// The canonical definition lives in internal/fail; this alias allows cmd/
// packages to keep importing this package without change.
type DetailedError = ifail.DetailedError
