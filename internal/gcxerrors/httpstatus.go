package gcxerrors

// HTTPStatusError carries the HTTP transport status of a failing request
// out-of-band, so the usage-event reporter can record it without parsing the
// rendered message. Message is the whole user-facing contract: a constructor
// migrating an existing fmt.Errorf must preserve the text byte for byte,
// because converters in cmd/gcx/fail and provider tests match on it.
//
// The method set — Error, Unwrap, HTTPStatusCode — is deliberately minimal and
// must stay that way. cmd/gcx/fail gives the auth exit code to errors
// implementing its three-method serviceAPIError interface (adding
// APIServiceName and APIUserMessage), so growing this type would silently flip
// dozens of provider 401/403 call sites from exit 1 to exit 3.
type HTTPStatusError struct {
	// Status is the HTTP transport status of the failing request.
	Status int
	// Message is the complete rendered error text.
	Message string
	// Cause is the optional underlying error, preserved for errors.Is/As.
	Cause error
}

func (e *HTTPStatusError) Error() string { return e.Message }

// Unwrap exposes the underlying cause to errors.Is/As chains. It is nil for
// messages that never wrapped anything, preserving each migrated call site's
// pre-migration unwrap shape.
func (e *HTTPStatusError) Unwrap() error { return e.Cause }

// HTTPStatusCode returns the transport status. The name matches the accessor
// the typed provider API errors already expose, so one structural probe
// covers this type and them alike.
func (e *HTTPStatusError) HTTPStatusCode() int { return e.Status }
