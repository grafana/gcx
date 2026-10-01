package embed

import (
	"errors"
	"fmt"
	"strings"
)

// errShell is returned for shell syntax gcx cannot honour: the command is run
// directly, never through a shell.
var errShell = errors.New("shell syntax is not supported: the command runs gcx directly, without a shell (use flags such as --output or --jq instead of pipes and redirects)")

// splitCommand splits command into arguments using POSIX shell quoting rules:
// whitespace separates words, single quotes preserve everything literally,
// double quotes allow \" and \\ escapes, and a backslash outside quotes
// escapes the next character. Unquoted shell operators, and expansions
// anywhere outside single quotes, are rejected rather than passed through.
// A leading "gcx" word is dropped.
func splitCommand(command string) ([]string, error) {
	var (
		args    []string
		word    strings.Builder
		inWord  bool
		quote   rune // 0, '\'' or '"'
		escaped bool
	)

	for _, r := range command {
		switch {
		case escaped:
			word.WriteRune(r)
			escaped = false
		case quote == '\'':
			if r == '\'' {
				quote = 0
			} else {
				word.WriteRune(r)
			}
		case quote == '"':
			switch r {
			case '"':
				quote = 0
			case '\\':
				escaped = true
			case '$', '`':
				return nil, fmt.Errorf("%w: found %q", errShell, r)
			default:
				word.WriteRune(r)
			}
		case r == ' ' || r == '\t' || r == '\n':
			if inWord {
				args = append(args, word.String())
				word.Reset()
				inWord = false
			}
		default:
			inWord = true
			switch r {
			case '\'', '"':
				quote = r
			case '\\':
				escaped = true
			case '|', '&', ';', '<', '>', '(', ')', '$', '`':
				return nil, fmt.Errorf("%w: found %q", errShell, r)
			default:
				word.WriteRune(r)
			}
		}
	}

	if quote != 0 {
		return nil, fmt.Errorf("unterminated %c quote in command", quote)
	}
	if escaped {
		return nil, errors.New("command ends with an unfinished escape")
	}
	if inWord {
		args = append(args, word.String())
	}

	if len(args) > 0 && args[0] == "gcx" {
		args = args[1:]
	}
	return args, nil
}
