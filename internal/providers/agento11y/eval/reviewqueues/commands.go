package reviewqueues

import (
	"errors"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/agento11y/agento11yhttp"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// warnPartial tells the reader that a queue's progress counts are lower bounds.
// The backend counts at most a fixed number of conversations per queue and sets
// truncated when it stopped there, so a table that reads "pending 0" can still
// hide unreviewed conversations.
func warnPartial(w io.Writer, collectionID string, counted int) {
	cmdio.Warning(w, "Review queue %s progress is partial: only %d conversations are counted, so total, reviewed and pending are lower bounds", collectionID, counted)
}

func newClient(cmd *cobra.Command, loader *providers.ConfigLoader) (*Client, error) {
	base, err := agento11yhttp.NewClientFromCommand(cmd, loader)
	if err != nil {
		return nil, err
	}
	return NewClient(base), nil
}

// Commands returns the review-queues command group.
func Commands(loader *providers.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "review-queues",
		Short: "Review conversations in a collection with human evaluators.",
		Long: `Review conversations in a collection with human evaluators.

A review queue is a collection marked for human review. Its rubric is a set of human evaluators (see 'gcx agento11y human-evaluators list'); a conversation counts as reviewed once every rubric question has an answer given in the queue. Create a queue from a manifest with 'gcx agento11y review-queues create -f review-queue.yaml'; record a reviewer's answers with 'gcx agento11y reviews create'.

Review queues are only available for tenants with human review enabled; for other tenants the backend answers 404.`,
	}
	cmd.AddCommand(
		newListCommand(loader),
		newGetCommand(loader),
		newCreateCommand(loader),
		newUpdateCommand(loader),
		newListScoresCommand(loader),
	)
	return cmd
}

// --- list ---

type listOpts struct {
	IO cmdio.Options
}

func (o *listOpts) setup(flags *pflag.FlagSet) {
	cmdio.RegisterTable(&o.IO, Table())
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func newListCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List review queues with their progress.",
		Long:  "List review queues with their rubric and progress. Items are omitted: use 'get' for a queue's conversations.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}

			queues, err := client.List(cmd.Context())
			if err != nil {
				return err
			}
			for i := range queues {
				if queues[i].Truncated {
					warnPartial(cmd.ErrOrStderr(), queues[i].CollectionID, queues[i].Total)
				}
			}
			return opts.IO.Encode(cmd.OutOrStdout(), queues)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- get ---

type getOpts struct {
	IO           cmdio.Options
	Conversation string
}

func (o *getOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.Conversation, "conversation", "", "Read the queue as seen from this conversation ID: its membership, position and answers")
}

func newGetCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &getOpts{}
	cmd := &cobra.Command{
		Use:   "get <collection-id>",
		Short: "Get a review queue with its rubric, progress and items.",
		Long: `Get a review queue by its collection ID, with its rubric, progress and the answers given for each conversation.

With --conversation the command returns a different, smaller document instead: the rubric and progress plus that one conversation's membership, position in the queue and answers. It stays correct for a conversation beyond a truncated queue read.`,
		Example: `  # Read a whole queue.
  gcx agento11y review-queues get my-collection -o json

  # Read one conversation's answers in the queue.
  gcx agento11y review-queues get my-collection --conversation conv-1 -o json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			collectionID := strings.TrimSpace(args[0])
			conversationSet := cmd.Flags().Changed("conversation")
			if err := opts.Validate(collectionID, conversationSet); err != nil {
				return err
			}

			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}

			if conversationSet {
				view, err := client.GetConversation(cmd.Context(), collectionID, opts.Conversation)
				if err != nil {
					return err
				}
				if view.Truncated {
					warnPartial(cmd.ErrOrStderr(), collectionID, view.Total)
				}
				return opts.IO.Encode(cmd.OutOrStdout(), view)
			}

			queue, err := client.Get(cmd.Context(), collectionID)
			if err != nil {
				return err
			}
			if queue.Truncated {
				warnPartial(cmd.ErrOrStderr(), collectionID, queue.Total)
			}
			return opts.IO.Encode(cmd.OutOrStdout(), queue)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// Validate rejects an explicitly empty --conversation: treating it as unset
// would silently turn a one-conversation read into a whole-queue read.
func (o *getOpts) Validate(collectionID string, conversationSet bool) error {
	if err := o.IO.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(collectionID) == "" {
		return errors.New("collection ID is required")
	}
	if conversationSet && strings.TrimSpace(o.Conversation) == "" {
		return errors.New("--conversation cannot be blank")
	}
	return nil
}

// --- create ---

type createOpts struct {
	IO   cmdio.Options
	File string
}

func (o *createOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("json")
	o.IO.BindFlags(flags)
	flags.StringVarP(&o.File, "filename", "f", "", "File containing the review queue manifest (use - for stdin)")
}

func newCreateCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &createOpts{}
	cmd := &cobra.Command{
		Use:   "create -f FILE",
		Short: "Create a human review queue from a manifest.",
		Long: `Create a human review queue for an existing collection from a JSON or YAML manifest (or - for stdin).

The manifest has collection_id (the collection to review) and rubric_evaluator_ids (the human evaluators that make up the rubric). The command only creates the queue: it does not submit reviews or run judge inference. The backend rejects rubrics that are not human evaluators, and rejects a collection that is already a review queue. After an uncertain response, read the queue back before retrying.`,
		Example: `  # Review a collection against one human evaluator.
  gcx agento11y review-queues create -f review-queue.yaml -o json

  # review-queue.yaml
  collection_id: my-collection
  rubric_evaluator_ids:
    - helpfulness-human`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			var request RubricRequest
			if err := providers.ReadFileOrStdin(opts.File, cmd.InOrStdin(), &request); err != nil {
				return err
			}
			request.CollectionID = strings.TrimSpace(request.CollectionID)
			if request.CollectionID == "" {
				return errors.New("collection_id is required in the manifest")
			}
			if len(request.RubricEvaluatorIDs) == 0 {
				return errors.New("rubric_evaluator_ids needs at least one human evaluator ID")
			}
			if slices.ContainsFunc(request.RubricEvaluatorIDs, func(id string) bool { return strings.TrimSpace(id) == "" }) {
				return errors.New("rubric_evaluator_ids cannot contain a blank ID")
			}

			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}

			queue, err := client.Create(cmd.Context(), &request)
			if err != nil {
				return err
			}

			cmdio.Success(cmd.ErrOrStderr(), "Review queue created for collection %s", request.CollectionID)
			return opts.IO.Encode(cmd.OutOrStdout(), queue)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- update ---

type updateOpts struct {
	IO      cmdio.Options
	Rubrics []string
}

func (o *updateOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("json")
	o.IO.BindFlags(flags)
	flags.StringArrayVar(&o.Rubrics, "rubric", nil, "Human evaluator ID (repeat for multiple rubrics; replaces the whole rubric)")
}

func (o *updateOpts) Validate(collectionID string) error {
	if err := o.IO.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(collectionID) == "" {
		return errors.New("collection ID is required")
	}
	return validateRubrics(o.Rubrics)
}

// validateRubrics checks the --rubric values of update.
func validateRubrics(rubrics []string) error {
	if len(rubrics) == 0 {
		return errors.New("at least one --rubric is required")
	}
	for _, rubric := range rubrics {
		if strings.TrimSpace(rubric) == "" {
			return errors.New("--rubric cannot be blank")
		}
	}
	return nil
}

func newUpdateCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &updateOpts{}
	cmd := &cobra.Command{
		Use:   "update <collection-id>",
		Short: "Replace a review queue's rubric.",
		Long: `Replace the rubric of an existing review queue with the given human evaluators.

The --rubric list replaces the whole rubric, it is not merged with the current one. Answers already given are kept. The backend rejects rubrics that are not human evaluators and collections that are not review queues.`,
		Example: `  # Make helpfulness-human the only question in the queue.
  gcx agento11y review-queues update my-collection --rubric helpfulness-human -o json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			collectionID := strings.TrimSpace(args[0])
			if err := opts.Validate(collectionID); err != nil {
				return err
			}

			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}

			queue, err := client.SetRubric(cmd.Context(), collectionID, &RubricRequest{RubricEvaluatorIDs: opts.Rubrics})
			if err != nil {
				return err
			}

			cmdio.Success(cmd.ErrOrStderr(), "Review queue %s rubric updated", collectionID)
			return opts.IO.Encode(cmd.OutOrStdout(), queue)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- list-scores ---

