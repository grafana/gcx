package archive

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/docs"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
)

const experimentalPreamble = "This command is experimental. It may be removed, or its subcommands, flags and responses may change without following the normal semantic versioning conventions."

const (
	kindDestination = "archive-destination"
	kindStream      = "archive-stream"

	actionDeleted   = "deleted"
	actionValidated = "validated"
)

const maxInputFileBytes = 1 << 20 // the API's request body limit

func Commands(loader *providers.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "archive",
		Short: "Archive log streams to a cloud storage bucket you own.",
		Long: "Archive log streams to an S3, GCS or Azure Blob Storage bucket you own.\n\n" +
			"A destination is a bucket. A stream is a log stream selector whose lines are copied to one destination.\n\n" +
			"To set up a destination:\n" +
			"  1. Run 'gcx logs archive principal get' to see the identity to grant and the region the bucket must be in.\n" +
			"  2. Grant that identity list, read and write access to the bucket.\n" +
			"  3. Upload an empty object named grafana-archive-ownership under <path-prefix>/tenant=<logs-instance-id>/.\n" +
			"  4. Check the bucket with 'gcx logs archive destinations create --dry-run', then create the destination and its streams.\n\n" +
			"Requires an access policy token with the adaptive-logs:admin scope. See " + docs.AdaptiveLogsArchive + ".",
	}
	h := &archiveHelper{loader: loader}
	cmd.AddCommand(
		h.principalCommand(),
		h.destinationsCommand(),
		h.streamsCommand(),
	)
	markExperimental(cmd)
	return cmd
}

type archiveHelper struct {
	loader *providers.ConfigLoader
}

func (h *archiveHelper) newClient(ctx context.Context) (*Client, error) {
	return newClient(ctx, h.loader)
}

func markExperimental(cmd *cobra.Command) {
	long := cmd.Long
	if long == "" {
		long = cmd.Short
	}
	cmd.Short = "[experimental] " + cmd.Short
	cmd.Long = strings.TrimSpace(experimentalPreamble + "\n\n" + long)
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	cmd.Annotations[agent.AnnotationStability] = agent.StabilityExperimental
	for _, sub := range cmd.Commands() {
		markExperimental(sub)
	}
}

func registerSingleTable[T any](opts *cmdio.Options, t cmdio.Table[T]) {
	single := cmdio.TableAdapter[T, T]{
		Table: t,
		Rows:  func(v T) ([]T, error) { return []T{v}, nil },
	}
	opts.RegisterCustomCodec(cmdio.FormatTable, single.Codec(cmdio.FormatTable))
	for _, c := range t.Columns {
		if c.Visible == cmdio.WideOnly {
			opts.RegisterCustomCodec(cmdio.FormatWide, single.Codec(cmdio.FormatWide))
			return
		}
	}
}

type deleteTextCodec struct{ noun string }

func (c *deleteTextCodec) Format() format.Format { return cmdio.FormatText }

func (c *deleteTextCodec) Encode(w io.Writer, v any) error {
	m, ok := v.(cmdio.SingleMutation)
	if !ok {
		return fmt.Errorf("archive: delete text codec: expected SingleMutation, got %T", v)
	}
	label := m.Target.Name
	if label == "" {
		label = m.Target.ID
	}
	cmdio.Success(w, "Deleted %s %q", c.noun, label)
	return nil
}

func (c *deleteTextCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("text format does not support decoding")
}

// The prompt is built only when it will be shown, since building it can call
// the API.
func confirmDelete(cmd *cobra.Command, force bool, noun, label string, prompt func() (string, error)) error {
	bypass, err := providers.CheckDestructiveBypass(force)
	if err != nil || bypass {
		return err
	}
	text, err := prompt()
	if err != nil {
		return err
	}
	proceed, err := providers.ConfirmDestructive(cmd.InOrStdin(), cmd.ErrOrStderr(), false, text)
	if err != nil {
		return err
	}
	if proceed {
		return nil
	}
	exitCancelled := gcxerrors.ExitCancelled
	return &gcxerrors.DetailedError{
		Summary:  gcxerrors.SummaryOperationCancelled,
		Details:  fmt.Sprintf("Confirmation prompt was declined; %s %q was not deleted.", noun, label),
		ExitCode: &exitCancelled,
	}
}

func canonicalID(ref string) string {
	if id, err := uuid.Parse(ref); err == nil {
		return id.String()
	}
	return ref
}

// An empty ID would address the collection path, e.g. DELETE /archives/.
func rejectEmptyID(cmd *cobra.Command, noun, id string) error {
	if strings.TrimSpace(id) == "" {
		return gcxerrors.NewCommandUsageError(cmd, fmt.Sprintf("the %s ID cannot be empty", noun), nil)
	}
	return nil
}

// Not providers.ReadFileOrStdin: it decodes non-strictly, so a misspelled
// field would be silently dropped.
func readInputFile(path string, stdin io.Reader) ([]byte, error) {
	reader := stdin
	if path != "-" {
		f, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("opening file %s: %w", path, err)
		}
		defer f.Close()
		reader = f
	}

	data, err := io.ReadAll(io.LimitReader(reader, maxInputFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading input: %w", err)
	}
	if len(data) > maxInputFileBytes {
		return nil, fmt.Errorf("input exceeds maximum size (%d bytes)", maxInputFileBytes)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, errors.New("input is empty")
	}
	return data, nil
}
