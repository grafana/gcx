package experiments

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/grafana/gcx/internal/agent"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/util/validation"
)

type updateOpts struct {
	IO          cmdio.Options
	File        string
	Title       string
	Description string
	Status      string
}

func (o *updateOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&o.File, "filename", "f", "", "Updated complete Experiment YAML or JSON manifest (use - for stdin)")
	flags.StringVar(&o.Title, "title", "", "New experiment title")
	flags.StringVar(&o.Description, "description", "", "New experiment description")
	flags.StringVar(&o.Status, "status", "", "New experiment lifecycle status")
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
}

func (o *updateOpts) Validate(flags *pflag.FlagSet) error {
	fieldsChanged := flags.Changed("title") || flags.Changed("description") || flags.Changed("status")
	if o.File != "" && fieldsChanged {
		return errors.New("--filename/-f cannot be combined with --title, --description, or --status")
	}
	if o.File == "" && !fieldsChanged {
		return errors.New("give --title, --description, --status, or --filename/-f")
	}
	if flags.Changed("title") && strings.TrimSpace(o.Title) == "" {
		return errors.New("--title must not be empty")
	}
	if flags.Changed("status") && strings.TrimSpace(o.Status) == "" {
		return errors.New("--status must not be empty")
	}
	return o.IO.Validate()
}

func newUpdateCommand(loader grafanaConfigLoader) *cobra.Command {
	opts := &updateOpts{}
	cmd := &cobra.Command{
		Use:   "update <name>",
		Short: "[experimental] Update an Odin experiment.",
		Long: `This command is experimental. It may be removed, or its subcommands, flags and responses may change without following the normal semantic versioning conventions.

Update common fields with --title, --description, or --status. The command
fetches the current experiment, preserves its other fields, and sends the
current resourceVersion. For larger changes, pass the complete resource with
-f after running experiments get. Odin rejects a stale resourceVersion instead
of overwriting a concurrent edit.`,
		Example: `  gcx experiments update checkout-conversion --title "New title"
  gcx experiments update checkout-conversion --description "Measure checkout conversion" --status draft
  gcx experiments get checkout-conversion -o yaml > experiment.yaml
  gcx experiments update checkout-conversion -f experiment.yaml
  gcx experiments update checkout-conversion -f - < experiment.json`,
		Args: cobra.ExactArgs(1),
		Annotations: map[string]string{
			agent.AnnotationStability: agent.StabilityExperimental,
			agent.AnnotationTokenCost: "medium",
			agent.AnnotationLLMHint:   "Use --title, --description, or --status for common changes; gcx fetches the current Experiment and preserves other fields. For analytics or other changes, get the full resource and submit it with -f FILE or -f -. A stale resourceVersion fails with HTTP 409.",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(cmd.Flags()); err != nil {
				return err
			}
			name := args[0]
			if problems := validation.IsDNS1123Subdomain(name); len(problems) > 0 {
				return fmt.Errorf("invalid experiment name %q: %s; use a name such as checkout-conversion", name, strings.Join(problems, "; "))
			}
			var manifest []byte
			if opts.File != "" {
				var err error
				manifest, err = readUpdateManifest(opts.File, cmd.InOrStdin(), name)
				if err != nil {
					return err
				}
			}
			cfg, err := loader.LoadGrafanaConfig(cmd.Context())
			if err != nil {
				return err
			}
			client, err := NewClient(cfg)
			if err != nil {
				return err
			}
			if opts.File == "" {
				current, err := client.Get(cmd.Context(), name)
				if err != nil {
					return err
				}
				changed, err := opts.applyFields(cmd.Flags(), current)
				if err != nil {
					return err
				}
				if !changed {
					return opts.IO.Encode(cmd.OutOrStdout(), current)
				}
				manifest, err = json.Marshal(current)
				if err != nil {
					return fmt.Errorf("encode updated experiment %q: %w", name, err)
				}
			}
			updated, err := client.Update(cmd.Context(), name, manifest)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), updated)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func (o *updateOpts) applyFields(flags *pflag.FlagSet, current Experiment) (bool, error) {
	spec, ok := current["spec"].(map[string]any)
	if !ok {
		return false, errors.New("stored experiment has no spec")
	}
	changed := false
	for _, field := range []struct {
		flag  string
		value string
	}{
		{flag: "title", value: o.Title},
		{flag: "description", value: o.Description},
		{flag: "status", value: o.Status},
	} {
		if flags.Changed(field.flag) && spec[field.flag] != field.value {
			spec[field.flag] = field.value
			changed = true
		}
	}
	return changed, nil
}
