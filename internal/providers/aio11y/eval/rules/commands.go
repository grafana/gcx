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

	"github.com/goccy/go-yaml"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers/aio11y/aio11yhttp"
	"github.com/grafana/gcx/internal/providers/aio11y/eval"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/style"
	"github.com/grafana/gcx/internal/terminal"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Commands returns the rules command group.
func Commands() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rules",
		Short: "Manage rules that route generations to evaluators.",
	}
	cmd.AddCommand(
		newListCommand(),
		newGetCommand(),
		newCreateCommand(),
		newUpdateCommand(),
		newDeleteCommand(),
	)
	return cmd
}

// --- list ---

type listOpts struct {
	IO cmdio.Options
}

func (o *listOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &TableCodec{})
	o.IO.RegisterCustomCodec("wide", &TableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func newListCommand() *cobra.Command {
	opts := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List evaluation rules.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			crud, namespace, err := NewTypedCRUD(ctx)
			if err != nil {
				return err
			}

			typedObjs, err := crud.List(ctx, 0)
			if err != nil {
				return err
			}

			specs := make([]eval.RuleDefinition, len(typedObjs))
			for i := range typedObjs {
				specs[i] = typedObjs[i].Spec
			}

			if opts.IO.OutputFormat == "table" || opts.IO.OutputFormat == "wide" {
				return opts.IO.Encode(cmd.OutOrStdout(), specs)
			}

			objs := make([]unstructured.Unstructured, 0, len(specs))
			for _, spec := range specs {
				u, err := specToUnstructured(spec, namespace)
				if err != nil {
					return err
				}
				objs = append(objs, u)
			}
			return opts.IO.Encode(cmd.OutOrStdout(), objs)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- get ---

type getOpts struct {
	IO cmdio.Options
}

func (o *getOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
}

func newGetCommand() *cobra.Command {
	opts := &getOpts{}
	cmd := &cobra.Command{
		Use:   "get <rule-id>",
		Short: "Get a single evaluation rule.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			crud, namespace, err := NewTypedCRUD(ctx)
			if err != nil {
				return err
			}

			typedObj, err := crud.Get(ctx, args[0])
			if err != nil {
				return err
			}

			u, err := specToUnstructured(typedObj.Spec, namespace)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), &u)
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

func newCreateCommand() *cobra.Command {
	opts := &createOpts{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an evaluation rule from a file.",
		Example: `  # Create a rule from a YAML file.
  gcx aio11y rules create -f rule.yaml

  # Create from stdin.
  gcx aio11y rules create -f -

  # Create and output as YAML.
  gcx aio11y rules create -f rule.json -o yaml`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			rule, err := ReadRuleFile(opts.File, cmd.InOrStdin())
			if err != nil {
				return err
			}

			ctx := cmd.Context()
			crud, _, err := NewTypedCRUD(ctx)
			if err != nil {
				return err
			}

			typedObj := &adapter.TypedObject[eval.RuleDefinition]{Spec: *rule}
			created, err := crud.Create(ctx, typedObj)
			if err != nil {
				return err
			}

			cmdio.Success(cmd.ErrOrStderr(), "Rule %s created", created.Spec.RuleID)
			return opts.IO.Encode(cmd.OutOrStdout(), created.Spec)
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
	flags.StringVarP(&o.File, "filename", "f", "", "File containing the full rule definition (use - for stdin)")
	o.IO.DefaultFormat("json")
	o.IO.BindFlags(flags)
}

func (o *updateOpts) Validate() error {
	if o.File == "" {
		return errors.New("--filename/-f is required")
	}
	return o.IO.Validate()
}

func newUpdateCommand() *cobra.Command {
	opts := &updateOpts{}
	cmd := &cobra.Command{
		Use:   "update <rule-id>",
		Short: "Update an evaluation rule from a file.",
		Example: `  # Update a rule from a YAML file.
  gcx aio11y rules update my-rule -f rule.yaml`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			rule, err := ReadRuleFile(opts.File, cmd.InOrStdin())
			if err != nil {
				return err
			}

			ctx := cmd.Context()
			crud, _, err := NewTypedCRUD(ctx)
			if err != nil {
				return err
			}

			typedObj := &adapter.TypedObject[eval.RuleDefinition]{Spec: *rule}
			updated, err := crud.Update(ctx, args[0], typedObj)
			if err != nil {
				return err
			}

			cmdio.Success(cmd.ErrOrStderr(), "Rule %s updated", updated.Spec.RuleID)
			return opts.IO.Encode(cmd.OutOrStdout(), updated.Spec)
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

func newDeleteCommand() *cobra.Command {
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

			ctx := cmd.Context()
			crud, _, err := NewTypedCRUD(ctx)
			if err != nil {
				return err
			}

			for _, id := range args {
				if err := crud.Delete(ctx, id); err != nil {
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

	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable("ID", "ENABLED", "SELECTOR", "SAMPLE RATE", "EVALUATORS", "CREATED BY", "CREATED AT")
	} else {
		t = style.NewTable("ID", "ENABLED", "SELECTOR", "SAMPLE RATE", "EVALUATORS")
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
			t.Row(r.RuleID, enabled, r.Selector, sampleRate, evalIDs, createdBy, aio11yhttp.FormatTime(r.CreatedAt))
		} else {
			t.Row(r.RuleID, enabled, r.Selector, sampleRate, evalIDs)
		}
	}
	return t.Render(w)
}

func (c *TableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}
