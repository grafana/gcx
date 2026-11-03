package savedconversations

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
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

type createOpts struct {
	IO   cmdio.Options
	File string
}

func (o *createOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("json")
	o.IO.BindFlags(flags)
	flags.StringVarP(&o.File, "filename", "f", "", "File containing the manual conversation fixture (use - for stdin)")
}

func newCreateCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &createOpts{}
	cmd := &cobra.Command{
		Use:   "create -f FILE",
		Short: "Create a conversation fixture from a file, not live telemetry.",
		Long: `Create a manual test fixture from a JSON or YAML file (or - for stdin).

Required top-level fields: saved_id, name and generations (tags is optional). Each generation needs generation_id, model {provider, name}, and at least one input and one output message. A message is {role, content}, where role is user, assistant, tool or system and content is non-empty. operation_name, mode (SYNC or STREAM), started_at and completed_at are optional. Use 'save <conversation-id>' to save an existing conversation instead. The command does not invoke a model or invent live telemetry. Keep the returned saved_id and read the fixture back after an uncertain response instead of retrying.`,
		Example: `  # Create a fixture from a file.
  gcx agento11y saved-conversations create -f fixture.json -o json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			var request ManualSaveRequest
			if err := providers.ReadFileOrStdin(opts.File, cmd.InOrStdin(), &request); err != nil {
				return err
			}
			// Per-generation validation is left to the backend.
			if strings.TrimSpace(request.SavedID) == "" || strings.TrimSpace(request.Name) == "" || len(request.Generations) == 0 {
				return errors.New("saved_id, name and nonempty generations are required")
			}

			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}

			result, err := client.CreateManual(cmd.Context(), &request)
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
