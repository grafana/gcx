package docs

import (
	"errors"
	"fmt"
	goio "io"
	"strconv"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/style"
	"github.com/grafana/mcp-doc-server/pkg/grafanadocs"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type productsOpts struct {
	IO cmdio.Options
}

func (o *productsOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("text")
	o.IO.RegisterCustomCodec("text", &productsTextCodec{})
	o.IO.BindFlags(flags)
}

func (o *productsOpts) Validate() error {
	return o.IO.Validate()
}

// productEntry is the JSON-serializable form of a product.
type productEntry struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// productsResult wraps the product list under a "products" key.
type productsResult struct {
	Products []productEntry `json:"products"`
}

func toProductEntries(products []grafanadocs.Product) []productEntry {
	out := make([]productEntry, len(products))
	for i, p := range products {
		out[i] = productEntry{Name: p.Name, Count: p.Count}
	}
	return out
}

func productsCommand(loader *indexLoader) *cobra.Command {
	opts := &productsOpts{}
	cmd := &cobra.Command{
		Use:     "products",
		Short:   "List Grafana documentation products.",
		Long:    "List all product documentation groups in the index with their entry counts.",
		Example: `  gcx docs products`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			idx, err := loader.get(cmd.Context())
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), productsResult{
				Products: toProductEntries(idx.Products()),
			})
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// productsTextCodec renders products as a styled PRODUCT/COUNT table.
type productsTextCodec struct{}

func (c *productsTextCodec) Format() format.Format { return "text" }

func (c *productsTextCodec) Encode(w goio.Writer, v any) error {
	res, ok := v.(productsResult)
	if !ok {
		return fmt.Errorf("productsTextCodec: expected productsResult, got %T", v)
	}
	t := style.NewTable("PRODUCT", "COUNT")
	for _, p := range res.Products {
		t.Row(p.Name, strconv.Itoa(p.Count))
	}
	return t.Render(w)
}

func (c *productsTextCodec) Decode(_ goio.Reader, _ any) error {
	return errors.New("products text codec does not support decoding")
}
