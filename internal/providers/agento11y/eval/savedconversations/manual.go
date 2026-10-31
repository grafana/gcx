package savedconversations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/agento11y/agento11yhttp"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// ManualSaveRequest is the body of POST /eval/saved-conversations:manual.
// Generations stay raw so every field the backend accepts passes through
// without gcx modelling the generation schema.
type ManualSaveRequest struct {
	SavedID     string            `json:"saved_id"`
	Name        string            `json:"name"`
	Tags        map[string]string `json:"tags,omitempty"`
	Generations []json.RawMessage `json:"generations"`
}

// CreateManual saves a hand-written conversation fixture.
func (c *Client) CreateManual(ctx context.Context, request *ManualSaveRequest) (*SavedConversation, error) {
	result, err := agento11yhttp.DoJSON[ManualSaveRequest, SavedConversation](ctx, c.base, http.MethodPost, basePath+":manual", request, http.StatusOK, http.StatusCreated)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

type manualOpts struct {
	IO   cmdio.Options
	File string
}

func (o *manualOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("json")
	o.IO.BindFlags(flags)
	flags.StringVarP(&o.File, "filename", "f", "", "File containing the manual conversation JSON (use - for stdin)")
}

func (o *manualOpts) Validate() error {
	if err := o.IO.Validate(); err != nil {
		return err
	}
	if o.File == "" {
		return errors.New("--filename is required (use - to read from stdin)")
	}
	return nil
}

// readManualRequest decodes and sanity-checks a manual conversation file.
// Per-generation validation is left to the backend.
func readManualRequest(path string, stdin io.Reader) (*ManualSaveRequest, error) {
	var data []byte
	var err error
	if path == "-" {
		data, err = io.ReadAll(stdin)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, err
	}

	var request ManualSaveRequest
	if err := json.Unmarshal(data, &request); err != nil {
		return nil, fmt.Errorf("invalid manual conversation JSON: %w", err)
	}
	if strings.TrimSpace(request.SavedID) == "" || strings.TrimSpace(request.Name) == "" || len(request.Generations) == 0 {
		return nil, errors.New("saved_id, name and nonempty generations are required")
	}
	return &request, nil
}

func newManualCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &manualOpts{}
	cmd := &cobra.Command{
		Use:   "create-manual",
		Short: "Create a manual conversation fixture from JSON, not live telemetry.",
		Long: `Create a manual test fixture from a JSON file (or - for stdin).

Required top-level fields: saved_id, name and generations (tags is optional). Each generation needs generation_id, model {provider, name}, and at least one input and one output message. A message is {role, content}, where role is user, assistant, tool or system and content is non-empty. operation_name, mode (SYNC or STREAM), started_at and completed_at are optional. The command does not invoke a model or invent live telemetry. Keep the returned saved_id and read the fixture back after an uncertain response instead of retrying.`,
		Example: `  # Create a fixture from a JSON file.
  gcx agento11y saved-conversations create-manual -f fixture.json -o json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			request, err := readManualRequest(opts.File, cmd.InOrStdin())
			if err != nil {
				return err
			}

			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}

			result, err := client.CreateManual(cmd.Context(), request)
			if err != nil {
				return err
			}

			cmdio.Success(cmd.ErrOrStderr(), "Saved conversation %s created", result.SavedID)
			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}
