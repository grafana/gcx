package config

import (
	"errors"
	"io"
	"strconv"
	"time"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
)

type configPathEntry struct {
	Path     string `json:"path"`
	Type     string `json:"type"`
	Priority int    `json:"priority"`
	Modified string `json:"modified"`
}

// configPathTableCodec renders the historical PRIORITY/TYPE/PATH/MODIFIED
// table — including the "No config files found." line for an empty set —
// byte-identical to the pre-codec output.
type configPathTableCodec struct{}

func (c *configPathTableCodec) Format() format.Format { return "table" }

func (c *configPathTableCodec) Decode(io.Reader, any) error {
	return errors.New("table codec does not support decoding")
}

func (c *configPathTableCodec) Encode(w io.Writer, value any) error {
	entries, ok := value.([]configPathEntry)
	if !ok {
		return errors.New("invalid data type for config path table codec: expected []configPathEntry")
	}

	if len(entries) == 0 {
		_, err := io.WriteString(w, "No config files found.\n")
		return err
	}

	t := style.NewTable("PRIORITY", "TYPE", "PATH", "MODIFIED")
	for _, e := range entries {
		t.Row(strconv.Itoa(e.Priority), e.Type, e.Path, e.Modified)
	}
	return t.Render(w)
}

func pathCmd(configOpts *Options) *cobra.Command {
	opts := &ioOpts{}

	cmd := &cobra.Command{
		Use:   "path",
		Short: "Show loaded config file paths",
		Long:  "Display all config files that contribute to the merged configuration, ordered by priority.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			cfg, err := configOpts.loadConfigTolerantLayered(cmd.Context())
			if err != nil {
				return err
			}

			sources := cfg.Sources
			entries := make([]configPathEntry, 0, len(sources))
			for _, src := range sources {
				entries = append(entries, configPathEntry{
					Path:     src.Path,
					Type:     src.Type,
					Priority: src.Priority(),
					Modified: src.ModTime.Format(time.DateTime),
				})
			}

			// Reverse for display: highest priority (lowest number) first.
			for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
				entries[i], entries[j] = entries[j], entries[i]
			}

			return opts.IO.Encode(cmd.OutOrStdout(), entries)
		},
	}

	opts.setup(cmd.Flags(), "table", &configPathTableCodec{})

	return cmd
}
