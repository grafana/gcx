package agent

// contextExemptPaths lists command-tree paths that may run without naming a
// context under strict context mode (GCX_REQUIRE_CONTEXT). Each entry covers
// the command at that path and all of its descendants. Only commands that never
// reach a Grafana or Cloud environment belong here: local metadata, local file
// operations, and shell plumbing. Commands whose reach depends on their flags
// or arguments are decided in cmd/gcx/root/contextguard.go instead.
//
//nolint:gochecknoglobals // central strict-context registry, accessed via RequiresExplicitContextPath
var contextExemptPaths = []string{
	"gcx agent",      // Local: skills installer, invocation-log pruning.
	"gcx completion", // Shell plumbing.
	"gcx config",     // Operates on the config file itself (see contextRequiredPaths).
	"gcx dev generate",
	"gcx dev lint",
	"gcx dev scaffold",
	"gcx help",      // Cobra's help command.
	"gcx help-tree", // Local: command tree for agent context.
	"gcx instrumentation explain",
	"gcx instrumentation list-explanations",
	"gcx providers", // Local: registered provider list.
	"gcx version",
}

// contextRequiredPaths are enforced even though an ancestor path is exempt.
//
//nolint:gochecknoglobals // central strict-context registry, accessed via RequiresExplicitContextPath
var contextRequiredPaths = []string{
	"gcx config check", // Connects to the environment to verify it.
}

// RequiresExplicitContextPath reports whether the given command path (as
// returned by cobra's Command.CommandPath) must name its context under strict
// context mode. Paths are enforced unless listed as exempt, so a newly added
// command is covered by default.
func RequiresExplicitContextPath(path string) bool {
	for _, p := range contextRequiredPaths {
		if pathCovers(p, path) {
			return true
		}
	}
	for _, p := range contextExemptPaths {
		if pathCovers(p, path) {
			return false
		}
	}
	return true
}

// ContextPolicyPaths returns the command paths named by the strict context
// policy. Used by consistency tests to detect entries that no longer match a
// real command.
func ContextPolicyPaths() []string {
	out := make([]string, 0, len(contextExemptPaths)+len(contextRequiredPaths))
	out = append(out, contextExemptPaths...)
	return append(out, contextRequiredPaths...)
}
