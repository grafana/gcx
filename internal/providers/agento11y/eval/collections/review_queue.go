package collections

import (
	"errors"
	"strings"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/agento11y/agento11yhttp"
	"github.com/grafana/gcx/internal/providers/agento11y/eval/reviewqueues"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

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
	return reviewqueues.ValidateRubrics(o.Rubrics)
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
			collectionID := strings.TrimSpace(args[0])
			if err := opts.Validate(collectionID); err != nil {
				return err
			}

			base, err := agento11yhttp.NewClientFromCommand(cmd, loader)
			if err != nil {
				return err
			}

			result, err := reviewqueues.NewClient(base).Create(cmd.Context(), &reviewqueues.RubricRequest{CollectionID: collectionID, RubricEvaluatorIDs: opts.Rubrics})
			if err != nil {
				return err
			}

			cmdio.Success(cmd.ErrOrStderr(), "Review queue created for collection %s", collectionID)
			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}
