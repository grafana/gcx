package kg

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	k8syaml "sigs.k8s.io/yaml"
)

// thresholdIdentityFlags binds the {record, expr, labels} identity triple shared
// by `thresholds upsert` and `thresholds delete`.
type thresholdIdentityFlags struct {
	Record string
	Expr   string
	Labels map[string]string
}

func (f *thresholdIdentityFlags) bind(flags *pflag.FlagSet) {
	flags.StringVar(&f.Record, "record", "", "Threshold record name, e.g. asserts:resource:rate:critical (required)")
	flags.StringVar(&f.Expr, "expr", "", "PromQL threshold expression (required)")
	flags.StringToStringVar(&f.Labels, "label", nil, "Threshold label as key=value (repeatable or comma-separated; identity-significant)")
}

func (f *thresholdIdentityFlags) validate() error {
	if f.Record == "" {
		return errors.New("--record is required")
	}
	if f.Expr == "" {
		return errors.New("--expr is required")
	}
	return nil
}

func (f *thresholdIdentityFlags) identity() ThresholdIdentity {
	return ThresholdIdentity{Record: f.Record, Expr: f.Expr, Labels: f.Labels}
}

type thresholdsUpsertOpts struct {
	thresholdIdentityFlags

	IO cmdio.Options
}

func (o *thresholdsUpsertOpts) setup(flags *pflag.FlagSet) {
	o.bind(flags)
	o.IO.RegisterCustomCodec("text", singleMutationText(func(w io.Writer, m cmdio.SingleMutation) {
		cmdio.Success(w, "Threshold %q upserted", m.Target.Name)
	}))
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
}

func newThresholdsUpsertCommand(loader RESTConfigLoader) *cobra.Command {
	opts := &thresholdsUpsertOpts{}
	cmd := &cobra.Command{
		Use:   "upsert",
		Short: "Create or update a single threshold.",
		Long: `Creates or updates one threshold over the v1 threshold config API. A threshold is
identified by its full {record, expr, labels} triple, so changing --expr or a
label creates a new threshold rather than editing the existing one; use delete to
remove the old one. Use "thresholds update -f" to replace the whole config.`,
		Example: `  gcx kg thresholds upsert --record asserts:resource:rate:critical --expr 200e6 \
    --label asserts_resource_type=network:rx_bytes --label asserts_severity=critical`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			if err := opts.validate(); err != nil {
				return err
			}
			cfg, err := loader.LoadGrafanaConfig(cmd.Context())
			if err != nil {
				return err
			}
			client, err := NewClient(cfg)
			if err != nil {
				return err
			}
			if err := client.UpsertThreshold(cmd.Context(), opts.identity()); err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(),
				cmdio.NewSingleMutation("upserted", cmdio.MutationTarget{Kind: "Threshold", Name: opts.Record}))
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func newThresholdsDeleteCommand(loader RESTConfigLoader) *cobra.Command {
	opts := &guardedDeleteOpts{}
	var id thresholdIdentityFlags
	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete a single threshold.",
		Long: `Deletes one threshold over the v1 threshold config API. The backend has no
delete-by-name: pass the same --record, --expr and --label values the threshold
was created with (see "thresholds list").`,
		Example: `  gcx kg thresholds delete --record asserts:resource:rate:critical --expr 200e6 \
    --label asserts_resource_type=network:rx_bytes --label asserts_severity=critical`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := id.validate(); err != nil {
				return err
			}
			return runGuardedDelete(cmd, opts, loader, "Threshold", id.Record,
				fmt.Sprintf("Delete threshold %q (expr %s, labels %s)?", id.Record, id.Expr, formatThresholdLabels(id.Labels)),
				func(ctx context.Context, client *Client) error {
					return client.DeleteThreshold(ctx, id.identity())
				})
		},
	}
	opts.setup(cmd.Flags(), func(w io.Writer, m cmdio.SingleMutation) {
		cmdio.Success(w, "Threshold %q deleted", m.Target.Name)
	})
	id.bind(cmd.Flags())
	return cmd
}

