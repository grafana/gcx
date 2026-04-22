package evaluators

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
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/aio11y/eval"
	"github.com/grafana/gcx/internal/providers/aio11y/aio11yhttp"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/style"
	"github.com/grafana/gcx/internal/terminal"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
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

type listOpts struct {
	IO    cmdio.Options
	Limit int64
}

func (o *listOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &TableCodec{})
	o.IO.RegisterCustomCodec("wide", &TableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.Int64Var(&o.Limit, "limit", 50, "Maximum number of evaluators to return (0 for no limit)")
}

func newListCommand() *cobra.Command {
	opts := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List evaluator definitions.",
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

			specs := make([]eval.EvaluatorDefinition, len(typedObjs))
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
		Use:   "get <evaluator-id>",
		Short: "Get a single evaluator definition.",
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
	flags.StringVarP(&o.File, "filename", "f", "", "File containing the evaluator definition (use - for stdin)")
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
		Short: "Create or update an evaluator from a file.",
		Example: `  # Create an evaluator from a YAML file.
  gcx aio11y evaluators create -f evaluator.yaml

  # Create from stdin.
  gcx aio11y evaluators create -f -

  # Export a template, customize it, then create an evaluator.
  gcx aio11y templates show <template-id> -o yaml > evaluator.yaml
  gcx aio11y evaluators create -f evaluator.yaml`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			def, err := ReadEvaluatorFile(opts.File, cmd.InOrStdin())
			if err != nil {
				return err
			}

			ctx := cmd.Context()
			crud, _, err := NewTypedCRUD(ctx)
			if err != nil {
				return err
			}

			typedObj := &adapter.TypedObject[eval.EvaluatorDefinition]{Spec: *def}
			created, err := crud.Create(ctx, typedObj)
			if err != nil {
				return err
			}

			cmdio.Success(cmd.ErrOrStderr(), "Evaluator %s created", created.Spec.EvaluatorID)
			return opts.IO.Encode(cmd.OutOrStdout(), created.Spec)
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
		Short: "Delete evaluators.",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !opts.Force {
				if terminal.IsPiped() {
					return errors.New("stdin is not a terminal, use --force to skip confirmation")
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "Delete %d evaluator(s)? [y/N] ", len(args))
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
					return fmt.Errorf("deleting evaluator %s: %w", id, err)
				}
				cmdio.Success(cmd.ErrOrStderr(), "Deleted evaluator %s", id)
			}
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func ReadEvaluatorFile(path string, stdin io.Reader) (*eval.EvaluatorDefinition, error) {
	var data []byte
	var err error
	if path == "-" {
		data, err = io.ReadAll(stdin)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	var def eval.EvaluatorDefinition
	if err := json.Unmarshal(data, &def); err != nil {
		var yamlDef eval.EvaluatorDefinition
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
	evaluators, ok := v.([]eval.EvaluatorDefinition)
	if !ok {
		return errors.New("invalid data type for table codec: expected []EvaluatorDefinition")
	}

	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable("ID", "VERSION", "KIND", "DESCRIPTION", "OUTPUTS", "CREATED BY", "CREATED AT")
	} else {
		t = style.NewTable("ID", "VERSION", "KIND", "DESCRIPTION")
	}

	for _, e := range evaluators {
		desc := aio11yhttp.Truncate(e.Description, 40)

		if c.Wide {
			createdBy := e.CreatedBy
			if createdBy == "" {
				createdBy = "-"
			}
			t.Row(e.EvaluatorID, e.Version, e.Kind, desc, strconv.Itoa(len(e.OutputKeys)), createdBy, aio11yhttp.FormatTime(e.CreatedAt))
		} else {
			t.Row(e.EvaluatorID, e.Version, e.Kind, desc)
		}
	}
	return t.Render(w)
}

func (c *TableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}