// scoresEnvelope wraps the exported answers in the list envelope so stdout is
// `{"items": [...]}` and the backend's row cap can ride in-band in list_meta
// (output.md §15.2). The table/wide codec reads the items.
type scoresEnvelope struct {
	Items    []ScoreRow      `json:"items" yaml:"items"`
	ListMeta *cmdio.ListMeta `json:"list_meta,omitempty" yaml:"list_meta,omitempty"`
}

type listScoresOpts struct {
	IO    cmdio.Options
	Limit int
}

func (o *listScoresOpts) setup(flags *pflag.FlagSet) {
	table := cmdio.TableAdapter[scoresEnvelope, ScoreRow]{
		Table: ScoresTable(),
		Rows:  func(result scoresEnvelope) ([]ScoreRow, error) { return result.Items, nil },
	}
	o.IO.RegisterCustomCodec(cmdio.FormatTable, table.Codec(cmdio.FormatTable))
	o.IO.RegisterCustomCodec(cmdio.FormatWide, table.Codec(cmdio.FormatWide))
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	// The backend caps the export, so the standard "0 means all" binder does
	// not apply; disclose the cap in the help instead.
	flags.IntVar(&o.Limit, "limit", 0, "Maximum number of answers to return. 0 returns every answer the backend exports, up to its row cap")
}

func (o *listScoresOpts) Validate(collectionID string) error {
	if err := o.IO.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(collectionID) == "" {
		return errors.New("collection ID is required")
	}
	if o.Limit < 0 {
		return errors.New("--limit must be 0 or greater")
	}
	return nil
}

// exportPage applies --limit to an export and builds its truncation metadata.
// A limit that drops rows leaves the total unknown when the backend also
// stopped at its row cap; a backend cap that no limit is binding records the
// number of rows the backend returned as the cap, which --limit cannot raise.
func exportPage(rows []ScoreRow, limit int, backendTruncated bool) ([]ScoreRow, *cmdio.ListMeta) {
	page, meta := cmdio.TruncateCompleteList(rows, limit)
	switch {
	case meta != nil && backendTruncated:
		meta.Total = nil
	case meta == nil && backendTruncated:
		meta = &cmdio.ListMeta{Truncated: true, Returned: len(page), Cap: len(page)}
	}
	return page, meta
}

func newListScoresCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &listScoresOpts{}
	cmd := &cobra.Command{
		Use:   "list-scores <collection-id>",
		Short: "List every answer given in a review queue.",
		Long: `List every answer given in a review queue, read from the backend's scores.csv export.

Cells are the export's text: a text cell that starts with =, +, - or @ carries a leading apostrophe added by the backend. The export is capped by the backend and --limit cannot reach past that cap; this command has no filters, so answers beyond the cap cannot be reached from the CLI. When the cap is what cuts the export, list_meta.cap in the payload says so; when --limit also trims the page, list_meta omits the cap and a warning on stderr reports it. Only one export per tenant runs at a time: if the backend answers 429, wait and retry.`,
		Example: `  # Export a queue's answers as JSON.
  gcx agento11y review-queues list-scores my-collection -o json

  # Only the first 20 answers.
  gcx agento11y review-queues list-scores my-collection --limit 20 -o json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			collectionID := strings.TrimSpace(args[0])
			if err := opts.Validate(collectionID); err != nil {
				return err
			}

			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}

			rows, backendTruncated, err := client.ExportScores(cmd.Context(), collectionID)
			if err != nil {
				return err
			}

			page, meta := exportPage(rows, opts.Limit, backendTruncated)
			// The shared hint below discloses the cap when it is the binding
			// bound; when --limit also trimmed the page, list_meta carries no
			// cap, so say it here.
			if backendTruncated && meta.Cap == 0 {
				cmdio.Warning(cmd.ErrOrStderr(), "Export truncated by the backend row cap: %d answers fetched, more exist", len(rows))
			}
			meta = cmdio.AttachListMeta(meta, os.Args)
			if err := opts.IO.Encode(cmd.OutOrStdout(), scoresEnvelope{Items: page, ListMeta: meta}); err != nil {
				return err
			}
			cmdio.EmitListTruncationHint(cmd.ErrOrStderr(), meta)
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- tables ---

// Table returns the table codec for review queues.
func Table() cmdio.Table[ReviewQueue] {
	return cmdio.Table[ReviewQueue]{Columns: []cmdio.Column[ReviewQueue]{
		{Header: "ID", Content: func(q ReviewQueue) string { return q.CollectionID }},
		{Header: "NAME", Content: func(q ReviewQueue) string { return q.Name }},
		{Header: "TOTAL", Content: func(q ReviewQueue) string { return strconv.Itoa(q.Total) }},
		{Header: "REVIEWED", Content: func(q ReviewQueue) string { return strconv.Itoa(q.Reviewed) }},
		{Header: "PENDING", Content: func(q ReviewQueue) string { return strconv.Itoa(q.Pending) }},
		{Header: "RUBRIC", Content: func(q ReviewQueue) string {
			ids := make([]string, 0, len(q.Rubric))
			for _, item := range q.Rubric {
				ids = append(ids, item.EvaluatorID)
			}
			return cmdio.OrDash(strings.Join(ids, ", "))
		}},
		{Header: "TRUNCATED", Visible: cmdio.WideOnly, Content: func(q ReviewQueue) string { return strconv.FormatBool(q.Truncated) }},
		{Header: "NEXT", Visible: cmdio.WideOnly, Content: func(q ReviewQueue) string { return cmdio.OrDash(q.NextConversationID) }},
	}}
}

// ScoresTable returns the table codec for exported review answers.
func ScoresTable() cmdio.Table[ScoreRow] {
	return cmdio.Table[ScoreRow]{Columns: []cmdio.Column[ScoreRow]{
		{Header: "CONVERSATION", Content: func(r ScoreRow) string { return r.ConversationID }},
		{Header: "EVALUATOR", Content: func(r ScoreRow) string { return r.EvaluatorID }},
		{Header: "VERSION", Content: func(r ScoreRow) string { return r.EvaluatorVersion }},
		{Header: "KEY", Content: func(r ScoreRow) string { return r.ScoreKey }},
		{Header: "VALUE", Content: func(r ScoreRow) string { return agento11yhttp.Truncate(r.Value, 40) }},
		{Header: "PASSED", Content: func(r ScoreRow) string { return cmdio.OrDash(r.Passed) }},
		{Header: "REVIEWER", Content: func(r ScoreRow) string { return cmdio.OrDash(r.Reviewer) }},
		{Header: "CREATED AT", Content: func(r ScoreRow) string { return cmdio.OrDash(r.CreatedAt) }},
		{Header: "GENERATION", Visible: cmdio.WideOnly, Content: func(r ScoreRow) string { return r.GenerationID }},
		{Header: "AGENT", Visible: cmdio.WideOnly, Content: func(r ScoreRow) string { return cmdio.OrDash(r.AgentName) }},
		{Header: "AGENT VERSION", Visible: cmdio.WideOnly, Content: func(r ScoreRow) string { return cmdio.OrDash(r.AgentVersion) }},
		{Header: "NOTE", Visible: cmdio.WideOnly, Content: func(r ScoreRow) string { return agento11yhttp.Truncate(r.Note, 40) }},
	}}
}
