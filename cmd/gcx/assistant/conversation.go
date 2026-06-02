package assistant

import (
	"errors"
	"fmt"

	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/internal/assistant"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func conversationCommand(configOpts *cmdconfig.Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "conversation",
		Short: "Read Grafana Assistant conversations",
		Long:  "Fetch conversation metadata and message history without sending a new prompt.",
	}

	cmd.AddCommand(conversationListCommand(configOpts))
	cmd.AddCommand(conversationGetCommand(configOpts))
	return cmd
}

type conversationListOpts struct {
	IO              cmdio.Options
	source          string
	limit           int
	offset          int
	includeArchived bool
	archivedOnly    bool
	timeout         int
}

func (o *conversationListOpts) Validate() error {
	if err := o.IO.Validate(); err != nil {
		return err
	}
	if o.timeout <= 0 {
		return errors.New("--timeout must be positive")
	}
	if o.limit < 0 {
		return errors.New("--limit must not be negative")
	}
	if o.offset < 0 {
		return errors.New("--offset must not be negative")
	}
	if o.includeArchived && o.archivedOnly {
		return errors.New("cannot use both --include-archived and --archived-only flags")
	}
	return nil
}

func (o *conversationListOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &assistant.ConversationListCodec{})
	o.IO.RegisterCustomCodec("wide", &assistant.ConversationListCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.source, "source", assistant.DefaultConversationSources, `Filter by conversation source. Use "all" for every source, or a comma-separated list (e.g. "assistant,cli").`)
	flags.IntVar(&o.limit, "limit", 15, "Maximum number of conversations to return")
	flags.IntVar(&o.offset, "offset", 0, "Number of conversations to skip (for pagination)")
	flags.BoolVar(&o.includeArchived, "include-archived", false, "Include archived conversations")
	flags.BoolVar(&o.archivedOnly, "archived-only", false, "Show only archived conversations")
	flags.IntVar(&o.timeout, "timeout", 60, "HTTP timeout in seconds")
}

func conversationListCommand(configOpts *cmdconfig.Options) *cobra.Command {
	opts := &conversationListOpts{}

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List Grafana Assistant conversations",
		Long: `List the conversations you can access, most recently updated first.

Use this to discover conversation IDs, then pull a transcript with
'gcx assistant conversation get' or continue one with
'gcx assistant prompt --context-id'.`,
		Args: cobra.NoArgs,
		Example: `  gcx assistant conversation list
  gcx assistant conversation list --source assistant --limit 25
  gcx assistant conversation list -o json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			clientOpts, err := resolveAssistantClientOptions(cmd.Context(), configOpts, opts.timeout, assistant.DefaultAgentID)
			if err != nil {
				return err
			}
			client := assistant.New(clientOpts)

			chats, err := client.ListChats(cmd.Context(), assistant.ListChatsOptions{
				Source:          opts.source,
				Limit:           opts.limit,
				Offset:          opts.offset,
				IncludeArchived: opts.includeArchived,
				ArchivedOnly:    opts.archivedOnly,
			})
			if err != nil {
				return fmt.Errorf("failed to list conversations: %w", err)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), chats)
		},
	}

	opts.setup(cmd.Flags())
	return cmd
}

type conversationGetOpts struct {
	IO      cmdio.Options
	timeout int
}

func (o *conversationGetOpts) Validate() error {
	if err := o.IO.Validate(); err != nil {
		return err
	}
	if o.timeout <= 0 {
		return errors.New("--timeout must be positive")
	}
	return nil
}

func (o *conversationGetOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("text", &assistant.ConversationTextCodec{})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
	flags.IntVar(&o.timeout, "timeout", 60, "HTTP timeout in seconds")
}

func conversationGetCommand(configOpts *cmdconfig.Options) *cobra.Command {
	opts := &conversationGetOpts{}

	cmd := &cobra.Command{
		Use:   "get <conversation-id>",
		Short: "Get a conversation transcript",
		Long: `Fetch conversation metadata and message history for a conversation ID.

Use this to pull a web Assistant chat into a coding agent before continuing it
with 'gcx assistant prompt --context-id'.`,
		Args: cobra.ExactArgs(1),
		Example: `  gcx assistant conversation get 295a674f-3a3d-44e8-9166-3f8054409f65
  gcx assistant conversation get 295a674f-3a3d-44e8-9166-3f8054409f65 -o json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			clientOpts, err := resolveAssistantClientOptions(cmd.Context(), configOpts, opts.timeout, assistant.DefaultAgentID)
			if err != nil {
				return err
			}
			client := assistant.New(clientOpts)

			chatID := args[0]
			chat, err := client.GetChat(cmd.Context(), chatID)
			if err != nil {
				return fmt.Errorf("failed to fetch conversation: %w", err)
			}

			messages, err := client.GetChatMessages(cmd.Context(), chatID)
			if err != nil {
				return fmt.Errorf("failed to fetch conversation messages: %w", err)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), assistant.ConversationTranscript{
				Chat:     *chat,
				Messages: messages,
			})
		},
	}

	opts.setup(cmd.Flags())
	return cmd
}
