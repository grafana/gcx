package guards

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers/aio11y/aio11yhttp"
	"github.com/grafana/gcx/internal/providers/aio11y/eval"
	"github.com/grafana/gcx/internal/providers/crudcmd"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Commands returns the guards command group.
func Commands() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "guards",
		Short: "Manage synchronous policy guards (hook rules) that evaluate generations on the request path.",
		Long: `Guards (hook rules) are synchronous policies evaluated before or after each generation.
They can deny, warn, or transform a generation based on evaluators, regex patterns, or blocked tool names.

Unlike eval rules (gcx aio11y rules), guards run inline on the request path and short-circuit by default.`,
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

func newListCommand() *cobra.Command {
	return crudcmd.NewTypedListCommand(crudcmd.TypedListConfig[eval.HookRuleDefinition]{
		Use:          "list",
		Short:        "List hook rules (guards).",
		DefaultFmt:   "table",
		LimitDefault: 50,
		LimitUsage:   "Maximum number of hook rules to return (0 for no limit)",
		Codecs:       []format.Codec{&TableCodec{}, &TableCodec{Wide: true}},
		Noun:         "hook rule",
		NewCRUD:      NewTypedCRUD,
		ToResource: func(crud *adapter.TypedCRUD[eval.HookRuleDefinition], item eval.HookRuleDefinition) (unstructured.Unstructured, error) {
			return specToUnstructured(item, crud.Namespace)
		},
	})
}

// --- get ---

func newGetCommand() *cobra.Command {
	return crudcmd.NewGetCommand(crudcmd.GetConfig[*unstructured.Unstructured]{
		Use:        "get <rule-id>",
		Short:      "Get a single hook rule (guard).",
		Args:       cobra.ExactArgs(1),
		DefaultFmt: "yaml",
		Fetch: func(ctx context.Context, args []string) (*unstructured.Unstructured, error) {
			crud, namespace, err := NewTypedCRUD(ctx)
			if err != nil {
				return nil, err
			}
			typedObj, err := crud.Get(ctx, args[0])
			if err != nil {
				return nil, err
			}
			u, err := specToUnstructured(typedObj.Spec, namespace)
			if err != nil {
				return nil, err
			}
			return &u, nil
		},
	})
}

// --- create ---

func newCreateCommand() *cobra.Command {
	return crudcmd.NewCreateCommand(crudcmd.CreateConfig[eval.HookRuleDefinition]{
		Use:   "create",
		Short: "Create a hook rule (guard) from a file.",
		Example: `  # Create a guard from a YAML file.
  gcx aio11y guards create -f guard.yaml

  # Create from stdin.
  gcx aio11y guards create -f -

  # Create and output as YAML.
  gcx aio11y guards create -f guard.json -o yaml`,
		DefaultFmt:    "json",
		FilenameUsage: "File containing the hook rule definition (use - for stdin)",
		Read:          ReadHookRuleFile,
		Create: func(ctx context.Context, rule eval.HookRuleDefinition) (eval.HookRuleDefinition, error) {
			crud, _, err := NewTypedCRUD(ctx)
			if err != nil {
				return eval.HookRuleDefinition{}, err
			}
			created, err := crud.Create(ctx, &adapter.TypedObject[eval.HookRuleDefinition]{Spec: rule})
			if err != nil {
				return eval.HookRuleDefinition{}, err
			}
			return created.Spec, nil
		},
		OnSuccess: func(cmd *cobra.Command, created eval.HookRuleDefinition) {
			cmdio.Success(cmd.ErrOrStderr(), "Guard %s created", created.RuleID)
		},
	})
}

// --- update ---

func newUpdateCommand() *cobra.Command {
	return crudcmd.NewUpdateCommand(crudcmd.UpdateConfig[eval.HookRuleDefinition]{
		Use:   "update <rule-id>",
		Short: "Update a hook rule (guard) from a file. Full replace; omitted fields reset to defaults.",
		Example: `  # Update a guard from a YAML file.
  gcx aio11y guards update my-guard -f guard.yaml`,
		Args:          cobra.ExactArgs(1),
		DefaultFmt:    "json",
		FilenameUsage: "File containing the full hook rule definition (use - for stdin)",
		Read:          ReadHookRuleFile,
		Update: func(ctx context.Context, id string, rule eval.HookRuleDefinition) (eval.HookRuleDefinition, error) {
			crud, _, err := NewTypedCRUD(ctx)
			if err != nil {
				return eval.HookRuleDefinition{}, err
			}
			updated, err := crud.Update(ctx, id, &adapter.TypedObject[eval.HookRuleDefinition]{Spec: rule})
			if err != nil {
				return eval.HookRuleDefinition{}, err
			}
			return updated.Spec, nil
		},
		OnSuccess: func(cmd *cobra.Command, updated eval.HookRuleDefinition) {
			cmdio.Success(cmd.ErrOrStderr(), "Guard %s updated", updated.RuleID)
		},
	})
}

// --- delete ---

func newDeleteCommand() *cobra.Command {
	return crudcmd.NewDeleteCommand(crudcmd.DeleteConfig{
		Use:   "delete ID...",
		Short: "Delete hook rules (guards).",
		Args:  cobra.MinimumNArgs(1),
		Out:   func(cmd *cobra.Command) io.Writer { return cmd.ErrOrStderr() },
		Confirm: func(args []string) string {
			return fmt.Sprintf("Delete %d guard(s)?", len(args))
		},
		NewDelete: func(ctx context.Context) (func(string) error, error) {
			crud, _, err := NewTypedCRUD(ctx)
			if err != nil {
				return nil, err
			}
			return func(id string) error { return crud.Delete(ctx, id) }, nil
		},
		Success: func(id string) string { return "Deleted guard " + id },
	})
}

// ReadHookRuleFile reads a hook rule definition from path (or stdin, for
// "-"), trying JSON first and falling back to YAML.
func ReadHookRuleFile(path string, stdin io.Reader) (*eval.HookRuleDefinition, error) {
	return crudcmd.ReadJSONOrYAMLFile[eval.HookRuleDefinition](path, stdin)
}

// --- table codec ---

type TableCodec struct {
	Wide bool
}

func (c *TableCodec) Format() format.Format { return crudcmd.WideFormat(c.Wide) }

func (c *TableCodec) Encode(w io.Writer, v any) error {
	row := func(t *style.TableBuilder, r eval.HookRuleDefinition) { c.appendRow(t, r) }
	if c.Wide {
		return crudcmd.EncodeTable(w, v, "HookRuleDefinition",
			[]string{"ID", "ENABLED", "PHASE", "PRIORITY", "SELECTOR", "ACTION", "EVALUATORS", "TRANSFORM", "TOOL FILTER", "CREATED BY", "CREATED AT"}, row)
	}
	return crudcmd.EncodeTable(w, v, "HookRuleDefinition", []string{"ID", "ENABLED", "PHASE", "PRIORITY", "SELECTOR", "ACTION"}, row)
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
	return crudcmd.ErrTableDecode
}
