package crudcmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/resources"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// PullOpts is the standard opts struct for pull commands: an output
// directory.
type PullOpts struct {
	OutputDir string
}

// Setup registers the -d/--output-dir flag.
func (o *PullOpts) Setup(flags *pflag.FlagSet, usage string) {
	flags.StringVarP(&o.OutputDir, "output-dir", "d", ".", usage)
}

// PullConfig configures a generic "pull" command: list every item, convert
// each to a K8s-envelope resource, and write one YAML file per item under
// outputDir/SubDir.
type PullConfig[T any] struct {
	Use, Short  string
	OutputUsage string
	SubDir      string                                         // e.g. "SLO", "Report"
	Noun        string                                         // e.g. "SLO definitions", for the success message
	Fetch       func(ctx context.Context) ([]T, string, error) // returns items and the namespace to stamp on each
	ToResource  func(item T, namespace string) (*resources.Resource, error)
	ID          func(item T) string
}

// NewPullCommand builds a cobra "pull" command implementing the shared
// list-then-write-one-file-per-item flow.
func NewPullCommand[T any](cfg PullConfig[T]) *cobra.Command {
	opts := &PullOpts{}
	cmd := &cobra.Command{
		Use:   cfg.Use,
		Short: cfg.Short,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()

			items, namespace, err := cfg.Fetch(ctx)
			if err != nil {
				return err
			}

			outputDir := filepath.Join(opts.OutputDir, cfg.SubDir)
			if err := os.MkdirAll(outputDir, 0755); err != nil {
				return fmt.Errorf("failed to create output directory %s: %w", outputDir, err)
			}

			codec := format.NewYAMLCodec()

			for _, item := range items {
				res, err := cfg.ToResource(item, namespace)
				if err != nil {
					return fmt.Errorf("failed to convert %s %s to resource: %w", cfg.Noun, cfg.ID(item), err)
				}

				filePath := filepath.Join(outputDir, cfg.ID(item)+".yaml")
				f, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
				if err != nil {
					return fmt.Errorf("failed to open file %s: %w", filePath, err)
				}

				obj := res.ToUnstructured()
				if err := codec.Encode(f, &obj); err != nil {
					f.Close()
					return fmt.Errorf("failed to write %s %s: %w", cfg.Noun, cfg.ID(item), err)
				}
				f.Close()
			}

			cmdio.Success(cmd.OutOrStdout(), "Pulled %d %s to %s/", len(items), cfg.Noun, outputDir)
			return nil
		},
	}
	usage := cfg.OutputUsage
	if usage == "" {
		usage = "Directory to write files to"
	}
	opts.Setup(cmd.Flags(), usage)
	return cmd
}

// PushOpts is the standard opts struct for push commands: a --dry-run flag.
type PushOpts struct {
	DryRun bool
}

// Setup registers the --dry-run flag.
func (o *PushOpts) Setup(flags *pflag.FlagSet) {
	flags.BoolVar(&o.DryRun, "dry-run", false, "Preview changes without making them")
}

// PushConfig configures a generic "push FILE..." command: decode each file
// as a K8s-envelope YAML resource, convert it to the domain type, and either
// preview it (--dry-run) or hand it to the upsert function built by
// NewUpsert.
type PushConfig[T any] struct {
	Use, Short string

	// FromResource converts a parsed K8s-envelope resource into the domain type.
	FromResource func(res *resources.Resource) (*T, error)

	// NewUpsert builds the upsert function once per invocation (so it can
	// load config / construct a client a single time), returning a function
	// that creates-or-updates a single item (see UpsertConfig / Upsert).
	NewUpsert func(ctx context.Context) (func(cmd *cobra.Command, item *T) error, error)

	// Name and ID describe an item for the --dry-run preview message.
	Name func(item T) string
	ID   func(item T) string
}

// NewPushCommand builds a cobra "push FILE..." command implementing the
// shared decode-then-upsert-per-file flow.
func NewPushCommand[T any](cfg PushConfig[T]) *cobra.Command {
	opts := &PushOpts{}
	cmd := &cobra.Command{
		Use:   cfg.Use,
		Short: cfg.Short,
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			yamlCodec := format.NewYAMLCodec()

			upsert, err := cfg.NewUpsert(ctx)
			if err != nil {
				return err
			}

			for _, filePath := range args {
				data, err := os.ReadFile(filePath)
				if err != nil {
					return fmt.Errorf("failed to read file %s: %w", filePath, err)
				}

				var obj unstructured.Unstructured
				if err := yamlCodec.Decode(strings.NewReader(string(data)), &obj); err != nil {
					return fmt.Errorf("failed to parse %s: %w", filePath, err)
				}

				res, err := resources.FromUnstructured(&obj)
				if err != nil {
					return fmt.Errorf("failed to build resource from %s: %w", filePath, err)
				}

				item, err := cfg.FromResource(res)
				if err != nil {
					return fmt.Errorf("failed to convert resource from %s: %w", filePath, err)
				}

				if opts.DryRun {
					cmdio.Info(cmd.OutOrStdout(), "[dry-run] Would push %q (id=%s)", cfg.Name(*item), cfg.ID(*item))
					continue
				}

				if err := upsert(cmd, item); err != nil {
					return err
				}
			}

			return nil
		},
	}
	opts.Setup(cmd.Flags())
	return cmd
}
