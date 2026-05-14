package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/grafana/gcx/internal/agent"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
)

// PruneSpillFiles deletes gcx agent spill files in dir that are older than olderThan.
// Returns the number of files deleted.
func PruneSpillFiles(dir string, olderThan time.Duration) (int, error) {
	matches, err := filepath.Glob(filepath.Join(dir, cmdio.SpillFilePattern))
	if err != nil {
		return 0, fmt.Errorf("glob spill files: %w", err)
	}

	cutoff := time.Now().Add(-olderThan)
	var deleted int
	for _, match := range matches {
		info, err := os.Stat(match)
		if err != nil {
			continue // deleted between glob and stat
		}
		if info.ModTime().Before(cutoff) {
			if err := os.Remove(match); err != nil && !os.IsNotExist(err) {
				return deleted, fmt.Errorf("remove %s: %w", match, err)
			}
			deleted++
		}
	}
	return deleted, nil
}

func pruneCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "prune",
		Short: "Remove gcx agent spill files older than 30 minutes",
		Annotations: map[string]string{
			agent.AnnotationTokenCost: "small",
		},
		Long: `Remove gcx agent spill files (` + cmdio.SpillFilePattern + `) from the system temp directory that are older than 30 minutes.

These files are created when a command response exceeds the spill threshold (default 100 KiB). Run prune periodically to keep the temp directory clean, or call it at the end of an agent session.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			n, err := PruneSpillFiles(os.TempDir(), 30*time.Minute)
			if err != nil {
				return err
			}
			if n == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no spill files found older than 30 minutes")
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "removed %d spill file(s)\n", n)
			}
			return nil
		},
	}
}
