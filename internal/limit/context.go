package limit

import "context"

type ctxKey struct{}

// Value carries the global --limit flag state through context.
type Value struct {
	N        int64
	Explicit bool // true when user passed --limit on the CLI
}

// WithLimit returns a context carrying the global limit value.
// explicit should be true when the user set the flag on the CLI,
// false for implicit defaults (e.g. agent-mode cap).
func WithLimit(ctx context.Context, n int64, explicit bool) context.Context {
	return context.WithValue(ctx, ctxKey{}, Value{N: n, Explicit: explicit})
}

// FromContext retrieves the global limit from the context.
func FromContext(ctx context.Context) (Value, bool) {
	v, ok := ctx.Value(ctxKey{}).(Value)
	return v, ok
}

// Resolve returns the effective limit for a command.
//
// Precedence:
//  1. Explicit CLI flag (--limit N) — always honored, even 0 (unlimited).
//  2. Implicit agent-mode default — only overrides commands whose own default
//     is 0 (unlimited), preventing unbounded scans in agent mode.
//  3. commandDefault — the command's natural default when no global limit is set.
func Resolve(ctx context.Context, commandDefault int64) int64 {
	v, ok := FromContext(ctx)
	if !ok {
		return commandDefault
	}
	if v.Explicit {
		return v.N
	}
	// Non-explicit (agent default): only override unlimited commands.
	if commandDefault == 0 {
		return v.N
	}
	return commandDefault
}
