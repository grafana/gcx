package kg

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"gopkg.in/yaml.v3"
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
	if strings.TrimSpace(f.Record) == "" {
		return errors.New("--record is required")
	}
	if strings.TrimSpace(f.Expr) == "" {
		return errors.New("--expr is required")
	}
	return nil
}

func (f *thresholdIdentityFlags) identity() ThresholdIdentity {
	return ThresholdIdentity{Record: f.Record, Expr: f.Expr, Labels: f.Labels}
}

// confirmThresholdWrite gates a destructive threshold write behind the shared
// confirmation guard. A declined prompt returns an ExitCancelled error, per
// docs/design/exit-codes.md §2.4, so scripts can tell a decline from a success.
func confirmThresholdWrite(cmd *cobra.Command, force bool, prompt string) error {
	proceed, err := providers.ConfirmDestructive(cmd.InOrStdin(), cmd.ErrOrStderr(), force, prompt)
	if err != nil {
		return err
	}
	if !proceed {
		exitCancelled := gcxerrors.ExitCancelled
		return &gcxerrors.DetailedError{
			Summary:  "operation cancelled by user",
			Details:  "Confirmation prompt was declined; the threshold config was not modified.",
			ExitCode: &exitCancelled,
		}
	}
	return nil
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
		Long: `Creates or updates one threshold over the v1 threshold config API. gcx sends the
full {record, expr, labels} triple; the backend matches an existing threshold on
record and labels, plus expr when the expr is not a plain number. Upserting a
numeric --expr therefore updates the matching threshold in place, while changing a
label (or a PromQL --expr) creates a new one that you may want to follow with a
delete of the old one. Use "thresholds update -f" to replace the whole config.`,
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
delete-by-name: pass the --record, --expr and --label values shown by "thresholds
list". The backend matches on record and labels, plus expr when the expr is not a
plain number.`,
		Example: `  gcx kg thresholds delete --record asserts:resource:rate:critical --expr 200e6 \
    --label asserts_resource_type=network:rx_bytes --label asserts_severity=critical`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			if err := id.validate(); err != nil {
				return err
			}
			prompt := fmt.Sprintf("Delete threshold %q (expr %s, labels %s)?", id.Record, id.Expr, renderLabels(id.Labels))
			if err := confirmThresholdWrite(cmd, opts.Force, prompt); err != nil {
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
			if err := client.DeleteThreshold(cmd.Context(), id.identity()); err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(),
				cmdio.NewSingleMutation("deleted", cmdio.MutationTarget{Kind: "Threshold", Name: id.Record}))
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
	flags.StringVarP(&o.File, "file", "f", "", "Input file (YAML), or '-' for stdin together with --force or GCX_AUTO_APPROVE (required)")
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
(skip with --force or GCX_AUTO_APPROVE). The file may be the bare PrometheusRules YAML or the
"kind: Thresholds" document printed by "thresholds get -o yaml", which allows a
get, edit, update round trip. The file is decoded strictly: unknown or misspelled
keys, a second YAML document, and numeric values where a string is expected
(quote them, e.g. expr: "200e6") are rejected before anything is sent.`,
		Example: `  gcx kg thresholds get -o yaml > thresholds.yaml
  # edit thresholds.yaml
  gcx kg thresholds update -f thresholds.yaml`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			if opts.File == "-" {
				bypass, err := providers.CheckDestructiveBypass(opts.Force)
				if err != nil {
					return err
				}
				if !bypass {
					return errors.New("-f - reads the whole of stdin, leaving nothing for the confirmation prompt: pass --force or set GCX_AUTO_APPROVE, or use a file path")
				}
			}
			data, err := readFileOrStdin(cmd, opts.File)
			if err != nil {
				return fmt.Errorf("failed to read file: %w", err)
			}
			body, err := thresholdsUpdateBody(data)
			if err != nil {
				return err
			}
			if err := confirmThresholdWrite(cmd, opts.Force,
				"Replace the entire threshold config? Thresholds absent from the file will be removed."); err != nil {
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

// thresholdsEnvelope is the resource document `thresholds get -o yaml` prints.
type thresholdsEnvelope struct {
	APIVersion string          `json:"apiVersion"`
	Kind       string          `json:"kind"`
	Metadata   json.RawMessage `json:"metadata"`
	Spec       json.RawMessage `json:"spec"`
}

// thresholdsUpdateBody validates the file and returns the PrometheusRules YAML to
// upload. Decoding is strict, because this body replaces the whole config and a
// silently dropped key or document would delete thresholds:
//   - a second YAML document is rejected, not ignored;
//   - unknown or misspelled keys are rejected;
//   - a number where a string is expected (an unquoted expr: 200e6) is rejected
//     rather than rewritten;
//   - a "kind: Thresholds" envelope is unwrapped to its spec, any other kind is
//     rejected so a prom-rules document is never uploaded as thresholds;
//   - the config must define groups, so an empty or mistyped file cannot wipe it.
//
// A bare document is sent exactly as written, so the bytes uploaded are the bytes
// the user reviewed before confirming.
func thresholdsUpdateBody(data []byte) (string, error) {
	if err := rejectExtraYAMLDocuments(data); err != nil {
		return "", err
	}
	jsonData, err := k8syaml.YAMLToJSONStrict(data)
	if err != nil {
		return "", fmt.Errorf("failed to parse thresholds file: %w", err)
	}

	var probe map[string]json.RawMessage
	if err := json.Unmarshal(jsonData, &probe); err != nil {
		return "", errors.New("thresholds file must be a YAML mapping")
	}

	ruleJSON := jsonData
	_, enveloped := probe["kind"]
	if enveloped {
		var env thresholdsEnvelope
		if err := decodeStrictJSON(jsonData, &env); err != nil {
			return "", fmt.Errorf("invalid thresholds document: %w", err)
		}
		if env.Kind != ThresholdsKind {
			return "", fmt.Errorf("file has kind %q: thresholds update accepts kind %q or a bare PrometheusRules document", env.Kind, ThresholdsKind)
		}
		if len(env.Spec) == 0 {
			return "", errors.New("thresholds file has kind Thresholds but no spec")
		}
		ruleJSON = env.Spec
	}

	var rule Rule
	if err := decodeStrictJSON(ruleJSON, &rule); err != nil {
		return "", fmt.Errorf("invalid thresholds config: %w", err)
	}
	if len(rule.Groups) == 0 {
		return "", errors.New("thresholds file defines no groups; refusing to replace the config with an empty document")
	}

	if !enveloped {
		return string(data), nil
	}
	out, err := k8syaml.Marshal(rule)
	if err != nil {
		return "", fmt.Errorf("failed to render thresholds file: %w", err)
	}
	return string(out), nil
}

func decodeStrictJSON(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// rejectExtraYAMLDocuments fails when the input holds more than one non-empty
// YAML document. yaml.v3 decodes a trailing "---" as an empty null document,
// which is allowed.
func rejectExtraYAMLDocuments(data []byte) error {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var first yaml.Node
	if err := dec.Decode(&first); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("failed to parse thresholds file: %w", err)
	}
	for {
		var extra yaml.Node
		err := dec.Decode(&extra)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("failed to parse thresholds file: %w", err)
		}
		if len(extra.Content) > 0 && extra.Content[0].Tag != "!!null" {
			return errors.New("multiple YAML documents are not supported: put all thresholds in one document, or every threshold after the first would be deleted")
		}
	}
}
