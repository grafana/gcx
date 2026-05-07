package fail

import ifail "github.com/grafana/gcx/internal/fail"

// Exit code re-exports from internal/fail.
// The canonical definitions live there; these aliases keep cmd/ callers stable.
const (
	ExitSuccess             = ifail.ExitSuccess
	ExitGeneralError        = ifail.ExitGeneralError
	ExitUsageError          = ifail.ExitUsageError
	ExitAuthFailure         = ifail.ExitAuthFailure
	ExitPartialFailure      = ifail.ExitPartialFailure
	ExitCancelled           = ifail.ExitCancelled
	ExitVersionIncompatible = ifail.ExitVersionIncompatible
)
