package version

import (
	"errors"
	"fmt"
	goio "io"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/style"
	appversion "github.com/grafana/gcx/internal/version"
	"github.com/spf13/cobra"
)

// Command returns the "version" subcommand with structured output support.
func Command() *cobra.Command {
	opts := &cmdio.Options{}
	opts.DefaultFormat("text")
	opts.RegisterCustomCodec("text", &versionTextCodec{})

	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print version information.",
		Long:  "Print version, commit, build date, Go version, OS, and architecture. Supports structured output via --output json/yaml.",
		Example: `  # Human-readable version
  gcx version

  # JSON output for automation
  gcx version -o json

  # Select specific fields
  gcx version --json version,commit`,
		Args: cobra.NoArgs,
		Annotations: map[string]string{
			agent.AnnotationTokenCost: "small",
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			return opts.Encode(cmd.OutOrStdout(), appversion.Info())
		},
	}

	opts.BindFlags(cmd.Flags())

	return cmd
}

// versionTextCodec renders version info as a key-value table.
type versionTextCodec struct{}

func (c *versionTextCodec) Format() format.Format { return "text" }

func (c *versionTextCodec) Encode(output goio.Writer, value any) error {
	info, ok := value.(appversion.InfoData)
	if !ok {
		return fmt.Errorf("unexpected type %T", value)
	}
	t := style.NewTable("FIELD", "VALUE")
	t.Row("Version", info.Version)
	t.Row("Commit", info.Commit)
	t.Row("Build Date", info.BuildDate)
	t.Row("Go", info.Go)
	t.Row("OS", info.OS)
	t.Row("Arch", info.Arch)
	return t.Render(output)
}

func (c *versionTextCodec) Decode(_ goio.Reader, _ any) error {
	return errors.New("version text codec does not support decoding")
}
