package providers

import (
	"encoding/json"
	"errors"
	"fmt"
	goio "io"
	"text/tabwriter"

	cmdio "github.com/grafana/grafanactl/cmd/grafanactl/io"
	"github.com/grafana/grafanactl/internal/format"
	coreproviders "github.com/grafana/grafanactl/internal/providers"
	"github.com/spf13/cobra"
)

type providerItem struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Command returns the "providers" command that lists all registered providers.
func Command(pp []coreproviders.Provider) *cobra.Command {
	opts := &cmdio.Options{}
	opts.DefaultFormat("text")
	opts.RegisterCustomCodec("text", &providersTextCodec{pp: pp})

	cmd := &cobra.Command{
		Use:   "providers",
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

			if opts.JSONDiscovery {
				if len(items) == 0 {
					return errors.New("no providers available for field discovery")
				}
				m, err := providerItemToMap(items[0])
				if err != nil {
					return err
				}
				for _, field := range cmdio.DiscoverFields(m) {
					fmt.Fprintln(cmd.OutOrStdout(), field)
				}
				return nil
			}

			if len(opts.JSONFields) > 0 {
				type list struct {
					Items []providerItem `json:"items"`
				}
				codec := cmdio.NewFieldSelectCodec(opts.JSONFields)
				return codec.Encode(cmd.OutOrStdout(), list{Items: items})
			}

			return opts.Encode(cmd.OutOrStdout(), items)
		},
	}

	opts.BindFlags(cmd.Flags())

	return cmd
}

// providerItemToMap marshals a providerItem to map[string]any for field discovery.
func providerItemToMap(item providerItem) (map[string]any, error) {
	data, err := json.Marshal(item)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m, nil
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
	tab := tabwriter.NewWriter(output, 0, 4, 2, ' ', tabwriter.TabIndent|tabwriter.DiscardEmptyColumns)
	fmt.Fprintf(tab, "NAME\tDESCRIPTION\n")
	for _, p := range c.pp {
		if p == nil {
			continue
		}
		fmt.Fprintf(tab, "%s\t%s\n", p.Name(), p.ShortDesc())
	}
	return tab.Flush()
}

func (c *providersTextCodec) Decode(_ goio.Reader, _ any) error {
	return errors.New("providers text codec does not support decoding")
}
