package output

import (
	"strconv"
	"strings"
)

// BuildPaginationCommand returns a shell command for fetching the next page of a
// K8s-style paginated command. It preserves the original argv, removes existing
// --limit/--continue flags, then appends the supplied next-page flags.
func BuildPaginationCommand(argv []string, limit int64, continueToken string) string {
	args := stripFlag(argv, "--limit")
	args = stripFlag(args, "--continue")
	if limit > 0 {
		args = append(args, "--limit", strconv.FormatInt(limit, 10))
	}
	args = append(args, "--continue", continueToken)
	return shellJoin(args)
}

// BuildListLimitCommand returns a runnable list command derived from the
// original argv: any existing --limit flag is stripped and the supplied limit
// is appended. All other flags (filters, output format, config) survive
// verbatim, so the suggestion stays faithful to the user's query — never a
// hardcoded command string.
func BuildListLimitCommand(argv []string, limit int) string {
	args := stripFlag(argv, "--limit")
	args = append(args, "--limit", strconv.Itoa(limit))
	return shellJoin(args)
}

func stripFlag(argv []string, flag string) []string {
	out := make([]string, 0, len(argv))
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		if arg == flag {
			if i+1 < len(argv) {
				i++
			}
			continue
		}
		if strings.HasPrefix(arg, flag+"=") {
			continue
		}
		out = append(out, arg)
	}
	return out
}

func shellJoin(argv []string) string {
	parts := make([]string, len(argv))
	for i, arg := range argv {
		parts[i] = shellQuoteArg(arg)
	}
	return strings.Join(parts, " ")
}

func shellQuoteArg(s string) string {
	if s != "" && isShellSafe(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func isShellSafe(s string) bool {
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case strings.ContainsRune("-_=+/:.,@%", r):
		default:
			return false
		}
	}
	return true
}
