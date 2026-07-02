package evaluators

import (
	"context"
	"fmt"
	"io"
	"strconv"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/aio11y/aio11yhttp"
	"github.com/grafana/gcx/internal/providers/aio11y/eval"
	"github.com/grafana/gcx/internal/providers/crudcmd"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Commands returns the evaluators command group.
func Commands(loader *providers.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "evaluators",
		Short: "Manage evaluator definitions (LLM judge, regex, heuristic).",
	}
	cmd.AddCommand(
		newListCommand(),
		newGetCommand(),
		newCreateCommand(),
		newDeleteCommand(),
		newTestCommand(loader),
	)
	return cmd
}

// --- list ---

func newListCommand() *cobra.Command {
	return crudcmd.NewTypedListCommand(crudcmd.TypedListConfig[eval.EvaluatorDefinition]{
		Use:          "list",
		Short:        "List evaluator definitions.",
		DefaultFmt:   "table",
		LimitDefault: 50,
		LimitUsage:   "Maximum number of evaluators to return (0 for no limit)",
		Codecs:       []format.Codec{&TableCodec{}, &TableCodec{Wide: true}},
		Noun:         "evaluator",
		NewCRUD:      NewTypedCRUD,
		ToResource: func(crud *adapter.TypedCRUD[eval.EvaluatorDefinition], item eval.EvaluatorDefinition) (unstructured.Unstructured, error) {
			return specToUnstructured(item, crud.Namespace)
		},
	})
}

// --- get ---

func newGetCommand() *cobra.Command {
	return crudcmd.NewGetCommand(crudcmd.GetConfig[*unstructured.Unstructured]{
		Use:        "get <evaluator-id>",
		Short:      "Get a single evaluator definition.",
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
	return crudcmd.NewCreateCommand(crudcmd.CreateConfig[eval.EvaluatorDefinition]{
		Use:   "create",
		Short: "Create or update an evaluator from a file.",
		Example: `  # Create an evaluator from a YAML file.
  gcx aio11y evaluators create -f evaluator.yaml

  # Create from stdin.
  gcx aio11y evaluators create -f -

  # Export a template, customize it, then create an evaluator.
  gcx aio11y templates show <template-id> -o yaml > evaluator.yaml
  gcx aio11y evaluators create -f evaluator.yaml`,
		DefaultFmt:    "json",
		FilenameUsage: "File containing the evaluator definition (use - for stdin)",
		Read:          ReadEvaluatorFile,
		Create: func(ctx context.Context, def eval.EvaluatorDefinition) (eval.EvaluatorDefinition, error) {
			crud, _, err := NewTypedCRUD(ctx)
			if err != nil {
				return eval.EvaluatorDefinition{}, err
			}
			created, err := crud.Create(ctx, &adapter.TypedObject[eval.EvaluatorDefinition]{Spec: def})
			if err != nil {
				return eval.EvaluatorDefinition{}, err
			}
			return created.Spec, nil
		},
		OnSuccess: func(cmd *cobra.Command, created eval.EvaluatorDefinition) {
			cmdio.Success(cmd.ErrOrStderr(), "Evaluator %s created", created.EvaluatorID)
		},
	})
}

// --- delete ---

func newDeleteCommand() *cobra.Command {
	return crudcmd.NewDeleteCommand(crudcmd.DeleteConfig{
		Use:   "delete ID...",
		Short: "Delete evaluators.",
		Args:  cobra.MinimumNArgs(1),
		Out:   func(cmd *cobra.Command) io.Writer { return cmd.ErrOrStderr() },
		Confirm: func(args []string) string {
			return fmt.Sprintf("Delete %d evaluator(s)?", len(args))
		},
		NewDelete: func(ctx context.Context) (func(string) error, error) {
			crud, _, err := NewTypedCRUD(ctx)
			if err != nil {
				return nil, err
			}
			return func(id string) error { return crud.Delete(ctx, id) }, nil
		},
		Success: func(id string) string { return "Deleted evaluator " + id },
	})
}

// ReadEvaluatorFile reads an evaluator definition from path (or stdin, for
// "-"), trying JSON first and falling back to YAML.
func ReadEvaluatorFile(path string, stdin io.Reader) (*eval.EvaluatorDefinition, error) {
	return crudcmd.ReadJSONOrYAMLFile[eval.EvaluatorDefinition](path, stdin)
}

// --- table codec ---

type TableCodec struct {
	Wide bool
}

func (c *TableCodec) Format() format.Format { return crudcmd.WideFormat(c.Wide) }

func (c *TableCodec) Encode(w io.Writer, v any) error {
	row := func(t *style.TableBuilder, e eval.EvaluatorDefinition) {
		desc := aio11yhttp.Truncate(e.Description, 40)

		if !c.Wide {
			t.Row(e.EvaluatorID, e.Version, e.Kind, desc)
			return
		}

		createdBy := e.CreatedBy
		if createdBy == "" {
			createdBy = "-"
		}
		t.Row(e.EvaluatorID, e.Version, e.Kind, desc, strconv.Itoa(len(e.OutputKeys)), createdBy, aio11yhttp.FormatTime(e.CreatedAt))
	}

	if c.Wide {
		return crudcmd.EncodeTable(w, v, "EvaluatorDefinition", []string{"ID", "VERSION", "KIND", "DESCRIPTION", "OUTPUTS", "CREATED BY", "CREATED AT"}, row)
	}
	return crudcmd.EncodeTable(w, v, "EvaluatorDefinition", []string{"ID", "VERSION", "KIND", "DESCRIPTION"}, row)
}

func (c *TableCodec) Decode(_ io.Reader, _ any) error {
	return crudcmd.ErrTableDecode
}
