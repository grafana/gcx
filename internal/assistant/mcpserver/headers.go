package mcpserver

import (
	"fmt"
	"os"
	"strings"

	assistantmcp "github.com/grafana/gcx/internal/assistant/mcpservers"
)

// resolveHeaderValue resolves h's supplied source -- inline Value, FromEnv
// (an environment variable name), or FromFile (a path) -- to its final wire
// value. At most one source may be set. A header with none set is
// name-only and resolves to an empty value, which is the preserve-on-update
// signal consumed by the client boundary (assistantmcp.HeaderWritesForUpdate,
// added in T2) -- FR-018. A referenced env var or file that is missing/empty
// is a hard error so an empty secret is never written silently (FR-020).
func resolveHeaderValue(h MCPServerHeader) (string, error) {
	sources := 0
	if h.Value != "" {
		sources++
	}
	if h.FromEnv != "" {
		sources++
	}
	if h.FromFile != "" {
		sources++
	}
	if sources > 1 {
		return "", fmt.Errorf("header %q: only one of value, fromEnv, or fromFile may be set", h.Name)
	}

	switch {
	case h.Value != "":
		return h.Value, nil
	case h.FromEnv != "":
		val, ok := os.LookupEnv(h.FromEnv)
		if !ok || val == "" {
			return "", fmt.Errorf("header %q: environment variable %q (fromEnv) is not set or empty", h.Name, h.FromEnv)
		}
		return val, nil
	case h.FromFile != "":
		data, err := os.ReadFile(h.FromFile)
		if err != nil {
			return "", fmt.Errorf("header %q: reading fromFile %q: %w", h.Name, h.FromFile, err)
		}
		val := strings.TrimRight(string(data), "\r\n")
		if val == "" {
			return "", fmt.Errorf("header %q: fromFile %q is empty", h.Name, h.FromFile)
		}
		return val, nil
	default:
		return "", nil
	}
}

// ResolveHeaders resolves every manifest header's source into the client
// boundary's plain Header list, ready to hand to assistantmcp.Client's
// Create/Update. fromEnv/fromFile are resolved here, at push time, and are
// never persisted back into a pulled manifest -- pull only ever populates
// MCPServerHeader.Name (see ServerToMCPServer below), so there is nothing to
// resolve on read (FR-020, FR-021).
//
// A resolved empty Value marks a name-only header. On an update, the client
// boundary (assistantmcp.HeaderWritesForUpdate, invoked internally by
// Client.Update) treats that as preserve-existing-secret. On a create there
// is no existing secret to preserve -- callers that determine create vs.
// update (T7's create-path guard) must reject a resolved empty Value before
// calling Create; ResolveHeaders itself does not know which path it is
// feeding and does not error on name-only headers.
//
// The returned list is always non-nil (even when headers is empty), so
// callers always pass Client.Update a full declarative header list --
// assistantmcp.HeaderWritesForUpdate's desired==nil "preserve everything"
// branch is reserved for its other caller (the mcp-servers CLI command
// path, which distinguishes "no --header flags at all" from "an explicit
// empty header list") and is never exercised via the adapter.
func ResolveHeaders(headers []MCPServerHeader) ([]assistantmcp.Header, error) {
	resolved := make([]assistantmcp.Header, 0, len(headers))
	for _, h := range headers {
		value, err := resolveHeaderValue(h)
		if err != nil {
			return nil, err
		}
		resolved = append(resolved, assistantmcp.Header{Name: h.Name, Value: value})
	}
	return resolved, nil
}

// headersFromServer converts a client Server's redacted header list
// (name-only; values are never returned on read) into the manifest's
// write-intent header shape, marking every header for preserve (FR-021).
// Used by ServerToMCPServer.
func headersFromServer(headers []assistantmcp.ServerHeader) []MCPServerHeader {
	out := make([]MCPServerHeader, 0, len(headers))
	for _, h := range headers {
		out = append(out, MCPServerHeader{Name: h.Name})
	}
	return out
}
