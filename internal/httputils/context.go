package httputils

import "context"

type payloadLoggingKey struct{}

// WithPayloadLogging returns a context that carries the --log-http-payload flag value.
func WithPayloadLogging(ctx context.Context, enabled bool) context.Context {
	return context.WithValue(ctx, payloadLoggingKey{}, enabled)
}

// PayloadLogging returns the --log-http-payload flag value from the context.
func PayloadLogging(ctx context.Context) bool {
	v, _ := ctx.Value(payloadLoggingKey{}).(bool)
	return v
}
