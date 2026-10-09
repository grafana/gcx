//go:build !wasip1

package httputils

import "net/http"

// WireTransport returns the RoundTripper that puts requests on the wire,
// given the one gcx would otherwise use. Every client stack (NewClient and
// the client-go stack in config.NewNamespacedRESTConfig) applies it as its
// innermost layer, so builds for hosts without sockets can swap it out.
func WireTransport(rt http.RoundTripper) http.RoundTripper { return rt }
