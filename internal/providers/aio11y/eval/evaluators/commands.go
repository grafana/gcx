package evaluators

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/goccy/go-yaml"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/aio11y/aio11yhttp"
	"github.com/grafana/gcx/internal/providers/aio11y/commandutil"
	"github.com/grafana/gcx/internal/providers/aio11y/eval"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Commands returns the evaluators command group. A nil loader is valid and
// behaves like a zero-value loader (see providers.ConfigLoader).
func Commands(loader *providers.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "evaluators",
		Short: "Manage evaluator definitions (LLM judge, regex, heuristic).",
	}
	cmd.AddCommand(
		newListCommand(loader),
		newGetCommand(loader),
		newCreateCommand(loader),
		newDeleteCommand(loader),
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

func newListCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List evaluator definitions.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			crud, namespace, err := NewTypedCRUD(ctx, loader)
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

func newGetCommand(loader *providers.ConfigLoader) *cobra.Command {
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
			crud, namespace, err := NewTypedCRUD(ctx, loader)
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

func newCreateCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &createOpts{}
	cmd := &cobra.Command{
		Use:   "upsert",
		Short: "Create or update an evaluator from a file.",
		Example: `  # Create an evaluator from a YAML file.
  gcx agento11y evaluators upsert -f evaluator.yaml

  # Create from stdin.
  gcx agento11y evaluators upsert -f -

  # Export a template, customize it, then create an evaluator.
  gcx agento11y templates get <template-id> -o yaml > evaluator.yaml
  gcx agento11y evaluators upsert -f evaluator.yaml`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			def, err := ReadEvaluatorFile(opts.File, cmd.InOrStdin())
			if err != nil {
				return err
			}

			ctx := cmd.Context()
			crud, _, err := NewTypedCRUD(ctx, loader)
			if err != nil {
				return err
			}

			typedObj := &adapter.TypedObject[eval.EvaluatorDefinition]{Spec: *def}
			created, err := crud.Create(ctx, typedObj)
			if err != nil {
				return err
			}

			cmdio.Success(cmd.ErrOrStderr(), "Evaluator %s upserted", created.Spec.EvaluatorID)
			return opts.IO.Encode(cmd.OutOrStdout(), created.Spec)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- delete ---

type deleteOpts struct {
	IO    cmdio.Options
	Force bool
}

func (o *deleteOpts) setup(flags *pflag.FlagSet) {
	flags.BoolVar(&o.Force, "force", false, "Skip confirmation prompt")
	// The delete result is a BatchMutation document through the codec
	// system: the human text default stays silent (per-id receipts go to
	// stderr, as they always have); agent mode and explicit -o json/yaml
	// get the structured document.
	o.IO.RegisterCustomCodec("text", commandutil.SilentTextCodec{})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
}

func newDeleteCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &deleteOpts{}
	cmd := &cobra.Command{
		Use:   "delete ID...",
		Short: "Delete evaluators.",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			proceed, err := providers.ConfirmDestructive(cmd.InOrStdin(), cmd.ErrOrStderr(), opts.Force,
				fmt.Sprintf("Delete %d evaluator(s)?", len(args)))
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			ctx := cmd.Context()
			crud, _, err := NewTypedCRUD(ctx, loader)
			if err != nil {
				return err
			}

			return runDelete(cmd.OutOrStdout(), cmd.ErrOrStderr(), opts, args, func(id string) error {
				return crud.Delete(ctx, id)
			})
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// runDelete performs the delete loop and writes the result document. Split
// from RunE so the output contract is testable without a live plugin API.
func runDelete(stdout, stderr io.Writer, opts *deleteOpts, ids []string, del func(id string) error) error {
	return commandutil.RunBatchDelete(stdout, stderr, &opts.IO,
		"evaluator", "Deleted evaluator %s", "deleting evaluator %s", ids, del)
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
