package instrumentation

import "errors"

// ErrMutuallyExclusiveFlags is the sentinel returned by command Validate()
// implementations when the user supplies a pair of mutually exclusive flags
// (e.g. --costmetrics together with --no-costmetrics). The fail converter
// surfaces it as the standard "Invalid command usage" envelope, with the
// wrapped error's message providing the per-call detail.
var ErrMutuallyExclusiveFlags = errors.New("mutually exclusive flags")
