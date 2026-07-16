package guards

import (
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
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/aio11y/aio11yhttp"
	"github.com/grafana/gcx/internal/providers/aio11y/eval"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Commands returns the guards command group.
func Commands() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "guards",
		Short: "Manage synchronous policy guards (hook rules) that evaluate generations on the request path.",
		Long: `Guards (hook rules) are synchronous policies evaluated before or after each generation.
They can deny, warn, or transform a generation based on evaluators, regex patterns, or blocked tool names.

Unlike eval rules (gcx agento11y rules), guards run inline on the request path and short-circuit by default.`,
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
	IO    cmdio.Options
	Limit int64
}

func (o *listOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &TableCodec{})
	o.IO.RegisterCustomCodec("wide", &TableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.Int64Var(&o.Limit, "limit", 50, "Maximum number of hook rules to return (0 for no limit)")
}

func newListCommand() *cobra.Command {
	opts := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List hook rules (guards).",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			crud, namespace, err := NewTypedCRUD(ctx)
			if err != nil {
				return err
			}

			typedObjs, err := crud.List(ctx, opts.Limit)
			if err != nil {
				return err
			}

			specs := make([]eval.HookRuleDefinition, len(typedObjs))
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
		Short: "Get a single hook rule (guard).",
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
	flags.StringVarP(&o.File, "filename", "f", "", "File containing the hook rule definition (use - for stdin)")
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
		Short: "Create a hook rule (guard) from a file.",
		Example: `  # Create a guard from a YAML file.
  gcx agento11y guards create -f guard.yaml

  # Create from stdin.
  gcx agento11y guards create -f -

  # Create and output as YAML.
  gcx agento11y guards create -f guard.json -o yaml`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			rule, err := ReadHookRuleFile(opts.File, cmd.InOrStdin())
			if err != nil {
				return err
			}

			ctx := cmd.Context()
			crud, _, err := NewTypedCRUD(ctx)
			if err != nil {
				return err
			}

			typedObj := &adapter.TypedObject[eval.HookRuleDefinition]{Spec: *rule}
			created, err := crud.Create(ctx, typedObj)
			if err != nil {
				return err
			}

			cmdio.Success(cmd.ErrOrStderr(), "Guard %s created", created.Spec.RuleID)
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
	flags.StringVarP(&o.File, "filename", "f", "", "File containing the full hook rule definition (use - for stdin)")
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
		Short: "Update a hook rule (guard) from a file. Full replace; omitted fields reset to defaults.",
		Example: `  # Update a guard from a YAML file.
  gcx agento11y guards update my-guard -f guard.yaml`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			rule, err := ReadHookRuleFile(opts.File, cmd.InOrStdin())
			if err != nil {
				return err
			}

			ctx := cmd.Context()
			crud, _, err := NewTypedCRUD(ctx)
			if err != nil {
				return err
			}

			typedObj := &adapter.TypedObject[eval.HookRuleDefinition]{Spec: *rule}
			updated, err := crud.Update(ctx, args[0], typedObj)
			if err != nil {
				return err
			}

			cmdio.Success(cmd.ErrOrStderr(), "Guard %s updated", updated.Spec.RuleID)
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
	flags.BoolVar(&o.Force, "force", false, "Skip confirmation prompt")
}

func newDeleteCommand() *cobra.Command {
	opts := &deleteOpts{}
	cmd := &cobra.Command{
		Use:   "delete ID...",
		Short: "Delete hook rules (guards).",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			proceed, err := providers.ConfirmDestructive(cmd.InOrStdin(), cmd.ErrOrStderr(), opts.Force,
				fmt.Sprintf("Delete %d guard(s)?", len(args)))
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			ctx := cmd.Context()
			crud, _, err := NewTypedCRUD(ctx)
			if err != nil {
				return err
			}

			for _, id := range args {
				if err := crud.Delete(ctx, id); err != nil {
					return fmt.Errorf("deleting hook rule %s: %w", id, err)
				}
				cmdio.Success(cmd.ErrOrStderr(), "Deleted guard %s", id)
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

func ReadHookRuleFile(path string, stdin io.Reader) (*eval.HookRuleDefinition, error) {
	data, err := ReadFile(path, stdin)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	var def eval.HookRuleDefinition
	if err := json.Unmarshal(data, &def); err != nil {
		var yamlDef eval.HookRuleDefinition
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
	rules, ok := v.([]eval.HookRuleDefinition)
	if !ok {
		return errors.New("invalid data type for table codec: expected []HookRuleDefinition")
	}

	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable("ID", "ENABLED", "PHASE", "PRIORITY", "SELECTOR", "ACTION", "EVALUATORS", "TRANSFORM", "TOOL FILTER", "CREATED BY", "CREATED AT")
	} else {
		t = style.NewTable("ID", "ENABLED", "PHASE", "PRIORITY", "SELECTOR", "ACTION")
	}

	for _, r := range rules {
		c.appendRow(t, r)
	}
	return t.Render(w)
}

func (c *TableCodec) appendRow(t *style.TableBuilder, r eval.HookRuleDefinition) {
	enabled := "no"
	if r.Enabled {
		enabled = "yes"
	}
	priority := strconv.Itoa(r.Priority)

	if !c.Wide {
		t.Row(r.RuleID, enabled, r.Phase, priority, r.Selector, r.ActionOnFail)
		return
	}

	evalIDs := strings.Join(r.EvaluatorIDs, ", ")
	if evalIDs == "" {
		evalIDs = "-"
	}
	transform := "no"
	if r.Transform != nil && len(r.Transform.Patterns) > 0 {
		transform = "yes"
	}
	toolFilter := "no"
	if r.ToolFilter != nil && len(r.ToolFilter.BlockedNames) > 0 {
		toolFilter = "yes"
	}
	createdBy := r.CreatedBy
	if createdBy == "" {
		createdBy = "-"
	}
	t.Row(r.RuleID, enabled, r.Phase, priority, r.Selector, r.ActionOnFail, evalIDs, transform, toolFilter, createdBy, aio11yhttp.FormatTime(r.CreatedAt))
}

func (c *TableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}
