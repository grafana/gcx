package loki

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	dsquery "github.com/grafana/gcx/internal/datasources/query"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/query/loki"
)

const droppedLogsFlag = "--dropped-logs"

// droppedLogsIncompatibleValueFlags are removed from a follow-up command
// because the time machine has no step and the window is re-pinned.
var droppedLogsIncompatibleValueFlags = []string{"--since", "--from", "--to", "--step"} //nolint:gochecknoglobals // constant-like lookup table

// droppedLogsIncompatibleBoolFlags are removed from a follow-up command
// because no Explore or Drilldown view can show dropped lines.
var droppedLogsIncompatibleBoolFlags = []string{"--share-link", "--open", "--drilldown-link", "--open-drilldown", droppedLogsFlag} //nolint:gochecknoglobals // constant-like lookup table

// DroppedLogsCommand rewrites the invoking argv into the --dropped-logs
// follow-up for the same query. The window is pinned to absolute start/end so
// a later rerun covers the same lines even when the original used --since.
func DroppedLogsCommand(argv []string, start, end time.Time, limit int) string {
	if len(argv) == 0 {
		return ""
	}
	valueFlags := droppedLogsIncompatibleValueFlags
	appendArgs := droppedLogsWindowArgs(start, end)
	if limit > loki.MaxDroppedLogsLimit {
		valueFlags = append(append([]string(nil), valueFlags...), "--limit")
		appendArgs = append(appendArgs, "--limit", strconv.Itoa(loki.MaxDroppedLogsLimit))
	}
	appendArgs = append(appendArgs, droppedLogsFlag)
	return cmdio.RewriteCommand(argv, valueFlags, droppedLogsIncompatibleBoolFlags, appendArgs)
}

// GenericDroppedLogsCommand builds the --dropped-logs follow-up for a Loki
// query made through the generic datasources query command, which has no
// such flag. configFile and contextName are included only when non-empty;
// limit is included only when limitSet.
func GenericDroppedLogsCommand(binary, configFile, contextName, datasourceUID, expr string, start, end time.Time, limit int, limitSet bool) string {
	if binary == "" {
		binary = "gcx"
	}
	args := []string{binary}
	if configFile != "" {
		args = append(args, "--config", configFile)
	}
	if contextName != "" {
		args = append(args, "--context", contextName)
	}
	args = append(args, "logs", "query", "-d", datasourceUID, expr)
	args = append(args, droppedLogsWindowArgs(start, end)...)
	if limitSet {
		args = append(args, "--limit", strconv.Itoa(min(limit, loki.MaxDroppedLogsLimit)))
	}
	args = append(args, droppedLogsFlag)
	return cmdio.ShellCommand(args)
}

func droppedLogsWindowArgs(start, end time.Time) []string {
	return []string{
		"--from", start.UTC().Format(time.RFC3339Nano),
		"--to", end.UTC().Format(time.RFC3339Nano),
	}
}

func validateDroppedLogs(step string, limit int, share dsquery.ExploreLinkOpts, drilldown dsquery.DrilldownLinkOpts) error {
	switch {
	case step != "":
		return errors.New("--dropped-logs cannot be combined with --step: the time machine returns log lines, not samples")
	case share.ShareLink || share.Open:
		return errors.New("--dropped-logs cannot be combined with --share-link or --open: Grafana Explore cannot show dropped lines")
	case drilldown.ShareLink || drilldown.Open:
		return errors.New("--dropped-logs cannot be combined with --drilldown-link or --open-drilldown: Logs Drilldown cannot show dropped lines")
	case limit > loki.MaxDroppedLogsLimit:
		return fmt.Errorf("--dropped-logs returns at most %d lines; lower --limit", loki.MaxDroppedLogsLimit)
	}
	return nil
}
