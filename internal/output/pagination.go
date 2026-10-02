package output

import (
	"strconv"
	"strings"

	"github.com/grafana/gcx/internal/shellquote"
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
	return shellquote.Join(args)
}

// BuildListLimitCommand returns a runnable list command derived from the
// original argv: any existing --limit flag is stripped and the supplied limit
// is appended. All other flags (filters, output format, config) survive
// verbatim, so the suggestion stays faithful to the user's query — never a
// hardcoded command string.
func BuildListLimitCommand(argv []string, limit int) string {
	args := stripFlag(argv, "--limit")
	args = append(args, "--limit", strconv.Itoa(limit))
	return shellquote.Join(args)
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
