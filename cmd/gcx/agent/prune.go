package agent

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// pruneOlderThan is the fixed age threshold for spill file removal.
const pruneOlderThan = 30 * time.Minute

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

type pruneOpts struct {
	IO cmdio.Options
}

func (o *pruneOpts) setup(flags *pflag.FlagSet) {
	// The prune result is a BatchMutation document through the codec system:
	// the default text codec prints the familiar one-line summary; agent mode
	// and explicit -o json/yaml get the structured document.
	o.IO.RegisterCustomCodec("text", &pruneTextCodec{})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
}

func (o *pruneOpts) Validate() error {
	return o.IO.Validate()
}

// pruneTextCodec is the human "text" codec for the prune BatchMutation: it
// reproduces the exact lines prune has always printed, so default human
// stdout stays byte-identical to the pre-codec output.
type pruneTextCodec struct{}

func (c *pruneTextCodec) Format() format.Format { return "text" }

func (c *pruneTextCodec) Decode(io.Reader, any) error {
	return errors.New("prune text codec does not support decoding")
}

func (c *pruneTextCodec) Encode(w io.Writer, value any) error {
	result, ok := value.(cmdio.BatchMutation)
	if !ok {
		return errors.New("invalid data type for prune text codec: expected BatchMutation")
	}
	if result.Summary.Succeeded == 0 {
		_, err := fmt.Fprintln(w, "no spill files found older than 30 minutes")
		return err
	}
	_, err := fmt.Fprintf(w, "removed %d spill file(s)\n", result.Summary.Succeeded)
	return err
}

func pruneCommand() *cobra.Command {
	opts := &pruneOpts{}

	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Remove gcx agent spill files older than 30 minutes",
		Annotations: map[string]string{
			agent.AnnotationTokenCost: "small",
		},
		Long: `Remove gcx agent spill files (` + cmdio.SpillFilePattern + `) from the system temp directory that are older than 30 minutes.

These files are created when a command response exceeds the spill threshold (default 100 KiB). Run prune periodically to keep the temp directory clean, or call it at the end of an agent session.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			n, err := PruneSpillFiles(os.TempDir(), pruneOlderThan)
			if err != nil {
				// Nothing has been written to stdout yet — the standard
				// error path (single fused error document in agent mode)
				// is the honest one.
				return err
			}

			result := cmdio.NewBatchMutation("pruned")
			result.Summary = cmdio.MutationSummary{Succeeded: n}
			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}

	opts.setup(cmd.Flags())

	return cmd
}
