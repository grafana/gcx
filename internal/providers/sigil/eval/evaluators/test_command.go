package evaluators

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/goccy/go-yaml"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/sigil/eval"
	"github.com/grafana/gcx/internal/providers/sigil/sigilhttp"
	"github.com/grafana/gcx/internal/style"
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

type testOpts struct {
	EvaluatorID    string
	GenerationID   string
	ConversationID string
	File           string
	IO             cmdio.Options
}

func (o *testOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&o.EvaluatorID, "evaluator", "e", "", "Evaluator ID to test (fetches config from server)")
	flags.StringVarP(&o.GenerationID, "generation", "g", "", "Generation ID to evaluate")
	flags.StringVar(&o.ConversationID, "conversation-id", "", "Conversation ID hint for generation lookup")
	flags.StringVarP(&o.File, "filename", "f", "", "File with full eval:test request body (use - for stdin)")
	o.IO.RegisterCustomCodec("table", &TestTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func (o *testOpts) Validate() error {
	if o.EvaluatorID == "" && o.File == "" {
		return errors.New("either --evaluator/-e or --filename/-f is required")
	}
	if o.EvaluatorID != "" && o.File != "" {
		return errors.New("--evaluator/-e and --filename/-f are mutually exclusive")
	}
	if o.EvaluatorID != "" && o.GenerationID == "" {
		return errors.New("--generation/-g is required when using --evaluator/-e")
	}
	return o.IO.Validate()
}

func newTestCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &testOpts{}
	cmd := &cobra.Command{
		Use:   "test",
		Short: "Run an evaluator against a generation without persisting results.",
		Example: `  # Test an existing evaluator against a generation.
  gcx sigil evaluators test -e my-evaluator -g gen-abc123

  # Test from a full request file (kind, config, output_keys, generation_id).
  gcx sigil evaluators test -f test-request.yaml

  # Test with JSON output.
  gcx sigil evaluators test -e my-evaluator -g gen-abc123 -o json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}

			req, err := buildTestRequest(cmd, client, opts)
			if err != nil {
				return err
			}

			result, err := client.RunTest(cmd.Context(), req)
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func buildTestRequest(cmd *cobra.Command, client *Client, opts *testOpts) (*eval.EvalTestRequest, error) {
	if opts.File != "" {
		req, err := ReadTestRequestFile(opts.File, cmd.InOrStdin())
		if err != nil {
			return nil, err
		}
		if opts.GenerationID != "" {
			req.GenerationID = opts.GenerationID
		}
		if opts.ConversationID != "" {
			req.ConversationID = opts.ConversationID
		}
		return req, nil
	}

	evaluator, err := client.Get(cmd.Context(), opts.EvaluatorID)
	if err != nil {
		return nil, fmt.Errorf("fetching evaluator %s: %w", opts.EvaluatorID, err)
	}
	return &eval.EvalTestRequest{
		Kind:           evaluator.Kind,
		Config:         evaluator.Config,
		OutputKeys:     evaluator.OutputKeys,
		GenerationID:   opts.GenerationID,
		ConversationID: opts.ConversationID,
	}, nil
}

func ReadTestRequestFile(path string, stdin io.Reader) (*eval.EvalTestRequest, error) {
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

	var req eval.EvalTestRequest
	if err := json.Unmarshal(data, &req); err != nil {
		var yamlReq eval.EvalTestRequest
		if yamlErr := yaml.Unmarshal(data, &yamlReq); yamlErr != nil {
			return nil, fmt.Errorf("parsing %s: %w", path, yamlErr)
		}
		return &yamlReq, nil
	}
	return &req, nil
}

// TestTableCodec renders eval:test results as a text table.
type TestTableCodec struct{}

func (c *TestTableCodec) Format() format.Format { return "table" }

func (c *TestTableCodec) Encode(w io.Writer, v any) error {
	resp, ok := v.(*eval.EvalTestResponse)
	if !ok {
		return errors.New("invalid data type for table codec: expected *EvalTestResponse")
	}

	t := style.NewTable("KEY", "TYPE", "VALUE", "PASSED", "EXPLANATION")
	for _, s := range resp.Scores {
		passed := "-"
		if s.Passed != nil {
			if *s.Passed {
				passed = "yes"
			} else {
				passed = "no"
			}
		}

		explanation := sigilhttp.Truncate(s.Explanation, 60)
		t.Row(s.Key, s.Type, fmt.Sprintf("%v", s.Value), passed, explanation)
	}

	if err := t.Render(w); err != nil {
		return err
	}

	fmt.Fprintf(w, "\nGeneration: %s  Execution: %dms\n", resp.GenerationID, resp.ExecutionTimeMs)
	return nil
}

func (c *TestTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}
