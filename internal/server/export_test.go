package server

import "net/http"

// MakeOriginChecker exposes makeOriginChecker for white-box testing.
func MakeOriginChecker(listenAddr string) func(r *http.Request) bool {
	return makeOriginChecker(listenAddr)
}
