package output

import (
	"runtime"
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
	return shellJoinForOS(argv, runtime.GOOS)
}

// ShellCommand formats a command for a POSIX shell on Unix. On Windows, simple
// commands also work in cmd.exe. Commands that need quoting use PowerShell
// syntax. The caller must use PowerShell to run those Windows commands.
func ShellCommand(argv []string) string {
	return shellJoin(argv)
}

func shellJoinForOS(argv []string, goos string) string {
	allSafe := true
	for _, arg := range argv {
		if arg == "" || !isShellSafe(arg) || (goos == "windows" && (strings.ContainsRune(arg, '%') || strings.HasPrefix(arg, "@"))) {
			allSafe = false
			break
		}
	}
	if allSafe {
		return strings.Join(argv, " ")
	}
	if goos == "windows" {
		parts := make([]string, len(argv))
		for i, arg := range argv {
			parts[i] = powerShellQuote(arg)
		}
		// PowerShell needs the call operator for a quoted executable path.
		// Windows PowerShell 5.1 loses empty arguments and literal double quotes
		// when it passes ordinary strings to a native command. A trailing backslash
		// can also escape its closing quote. Use its legacy
		// command-line format for those values. PowerShell 7 can use the same
		// format when its native argument mode is set to Legacy in this scope.
		for _, arg := range argv[1:] {
			if arg == "" || strings.ContainsRune(arg, '"') || strings.HasSuffix(arg, `\`) {
				for i := 1; i < len(parts); i++ {
					parts[i] = powerShellQuote(windowsCommandLineQuote(argv[i]))
				}
				return "& { $PSNativeCommandArgumentPassing = 'Legacy'; & " + strings.Join(parts, " ") + " }"
			}
		}
		return "& " + strings.Join(parts, " ")
	}
	parts := make([]string, len(argv))
	for i, arg := range argv {
		parts[i] = shellQuoteArg(arg)
	}
	return strings.Join(parts, " ")
}

func powerShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// windowsCommandLineQuote preserves an argument when Legacy mode joins native
// arguments into one command line. The Windows runtime then parses that line.
func windowsCommandLineQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	backslashes := 0
	for _, r := range s {
		switch r {
		case '\\':
			backslashes++
		case '"':
			b.WriteString(strings.Repeat("\\", backslashes*2+1))
			b.WriteByte('"')
			backslashes = 0
		default:
			b.WriteString(strings.Repeat("\\", backslashes))
			backslashes = 0
			b.WriteRune(r)
		}
	}
	b.WriteString(strings.Repeat("\\", backslashes*2))
	b.WriteByte('"')
	return b.String()
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
