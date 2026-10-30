package collections

import (
	"context"
	"errors"
	"net/http"
	"strings"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/agento11y/agento11yhttp"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// ReviewQueueRequest is the body of POST /eval/review-queues.
type ReviewQueueRequest struct {
	CollectionID       string   `json:"collection_id"`
	RubricEvaluatorIDs []string `json:"rubric_evaluator_ids"`
}

// CreateReviewQueue marks an existing collection as a human review queue. The
// queue document is returned untyped: it carries progress and item state that
// gcx only relays.
func (c *Client) CreateReviewQueue(ctx context.Context, request *ReviewQueueRequest) (map[string]any, error) {
	return agento11yhttp.DoJSON[ReviewQueueRequest, map[string]any](ctx, c.base, http.MethodPost, "/eval/review-queues", request, http.StatusOK, http.StatusCreated)
}

type reviewQueueOpts struct {
	IO      cmdio.Options
	Rubrics []string
}

func (o *reviewQueueOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("json")
	o.IO.BindFlags(flags)
	flags.StringArrayVar(&o.Rubrics, "rubric", nil, "Human evaluator ID (repeat for multiple rubrics)")
}

func (o *reviewQueueOpts) Validate(collectionID string) error {
	if err := o.IO.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(collectionID) == "" {
		return errors.New("collection ID is required")
	}
	if len(o.Rubrics) == 0 {
		return errors.New("at least one --rubric is required")
	}
	for _, rubric := range o.Rubrics {
		if strings.TrimSpace(rubric) == "" {
			return errors.New("--rubric cannot be blank")
		}
	}
	return nil
}

func newReviewQueueCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &reviewQueueOpts{}
	cmd := &cobra.Command{
		Use:   "create-review-queue <collection-id>",
		Short: "Create a human review queue for an existing collection.",
		Long: `Create a human review queue for an existing collection, scored against one or more human evaluators (--rubric).

The command only creates the queue: it does not submit reviews or run judge inference. The backend rejects rubrics that are not human evaluators, and rejects a collection that is already a review queue. After an uncertain response, read the queue back before retrying.`,
		Example: `  # Review a collection against one human evaluator.
  gcx agento11y collections create-review-queue my-collection --rubric helpfulness-human -o json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(args[0]); err != nil {
				return err
			}

			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}

			result, err := client.CreateReviewQueue(cmd.Context(), &ReviewQueueRequest{CollectionID: args[0], RubricEvaluatorIDs: opts.Rubrics})
			if err != nil {
				return err
			}

			cmdio.Success(cmd.ErrOrStderr(), "Review queue created for collection %s", args[0])
			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}
