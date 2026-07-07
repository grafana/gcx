package rules

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

func newListCommand() *cobra.Command {
	return crudcmd.NewTypedListCommand(crudcmd.TypedListConfig[eval.RuleDefinition]{
		Use:          "list",
		Short:        "List evaluation rules.",
		DefaultFmt:   "table",
		LimitDefault: 50,
		LimitUsage:   "Maximum number of rules to return (0 for no limit)",
		Codecs:       []format.Codec{&TableCodec{}, &TableCodec{Wide: true}},
		Noun:         "rule",
		NewCRUD:      NewTypedCRUD,
		ToResource: func(crud *adapter.TypedCRUD[eval.RuleDefinition], item eval.RuleDefinition) (unstructured.Unstructured, error) {
			return specToUnstructured(item, crud.Namespace)
		},
	})
}

// --- get ---

func newGetCommand() *cobra.Command {
	return crudcmd.NewGetCommand(crudcmd.GetConfig[*unstructured.Unstructured]{
		Use:        "get <rule-id>",
		Short:      "Get a single evaluation rule.",
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
	return crudcmd.NewCreateCommand(crudcmd.CreateConfig[eval.RuleDefinition]{
		Use:   "create",
		Short: "Create an evaluation rule from a file.",
		Example: `  # Create a rule from a YAML file.
  gcx aio11y rules create -f rule.yaml

  # Create from stdin.
  gcx aio11y rules create -f -

  # Create and output as YAML.
  gcx aio11y rules create -f rule.json -o yaml`,
		DefaultFmt:    "json",
		FilenameUsage: "File containing the rule definition (use - for stdin)",
		Read:          ReadRuleFile,
		Create: func(ctx context.Context, rule eval.RuleDefinition) (eval.RuleDefinition, error) {
			crud, _, err := NewTypedCRUD(ctx)
			if err != nil {
				return eval.RuleDefinition{}, err
			}
			created, err := crud.Create(ctx, &adapter.TypedObject[eval.RuleDefinition]{Spec: rule})
			if err != nil {
				return eval.RuleDefinition{}, err
			}
			return created.Spec, nil
		},
		OnSuccess: func(cmd *cobra.Command, created eval.RuleDefinition) {
			cmdio.Success(cmd.ErrOrStderr(), "Rule %s created", created.RuleID)
		},
	})
}

// --- update ---

func newUpdateCommand() *cobra.Command {
	return crudcmd.NewUpdateCommand(crudcmd.UpdateConfig[eval.RuleDefinition]{
		Use:   "update <rule-id>",
		Short: "Update an evaluation rule from a file.",
		Example: `  # Update a rule from a YAML file.
  gcx aio11y rules update my-rule -f rule.yaml`,
		Args:          cobra.ExactArgs(1),
		DefaultFmt:    "json",
		FilenameUsage: "File containing the full rule definition (use - for stdin)",
		Read:          ReadRuleFile,
		Update: func(ctx context.Context, id string, rule eval.RuleDefinition) (eval.RuleDefinition, error) {
			crud, _, err := NewTypedCRUD(ctx)
			if err != nil {
				return eval.RuleDefinition{}, err
			}
			updated, err := crud.Update(ctx, id, &adapter.TypedObject[eval.RuleDefinition]{Spec: rule})
			if err != nil {
				return eval.RuleDefinition{}, err
			}
			return updated.Spec, nil
		},
		OnSuccess: func(cmd *cobra.Command, updated eval.RuleDefinition) {
			cmdio.Success(cmd.ErrOrStderr(), "Rule %s updated", updated.RuleID)
		},
	})
}

// --- delete ---

func newDeleteCommand() *cobra.Command {
	return crudcmd.NewDeleteCommand(crudcmd.DeleteConfig{
		Use:   "delete ID...",
		Short: "Delete evaluation rules.",
		Args:  cobra.MinimumNArgs(1),
		Out:   func(cmd *cobra.Command) io.Writer { return cmd.ErrOrStderr() },
		Confirm: func(args []string) string {
			return fmt.Sprintf("Delete %d rule(s)?", len(args))
		},
		NewDelete: func(ctx context.Context) (func(string) error, error) {
			crud, _, err := NewTypedCRUD(ctx)
			if err != nil {
				return nil, err
			}
			return func(id string) error { return crud.Delete(ctx, id) }, nil
		},
		Success: func(id string) string { return "Deleted rule " + id },
	})
}

// ReadRuleFile reads a rule definition from path (or stdin, for "-"), trying
// JSON first and falling back to YAML.
func ReadRuleFile(path string, stdin io.Reader) (*eval.RuleDefinition, error) {
	return crudcmd.ReadJSONOrYAMLFile[eval.RuleDefinition](path, stdin)
}

// --- table codec ---

type TableCodec struct {
	Wide bool
}

func (c *TableCodec) Format() format.Format { return crudcmd.WideFormat(c.Wide) }

func (c *TableCodec) Encode(w io.Writer, v any) error {
	row := func(t *style.TableBuilder, r eval.RuleDefinition) {
		enabled := "no"
		if r.Enabled {
			enabled = "yes"
		}
		evalIDs := strings.Join(r.EvaluatorIDs, ", ")
		if evalIDs == "" {
			evalIDs = "-"
		}
		sampleRate := strconv.FormatFloat(r.SampleRate, 'f', -1, 64)

		if !c.Wide {
			t.Row(r.RuleID, enabled, r.Selector, sampleRate, evalIDs)
			return
		}

		createdBy := r.CreatedBy
		if createdBy == "" {
			createdBy = "-"
		}
		t.Row(r.RuleID, enabled, r.Selector, sampleRate, evalIDs, createdBy, aio11yhttp.FormatTime(r.CreatedAt))
	}

	if c.Wide {
		return crudcmd.EncodeTable(w, v, "RuleDefinition", []string{"ID", "ENABLED", "SELECTOR", "SAMPLE RATE", "EVALUATORS", "CREATED BY", "CREATED AT"}, row)
	}
	return crudcmd.EncodeTable(w, v, "RuleDefinition", []string{"ID", "ENABLED", "SELECTOR", "SAMPLE RATE", "EVALUATORS"}, row)
}

func (c *TableCodec) Decode(_ io.Reader, _ any) error {
	return crudcmd.ErrTableDecode
}
