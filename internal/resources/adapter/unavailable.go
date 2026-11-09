package adapter

import "errors"

// ErrUnavailable signals that a resource API is not available on the selected
// target. Selector-free pulls skip resource types whose collection returns it.
var ErrUnavailable = errors.New("resource API unavailable")

// Unavailable marks err as an unavailable resource API without changing its text
// or hiding the underlying error from errors.Is and errors.As.
func Unavailable(err error) error {
	if err == nil {
		return nil
	}
	return unavailableError{err: err}
}

type unavailableError struct {
	err error
}

func (e unavailableError) Error() string        { return e.err.Error() }
func (e unavailableError) Unwrap() error        { return e.err }
func (e unavailableError) Is(target error) bool { return target == ErrUnavailable }
