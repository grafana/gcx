package rules

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/goccy/go-yaml"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/sigil/commandutil"
	"github.com/grafana/gcx/internal/providers/sigil/eval"
	"github.com/grafana/gcx/internal/providers/sigil/sigilhttp"
	"github.com/grafana/gcx/internal/terminal"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func newClient(cmd *cobra.Command, loader *providers.ConfigLoader) (*Client, error) {
	base, err := sigilhttp.NewClientFromCommand(cmd, loader)
	if err != nil {
		return nil, err
	}
	return NewClient(base), nil
}

// Commands returns the rules command group.
func Commands(loader *providers.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rules",
		Short: "Manage rules that route generations to evaluators.",
	}
	cmd.AddCommand(
		newShowCommand(loader),
		newCreateCommand(loader),
		newUpdateCommand(loader),
		newDeleteCommand(loader),
	)
	return cmd
}

// --- show (list + get) ---

type showOpts struct {
	IO cmdio.Options
}

func (o *showOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &TableCodec{})
	o.IO.RegisterCustomCodec("wide", &TableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func newShowCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &showOpts{}
	cmd := &cobra.Command{
		Use:   "show [rule-id]",
		Short: "Show evaluation rules or a single rule detail.",
		Long: `Show evaluation rules. Without an ID, lists all rules.
With an ID, shows the full rule definition.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}

			if len(args) == 1 {
				if commandutil.ShouldDefaultDetailToYAML(cmd) {
					opts.IO.OutputFormat = "yaml"
				}
				if err := commandutil.ValidateDetailOutputFormat(cmd, opts.IO.OutputFormat, "rule", args[0]); err != nil {
					return err
				}
				rule, err := client.Get(cmd.Context(), args[0])
				if err != nil {
					return err
				}
				return opts.IO.Encode(cmd.OutOrStdout(), rule)
			}

			rules, err := client.List(cmd.Context())
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), rules)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- create ---

type createOpts struct {
	File string
	IO   cmdio.Options
}

func (o *createOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&o.File, "filename", "f", "", "File containing the rule definition (use - for stdin)")
	o.IO.DefaultFormat("json")
	o.IO.BindFlags(flags)
}

func (o *createOpts) Validate() error {
	if o.File == "" {
		return errors.New("--filename/-f is required")
	}
	return o.IO.Validate()
}

func newCreateCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &createOpts{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an evaluation rule from a file.",
		Example: `  # Create a rule from a YAML file.
  gcx sigil rules create -f rule.yaml

  # Create from stdin.
  gcx sigil rules create -f -

  # Create and output as YAML.
  gcx sigil rules create -f rule.json -o yaml`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			rule, err := ReadRuleFile(opts.File, cmd.InOrStdin())
			if err != nil {
				return err
			}

			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}

			created, err := client.Create(cmd.Context(), rule)
			if err != nil {
				return err
			}

			cmdio.Success(cmd.ErrOrStderr(), "Rule %s created", created.RuleID)
			return opts.IO.Encode(cmd.OutOrStdout(), created)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- update ---

type updateOpts struct {
	File string
	IO   cmdio.Options
}

func (o *updateOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&o.File, "filename", "f", "", "File containing the rule fields to update (use - for stdin)")
	o.IO.DefaultFormat("json")
	o.IO.BindFlags(flags)
}

func (o *updateOpts) Validate() error {
	if o.File == "" {
		return errors.New("--filename/-f is required")
	}
	return o.IO.Validate()
}

func newUpdateCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &updateOpts{}
	cmd := &cobra.Command{
		Use:   "update <rule-id>",
		Short: "Update an evaluation rule from a file.",
		Long: `Update an evaluation rule by patching it with fields from a JSON or YAML file.
Only the fields present in the file are updated; omitted fields are left unchanged.`,
		Example: `  # Update a rule's sample rate and evaluators.
  gcx sigil rules update my-rule -f patch.yaml`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			data, err := ReadFile(opts.File, cmd.InOrStdin())
			if err != nil {
				return err
			}

			patchJSON, err := ToJSON(data)
			if err != nil {
				return err
			}

			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}

			updated, err := client.Update(cmd.Context(), args[0], patchJSON)
			if err != nil {
				return err
			}

			cmdio.Success(cmd.ErrOrStderr(), "Rule %s updated", updated.RuleID)
			return opts.IO.Encode(cmd.OutOrStdout(), updated)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- delete ---

type deleteOpts struct {
	Force bool
}

func (o *deleteOpts) setup(flags *pflag.FlagSet) {
	flags.BoolVarP(&o.Force, "force", "f", false, "Skip confirmation prompt")
}

func newDeleteCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &deleteOpts{}
	cmd := &cobra.Command{
		Use:   "delete ID...",
		Short: "Delete evaluation rules.",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !opts.Force {
				if terminal.IsPiped() {
					return errors.New("stdin is not a terminal, use --force to skip confirmation")
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "Delete %d rule(s)? [y/N] ", len(args))
				reader := bufio.NewReader(cmd.InOrStdin())
				answer, err := reader.ReadString('\n')
				if err != nil {
					return fmt.Errorf("reading confirmation: %w", err)
				}
				answer = strings.TrimSpace(strings.ToLower(answer))
				if answer != "y" && answer != "yes" {
					cmdio.Info(cmd.ErrOrStderr(), "Aborted.")
					return nil
				}
			}

			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}

			for _, id := range args {
				if err := client.Delete(cmd.Context(), id); err != nil {
					return fmt.Errorf("deleting rule %s: %w", id, err)
				}
				cmdio.Success(cmd.ErrOrStderr(), "Deleted rule %s", id)
			}
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func ReadFile(path string, stdin io.Reader) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(stdin)
	}
	return os.ReadFile(path)
}

func ReadRuleFile(path string, stdin io.Reader) (*eval.RuleDefinition, error) {
	data, err := ReadFile(path, stdin)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	var def eval.RuleDefinition
	if err := json.Unmarshal(data, &def); err != nil {
		var yamlDef eval.RuleDefinition
		if yamlErr := yaml.Unmarshal(data, &yamlDef); yamlErr != nil {
			return nil, fmt.Errorf("parsing %s: %w", path, yamlErr)
		}
		return &yamlDef, nil
	}
	return &def, nil
}

// ToJSON converts JSON or YAML input to a JSON object for PATCH requests.
// Rejects non-object input (arrays, scalars) because map[string]any only accepts mappings.
func ToJSON(data []byte) ([]byte, error) {
	var obj map[string]any
	if err := yaml.Unmarshal(data, &obj); err != nil {
		return nil, fmt.Errorf("parsing input: %w", err)
	}
	return json.Marshal(obj)
}

// --- table codec ---

type TableCodec struct {
	Wide bool
}

func (c *TableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *TableCodec) Encode(w io.Writer, v any) error {
	rules, ok := v.([]eval.RuleDefinition)
	if !ok {
		return errors.New("invalid data type for table codec: expected []RuleDefinition")
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if c.Wide {
		fmt.Fprintln(tw, "ID\tENABLED\tSELECTOR\tSAMPLE RATE\tEVALUATORS\tCREATED BY\tCREATED AT")
	} else {
		fmt.Fprintln(tw, "ID\tENABLED\tSELECTOR\tSAMPLE RATE\tEVALUATORS")
	}

	for _, r := range rules {
		enabled := "no"
		if r.Enabled {
			enabled = "yes"
		}
		evalIDs := strings.Join(r.EvaluatorIDs, ", ")
		if evalIDs == "" {
			evalIDs = "-"
		}
		sampleRate := strconv.FormatFloat(r.SampleRate, 'f', -1, 64)

		if c.Wide {
			createdBy := r.CreatedBy
			if createdBy == "" {
				createdBy = "-"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				r.RuleID, enabled, r.Selector, sampleRate, evalIDs, createdBy, sigilhttp.FormatTime(r.CreatedAt))
		} else {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
				r.RuleID, enabled, r.Selector, sampleRate, evalIDs)
		}
	}
	return tw.Flush()
}

func (c *TableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}
