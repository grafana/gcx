package errortracking

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
)

func say(w io.Writer, summary, command string) {
	cmdio.EmitHint(w, printable(summary), printable(command))
}

func usage(cmd *cobra.Command, msg string, suggestions ...string) error {
	err := gcxerrors.NewCommandUsageError(cmd, msg, nil)
	err.Suggestions = append(suggestions, err.Suggestions...)
	return err
}

func validIO(cmd *cobra.Command, io *cmdio.Options) error {
	if err := io.Validate(); err != nil {
		return usage(cmd, err.Error())
	}
	return nil
}

const (
	projectSlot = "gcx-slot-project"
	errorSlot   = "gcx-slot-error"
	dirSlot     = "gcx-slot-dir"
)

func et(args ...string) string {
	slots := strings.NewReplacer("'"+projectSlot+"'", "<project>", projectSlot, "<project>", "'"+errorSlot+"'", "<error>", errorSlot, "<error>", "'"+dirSlot+"'", "<dir>", dirSlot, "<dir>")
	return slots.Replace(cmdio.ShellCommand(append([]string{"gcx", "error-tracking"}, args...)))
}

func projectOf(cmd *cobra.Command) string {
	if f := cmd.Flags().Lookup("project"); f != nil && f.Changed && strings.TrimSpace(f.Value.String()) != "" {
		return printable(strings.TrimSpace(f.Value.String()))
	}
	return projectSlot
}

func argHelp(cmd *cobra.Command, name string, args []string) string {
	project := projectOf(cmd)
	errID := errorSlot
	if len(args) > 0 {
		errID = printable(args[0])
	}
	switch name {
	case "<error>":
		return et("errors", "list", "--project", project)
	case "<comment>":
		return et("errors", "activity", "list", errID, "--project", project, "--type", "comment")
	case "<raw-event>":
		return et("raw-events", "list", "--project", project)
	case "<user>":
		return et("users", "list", "--project", project)
	}
	return cmd.CommandPath() + " --help"
}

const maxID = 1<<53 - 1

func parseID(v string) (int64, bool, bool) {
	id, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		ne, isNum := errors.AsType[*strconv.NumError](err)
		return 0, isNum && errors.Is(ne.Err, strconv.ErrRange), false
	}
	return id, true, id > 0 && id <= maxID
}

func positiveID(cmd *cobra.Command, label, arg string) (int64, error) {
	id, _, ok := parseID(strings.TrimSpace(arg))
	if !ok {
		return 0, positiveIDErr(cmd, label, arg)
	}
	return id, nil
}

func positiveIDErr(cmd *cobra.Command, label, arg string) error {
	kind := strings.Trim(label, "<>-")
	see := argHelp(cmd, "<"+kind+">", nil)
	if kind == "raw-event" {
		return usage(cmd, fmt.Sprintf("%s %q must be a positive raw event ID or an EVENT ID", label, arg), "See them: "+see)
	}
	return usage(cmd, fmt.Sprintf("%s %q must be a positive %s ID", label, arg, strings.ReplaceAll(kind, "-", " ")), "See them: "+see)
}

func projectLabel(id int64, name string) string {
	if name = printable(name); name != "" {
		return fmt.Sprintf("project %s (ID %d)", name, id)
	}
	return fmt.Sprintf("project %d", id)
}

func parseTime(ts string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(ts))
	return t, err == nil
}

func age(ts string) string {
	t, ok := parseTime(ts)
	if !ok {
		return cmdio.OrDash(printable(ts))
	}
	return cmdio.RelativeAge(time.Since(t))
}

func stamp(ts string) string {
	t, ok := parseTime(ts)
	if !ok {
		return cmdio.OrDash(printable(ts))
	}
	return t.UTC().Format("2006-01-02 15:04:05Z")
}

func minute(ts string) string {
	t, ok := parseTime(ts)
	if !ok {
		return cmdio.OrDash(printable(ts))
	}
	return t.UTC().Format("2006-01-02 15:04Z")
}

func optTime(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func capital(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	if size == 0 {
		return s
	}
	return string(unicode.ToUpper(r)) + s[size:]
}

func minutes(n int64) string {
	switch {
	case n > 0 && n%(24*60) == 0:
		return strconv.FormatInt(n/(24*60), 10) + "d"
	case n > 0 && n%60 == 0:
		return strconv.FormatInt(n/60, 10) + "h"
	case n > 60:
		return fmt.Sprintf("%dh%dm", n/60, n%60)
	}
	return strconv.FormatInt(n, 10) + "m"
}
