package agentlog

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/adrg/xdg"
)

const (
	logFileName = "agent-invocation-errors.jsonl"
	// maxEntries is the maximum number of JSONL records kept in the log file.
	// When exceeded, the oldest entries are dropped to stay at this limit.
	maxEntries = 1000
)

// Config holds agentlog settings extracted from DiagnosticsConfig at startup.
type Config struct {
	Enabled bool
	LogDir  string
}

//nolint:gochecknoglobals
var cfg Config

// Configure sets agentlog options. Called once from main() before command execution.
func Configure(c Config) { cfg = c }

// IsEnabled reports whether agent invocation logging is active.
func IsEnabled() bool { return cfg.Enabled }

// LogPath returns the full path to the log file.
func LogPath() string {
	dir := cfg.LogDir
	if dir == "" {
		dir = filepath.Join(xdg.StateHome, "gcx")
	}
	return filepath.Join(dir, logFileName)
}

// Entry is one logged failed invocation.
type Entry struct {
	Timestamp time.Time `json:"timestamp"`
	Version   string    `json:"version"`
	Args      []string  `json:"args"`
	ErrorKind string    `json:"error_kind"`
	Error     string    `json:"error"`
	ExitCode  int       `json:"exit_code"`
}

// Append writes entry to the log file as a JSONL record and trims to maxEntries.
func Append(entry Entry) error {
	path := LogPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(data)
	f.Close()
	if writeErr != nil {
		return writeErr
	}
	return trimLog(path, maxEntries)
}

// trimLog keeps only the last max entries in the JSONL file, dropping the oldest.
// It is a no-op when the file has max or fewer entries.
func trimLog(path string, limit int) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	count := bytes.Count(data, []byte("\n"))
	if count <= limit {
		return nil
	}
	drop := count - limit
	for i := range data {
		if data[i] == '\n' {
			drop--
			if drop == 0 {
				data = data[i+1:]
				break
			}
		}
	}
	return os.WriteFile(path, data, 0o600)
}

// StripArgValues returns a copy of args with all flag values replaced by
// "<value>", keeping only command names and flag names. This prevents any
// user-supplied data (including secrets) from reaching the log.
//
// Rules:
//   - "--flag=value" becomes "--flag=<value>"
//   - "--flag value" becomes "--flag", "<value>" (next token is the value)
//   - "-f value" follows the same rule for single-char flags
//   - args after "--" are dropped entirely
//   - standalone positional args (subcommand names, resource kinds) are kept
func StripArgValues(args []string) []string {
	out := make([]string, 0, len(args))
	consumeNext := false
	for _, arg := range args {
		if arg == "--" {
			break
		}
		if consumeNext {
			out = append(out, "<value>")
			consumeNext = false
			continue
		}
		isLongFlag := strings.HasPrefix(arg, "--")
		isShortFlag := len(arg) == 2 && arg[0] == '-' && arg[1] != '-'
		if isLongFlag || isShortFlag {
			if before, _, ok := strings.Cut(arg, "="); ok {
				out = append(out, before+"=<value>")
			} else {
				out = append(out, arg)
				consumeNext = true
			}
		} else {
			out = append(out, arg)
			consumeNext = false
		}
	}
	return out
}

// KindFromExitCode maps an exit code to a human-readable error kind.
func KindFromExitCode(code int) string {
	switch code {
	case 2:
		return "usage_error"
	case 3:
		return "auth_failure"
	case 4:
		return "partial_failure"
	case 6:
		return "version_incompatible"
	default:
		return "error"
	}
}
