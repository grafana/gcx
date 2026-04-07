package providers

import (
	"errors"
	"fmt"
	goio "io"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	coreproviders "github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
)

type providerItem struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Command returns the "providers" command group. Running "gcx providers"
// without a subcommand is equivalent to "gcx providers list".
func Command(pp []coreproviders.Provider) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "providers",
		Short: "Manage registered providers",
	}

	cmd.AddCommand(newListCommand(pp))

	return cmd
}

func newListCommand(pp []coreproviders.Provider) *cobra.Command {
	opts := &cmdio.Options{}
	opts.DefaultFormat("text")
	opts.RegisterCustomCodec("text", &providersTextCodec{pp: pp})

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List registered providers",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			items := make([]providerItem, 0, len(pp))
			for _, p := range pp {
				if p == nil {
					continue
				}
				items = append(items, providerItem{Name: p.Name(), Description: p.ShortDesc()})
			}

			return opts.Encode(cmd.OutOrStdout(), items)
		},
	}

	opts.BindFlags(cmd.Flags())

	return cmd
}

// providersTextCodec renders a tabwriter table of providers.
type providersTextCodec struct {
	pp []coreproviders.Provider
}

func (c *providersTextCodec) Format() format.Format { return "text" }

func (c *providersTextCodec) Encode(output goio.Writer, _ any) error {
	if len(c.pp) == 0 {
		fmt.Fprintf(output, "No providers registered.\n")
		return nil
	}
	t := style.NewTable("NAME", "DESCRIPTION")
	for _, p := range c.pp {
		if p == nil {
			continue
		}
		t.Row(p.Name(), p.ShortDesc())
	}
	return t.Render(output)
}

func (c *providersTextCodec) Decode(_ goio.Reader, _ any) error {
	return errors.New("providers text codec does not support decoding")
}
