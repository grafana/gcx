package gfc

import "context"

// Transport executes requests against a Grafana-related API.
// Implementations handle authentication and URL routing independently.
//
// The path argument is an API-relative path (e.g. "check/list" for SM).
// How it maps to a full URL is determined by the implementation.
type Transport interface {
	Do(ctx context.Context, method, path string, body []byte) (status int, respBody []byte, err error)
}
