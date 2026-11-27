package errortracking

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
)

func say(w io.Writer, summary, command string) {
	cmdio.EmitHint(w, printable(summary), printable(command))
}

func usage(cmd *cobra.Command, msg string) error {
	err := gcxerrors.NewCommandUsageError(cmd, msg, nil)
	return err
}

func validIO(cmd *cobra.Command, io *cmdio.Options) error {
	if err := io.Validate(); err != nil {
		return usage(cmd, err.Error())
	}
	return nil
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

func optTime(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
