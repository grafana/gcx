package crudcmd

import (
	"context"
	"fmt"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// TypedListConfig configures a generic "list" command for TypedCRUD-backed
// resources whose output diverges by format: the table/wide codecs render
// the raw domain slice directly, while every other format (yaml/json)
// converts each item to a K8s-envelope unstructured resource for
// consistency with get/pull and round-trip support. This is the dual-path
// list shape shared by most TypedCRUD-backed provider resources (guards,
// rules, probes, ...).
//
// Resources whose list additionally applies client-side filters (e.g. synth
// checks' --label/--job) don't fit this constructor's fixed shape and should
// call ListOpts directly instead.
type TypedListConfig[T adapter.ResourceNamer] struct {
	Use, Short   string
	Example      string
	DefaultFmt   string
	LimitDefault int64
	LimitUsage   string
	Codecs       []format.Codec

	// NewCRUD builds the TypedCRUD once per invocation, returning it along
	// with the namespace to stamp on converted resources.
	NewCRUD func(ctx context.Context) (*adapter.TypedCRUD[T], string, error)
	// ToResource converts a domain item into a K8s-envelope unstructured
	// object for non-table output formats. It receives the TypedCRUD built
	// by NewCRUD so implementations may use crud.Namespace or
	// crud.ToUnstructured directly.
	ToResource func(crud *adapter.TypedCRUD[T], item T) (unstructured.Unstructured, error)
	// Noun names the resource in conversion error messages (e.g. "SLO",
	// "probe"). Defaults to "item".
	Noun string
}

// NewTypedListCommand builds a cobra "list" command from cfg.
func NewTypedListCommand[T adapter.ResourceNamer](cfg TypedListConfig[T]) *cobra.Command {
	opts := &ListOpts{}
	cmd := &cobra.Command{
		Use:     cfg.Use,
		Short:   cfg.Short,
		Example: cfg.Example,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			crud, _, err := cfg.NewCRUD(ctx)
			if err != nil {
				return err
			}

			typedObjs, err := crud.List(ctx, opts.Limit)
			if err != nil {
				return err
			}

			items := make([]T, len(typedObjs))
			for i := range typedObjs {
				items[i] = typedObjs[i].Spec
			}

			if opts.IO.OutputFormat == "table" || opts.IO.OutputFormat == "wide" {
				return opts.IO.Encode(cmd.OutOrStdout(), items)
			}

			noun := cfg.Noun
			if noun == "" {
				noun = "item"
			}

			objs := make([]unstructured.Unstructured, 0, len(items))
			for _, item := range items {
				obj, err := cfg.ToResource(crud, item)
				if err != nil {
					return fmt.Errorf("failed to convert %s %s to resource: %w", noun, item.GetResourceName(), err)
				}
				objs = append(objs, obj)
			}
			return opts.IO.Encode(cmd.OutOrStdout(), objs)
		},
	}
	opts.Setup(cmd.Flags(), cfg.DefaultFmt, cfg.LimitDefault, cfg.LimitUsage, cfg.Codecs...)
	return cmd
}
