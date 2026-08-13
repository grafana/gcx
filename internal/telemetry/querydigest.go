package telemetry

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

const digestHexLen = 16

//nolint:gochecknoglobals // written in RunE, read at exit, same goroutine.
var capturedQuery string

// CaptureQuery records the resolved query for this invocation's usage event.
func CaptureQuery(expr string) {
	if e := strings.TrimSpace(expr); e != "" {
		capturedQuery = e
	}
}

// QueryDigest returns a truncated SHA-256 of the captured query, or "" if none.
func QueryDigest() string {
	if capturedQuery == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(capturedQuery))
	return hex.EncodeToString(sum[:])[:digestHexLen]
}
