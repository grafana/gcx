package gcxerrors

// HTTPStatusError carries the HTTP transport status of a failing request
// out-of-band, so converters and the usage-event reporter can read it without
// parsing the rendered message. Message is the transport error text: a constructor
// migrating an existing fmt.Errorf must preserve the text byte for byte,
// because converters in cmd/gcx/fail and provider tests match on it.
//
// The method set — Error, Unwrap, HTTPStatusCode — is deliberately minimal and
// must stay that way. Adding APIServiceName and APIUserMessage would satisfy
// cmd/gcx/fail's serviceAPIError interface and bypass the specialized provider
// converters and the final HTTPStatusError converter.
type HTTPStatusError struct {
	// Status is the HTTP transport status of the failing request.
	Status int
	// Message is the complete rendered error text.
	Message string
	// ServerMessage is the parsed server message, or the raw response body when
	// no message could be parsed. It excludes the rendered HTTP status prefix.
	ServerMessage string
	// TraceID is the server trace identifier, when supplied in the response.
	TraceID string
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