type thresholdsUpdateOpts struct {
	IO    cmdio.Options
	File  string
	Force bool
}

func (o *thresholdsUpdateOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&o.File, "file", "f", "", "Input file (YAML), or '-' for stdin (required)")
	flags.BoolVar(&o.Force, "force", false, "Skip confirmation prompt")
	o.IO.RegisterCustomCodec("text", singleMutationText(func(w io.Writer, _ cmdio.SingleMutation) {
		cmdio.Success(w, "Threshold config replaced")
	}))
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
}

func newThresholdsUpdateCommand(loader RESTConfigLoader) *cobra.Command {
	opts := &thresholdsUpdateOpts{}
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Replace the whole threshold config from a YAML file.",
		Long: `Replaces the entire threshold configuration with the file's contents. Anything
not in the file is removed, so this is destructive and asks for confirmation
(skip with --force). The file may be the bare PrometheusRules YAML or the
"kind: Thresholds" document printed by "thresholds get -o yaml", which allows a
get, edit, update round trip.`,
		Example: `  gcx kg thresholds get -o yaml > thresholds.yaml
  # edit thresholds.yaml
  gcx kg thresholds update -f thresholds.yaml`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			data, err := readFileOrStdin(cmd, opts.File)
			if err != nil {
				return fmt.Errorf("failed to read file: %w", err)
			}
			body, err := thresholdsUpdateBody(data)
			if err != nil {
				return err
			}
			proceed, err := providers.ConfirmDestructive(cmd.InOrStdin(), cmd.ErrOrStderr(), opts.Force,
				"Replace the entire threshold config? Thresholds absent from the file will be removed.")
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}
			cfg, err := loader.LoadGrafanaConfig(cmd.Context())
			if err != nil {
				return err
			}
			client, err := NewClient(cfg)
			if err != nil {
				return err
			}
			if err := client.ReplaceThresholds(cmd.Context(), body); err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(),
				cmdio.NewSingleMutation("replaced", cmdio.MutationTarget{Kind: ThresholdsKind}))
		},
	}
	opts.setup(cmd.Flags())
	_ = cmd.MarkFlagRequired("file")
	return cmd
}

// formatThresholdLabels renders labels as sorted k=v pairs for the confirmation
// prompt, so thresholds sharing a record and expr can be told apart.
func formatThresholdLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return "none"
	}
	pairs := make([]string, 0, len(labels))
	for k, v := range labels {
		pairs = append(pairs, k+"="+v)
	}
	sort.Strings(pairs)
	return strings.Join(pairs, ",")
}

// thresholdsUpdateBody returns the PrometheusRules YAML to upload. A "kind:
// Thresholds" envelope (the shape `thresholds get -o yaml` prints) is unwrapped
// to its spec; any other kind is rejected so a prom-rules or other document is
// never uploaded as the threshold config. A bare document must define groups,
// so an empty or mistyped file cannot silently wipe the config.
func thresholdsUpdateBody(data []byte) (string, error) {
	var doc map[string]any
	if err := k8syaml.Unmarshal(data, &doc); err != nil {
		return "", fmt.Errorf("failed to parse thresholds file: %w", err)
	}
	if rawKind, present := doc["kind"]; present {
		kind, ok := rawKind.(string)
		if !ok || kind != ThresholdsKind {
			return "", fmt.Errorf("file has kind %v: thresholds update accepts kind %q or a bare PrometheusRules document", rawKind, ThresholdsKind)
		}
		spec, ok := doc["spec"].(map[string]any)
		if !ok {
			return "", errors.New("thresholds file has kind Thresholds but no spec")
		}
		doc = spec
	}
	if groups, ok := doc["groups"].([]any); !ok || len(groups) == 0 {
		return "", errors.New("thresholds file defines no groups; refusing to replace the config with an empty document")
	}
	out, err := k8syaml.Marshal(doc)
	if err != nil {
		return "", fmt.Errorf("failed to render thresholds file: %w", err)
	}
	return string(out), nil
}
