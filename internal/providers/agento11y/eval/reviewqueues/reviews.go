package reviewqueues

import (
	"strings"

	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// ReviewManifest is the manifest of 'reviews create': the review queue's
// collection ID alongside one reviewer's answers for one conversation.
type ReviewManifest struct {
	ReviewRequest

	CollectionID string `json:"collection_id"`
}

// ReviewCommands returns the reviews command group.
func ReviewCommands(loader *providers.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reviews",
		Short: "Record human reviews of conversations in a review queue.",
		Long: `Record human reviews of conversations in a review queue.

A review is one reviewer's answers to the queue's rubric for one conversation. Read a queue with 'gcx agento11y review-queues get'.`,
	}
	cmd.AddCommand(newCreateReviewCommand(loader))
	return cmd
}

type createReviewOpts struct {
	IO   cmdio.Options
	File string
}

func (o *createReviewOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("json")
	o.IO.BindFlags(flags)
	flags.StringVarP(&o.File, "filename", "f", "", "File containing the review manifest (use - for stdin)")
}

func newCreateReviewCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &createReviewOpts{}
	cmd := &cobra.Command{
		Use:   "create -f FILE",
		Short: "Record one reviewer's answers for one conversation in a queue.",
		Long: `Record the calling user's answers to a review queue's rubric for one conversation, from a JSON or YAML manifest (or - for stdin).

The manifest has collection_id (the review queue), conversation_id, generation_id (the generation the answers are anchored to; use the conversation's final generation), an optional note applied to every answer, and answers: a list of {evaluator_id, evaluator_version, value}. value holds exactly one of number, bool or string. evaluator_version must be the question's current version (see 'review-queues get'); an answer to an outdated version is rejected. The backend stores all answers or none.

Submitting records scores attributed to the calling user and is not idempotent: read the conversation back with 'review-queues get --conversation' after an uncertain response instead of retrying. Needs the feedback write permission, not eval write.`,
		Example: `  # Submit answers from a file.
  gcx agento11y reviews create -f review.yaml -o json

  # review.yaml
  collection_id: my-collection
  conversation_id: conv-1
  generation_id: gen-1
  answers:
    - evaluator_id: helpfulness-human
      evaluator_version: v1
      value:
        number: 4`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			if opts.File == "" {
				return gcxerrors.NewCommandUsageError(cmd, "--filename is required (use - to read from stdin)", nil)
			}

			var manifest ReviewManifest
			if err := providers.ReadFileOrStdin(opts.File, cmd.InOrStdin(), &manifest); err != nil {
				return err
			}
			manifest.CollectionID = strings.TrimSpace(manifest.CollectionID)
			if manifest.CollectionID == "" || strings.TrimSpace(manifest.ConversationID) == "" || strings.TrimSpace(manifest.GenerationID) == "" || len(manifest.Answers) == 0 {
				return gcxerrors.NewCommandUsageError(cmd, "collection_id, conversation_id, generation_id and at least one answer are required in the manifest", nil)
			}

			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}

			result, err := client.SubmitReview(cmd.Context(), manifest.CollectionID, &manifest.ReviewRequest)
			if err != nil {
				return err
			}

			cmdio.Success(cmd.ErrOrStderr(), "Review of %s submitted to %s", manifest.ConversationID, manifest.CollectionID)
			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}
