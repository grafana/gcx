package resources

import (
	"fmt"

	"github.com/spf13/pflag"
)

// OnErrorMode controls how resource commands handle per-resource operation errors.
type OnErrorMode string

const (
	// OnErrorIgnore continues processing all resources and exits 0, even if some failed.
	OnErrorIgnore OnErrorMode = "ignore"

	// OnErrorFail continues processing all resources but exits with a non-zero code
	// if any resource operations failed. This is the default mode.
	OnErrorFail OnErrorMode = "fail"

	// OnErrorAbort stops processing on the first error and exits with a non-zero code.
	OnErrorAbort OnErrorMode = "abort"
)

// bindOnErrorFlag registers the --on-error flag on the given flag set.
func bindOnErrorFlag(flags *pflag.FlagSet, target *OnErrorMode) {
	*target = OnErrorFail
	flags.StringVar(
		(*string)(target),
		"on-error",
		string(OnErrorFail),
		`How to handle errors during resource operations:
  ignore — continue processing all resources and exit 0
  fail   — continue processing all resources and exit 1 if any failed (default)
  abort  — stop on the first error and exit 1`,
	)
}

// StopOnError reports whether the mode should abort processing on the first error.
func (m OnErrorMode) StopOnError() bool {
	return m == OnErrorAbort
}

// FailOnErrors reports whether the mode should exit with a non-zero code when any errors occurred.
func (m OnErrorMode) FailOnErrors() bool {
	return m == OnErrorFail || m == OnErrorAbort
}

// Validate returns an error if the mode value is not one of the recognized options.
func (m OnErrorMode) Validate() error {
	switch m {
	case OnErrorIgnore, OnErrorFail, OnErrorAbort:
		return nil
	default:
		return fmt.Errorf("invalid --on-error value %q: must be one of ignore, fail, abort", string(m))
	}
}
