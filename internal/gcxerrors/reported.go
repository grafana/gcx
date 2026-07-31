package gcxerrors

import "errors"

// ErrAlreadyReported is returned when a command has rendered a complete
// diagnostic report but still needs the process to exit unsuccessfully.
// Error reporting converts this sentinel into a non-zero exit without writing
// a second human or machine-readable error envelope.
var ErrAlreadyReported = errors.New("failure already reported")

// alreadyReportedError carries the process exit code for an error whose full
// diagnostic has already been rendered by the command.
type alreadyReportedError struct {
	code int
}

func (e *alreadyReportedError) Error() string {
	return ErrAlreadyReported.Error()
}

func (e *alreadyReportedError) Unwrap() error {
	return ErrAlreadyReported
}

// NewAlreadyReportedError creates a silent command failure with the requested
// process exit code. Zero and negative values fall back to ExitGeneralError.
func NewAlreadyReportedError(code int) error {
	if code <= ExitSuccess {
		code = ExitGeneralError
	}
	return &alreadyReportedError{code: code}
}

// AlreadyReportedExitCode returns the requested exit code for an already
// rendered failure. A bare ErrAlreadyReported uses ExitGeneralError.
func AlreadyReportedExitCode(err error) (int, bool) {
	var reported *alreadyReportedError
	if errors.As(err, &reported) {
		return reported.code, true
	}
	if errors.Is(err, ErrAlreadyReported) {
		return ExitGeneralError, true
	}
	return ExitSuccess, false
}
