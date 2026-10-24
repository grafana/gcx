package archive

import (
	"fmt"
	"strings"

	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

func (h *archiveHelper) streamsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "streams",
		Short: "Manage archive streams.",
		Long: "Manage archive streams: log stream selectors whose matching lines are copied to an archive destination.\n\n" +
			"Changes reach log ingestion after a short delay.",
	}
	cmd.AddCommand(
		h.streamsListCommand(),
		h.streamsGetCommand(),
		h.streamsCreateCommand(),
		h.streamsUpdateCommand(),
		h.streamsDeleteCommand(),
	)
	return cmd
}

func streamsTable() cmdio.Table[StreamRow] {
	return cmdio.Table[StreamRow]{
		Columns: []cmdio.Column[StreamRow]{
			{Header: "ID", Content: func(s StreamRow) string { return s.ID }},
			{Header: "NAME", Content: func(s StreamRow) string { return s.Name }},
			{Header: "DESTINATION", Content: func(s StreamRow) string { return cmdio.OrDash(s.ArchiveDestinationName) }},
			{Header: "SELECTOR", Content: func(s StreamRow) string { return s.StreamSelector }},
			{Header: "DESTINATION ID", Visible: cmdio.WideOnly, Content: func(s StreamRow) string { return s.ArchiveDestinationID }},
			{Header: "CREATED AT", Visible: cmdio.WideOnly, Content: func(s StreamRow) string { return s.CreatedAt }},
			{Header: "UPDATED AT", Visible: cmdio.WideOnly, Content: func(s StreamRow) string { return s.UpdatedAt }},
		},
	}
}

func destinationNames(destinations []Destination) map[string]string {
	names := make(map[string]string, len(destinations))
	for _, d := range destinations {
		names[d.ID] = d.Name
	}
	return names
}

func streamRow(s Stream, names map[string]string) StreamRow {
	return StreamRow{Stream: s, ArchiveDestinationName: names[s.ArchiveDestinationID]}
}

// streams list

type streamsListOpts struct {
	IO          cmdio.Options
	Destination string
}

func (o *streamsListOpts) setup(cmd *cobra.Command) {
	cmd.Flags().StringVar(&o.Destination, "destination", "", "Only list streams archived to the destination with this ID")
	cmdio.RegisterTable(&o.IO, streamsTable())
	o.IO.DefaultFormat(cmdio.FormatTable)
	o.IO.BindFlags(cmd.Flags())
}

func (o *streamsListOpts) Validate(cmd *cobra.Command) error {
	if cmd.Flags().Changed("destination") {
		if err := rejectEmptyFlags(cmd, map[string]string{"destination": o.Destination}); err != nil {
			return err
		}
	}
	return o.IO.Validate()
}

func (h *archiveHelper) streamsListCommand() *cobra.Command {
	opts := &streamsListOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List archive streams.",
		Example: `  # List archive streams across all destinations
  gcx logs archive streams list

  # List the streams archived to one destination
  gcx logs archive streams list --destination 0190a7e1-9c4d-7a2b-8e3f-5d6c7b8a9f01

  # Include destination IDs and timestamps
  gcx logs archive streams list -o wide`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(cmd); err != nil {
				return err
			}

			ctx := cmd.Context()
			client, err := h.newClient(ctx)
			if err != nil {
				return err
			}

			var streams []Stream
			var destinations []Destination
			g, gctx := errgroup.WithContext(ctx)
			g.Go(func() error {
				var err error
				streams, err = client.ListStreams(gctx)
				return err
			})
			g.Go(func() error {
				var err error
				destinations, err = client.ListDestinations(gctx)
				return err
			})
			if err := g.Wait(); err != nil {
				return err
			}

			names := destinationNames(destinations)
			filterID := ""
			if opts.Destination != "" {
				filterID = canonicalID(opts.Destination)
				if _, ok := names[filterID]; !ok {
					return &gcxerrors.DetailedError{
						Summary:     gcxerrors.SummaryResourceNotFound,
						Details:     fmt.Sprintf("No archive destination has the ID %q.", opts.Destination),
						Suggestions: []string{"List archive destinations: gcx logs archive destinations list"},
					}
				}
			}

			rows := make([]StreamRow, 0, len(streams))
			for _, s := range streams {
				if filterID != "" && s.ArchiveDestinationID != filterID {
					continue
				}
				rows = append(rows, streamRow(s, names))
			}

			return opts.IO.Encode(cmd.OutOrStdout(), rows)
		},
	}
	opts.setup(cmd)
	return cmd
}

// streams get

type streamsGetOpts struct {
	IO cmdio.Options
}

func (o *streamsGetOpts) setup(cmd *cobra.Command) {
	registerSingleTable(&o.IO, streamsTable())
	o.IO.DefaultFormat(cmdio.FormatTable)
	o.IO.BindFlags(cmd.Flags())
}

func (o *streamsGetOpts) Validate(cmd *cobra.Command, args []string) error {
	if err := rejectEmptyID(cmd, "stream", args[0]); err != nil {
		return err
	}
	return o.IO.Validate()
}

func (h *archiveHelper) streamsGetCommand() *cobra.Command {
	opts := &streamsGetOpts{}
	cmd := &cobra.Command{
		Use:   "get ID",
		Short: "Show one archive stream.",
		Example: `  # Show a stream
  gcx logs archive streams get 0190a7e2-5b1c-7d3e-9f40-1a2b3c4d5e6f

  # Show a stream as YAML
  gcx logs archive streams get 0190a7e2-5b1c-7d3e-9f40-1a2b3c4d5e6f -o yaml`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(cmd, args); err != nil {
				return err
			}
			id := canonicalID(args[0])

			ctx := cmd.Context()
			client, err := h.newClient(ctx)
			if err != nil {
				return err
			}

			var stream *Stream
			var destinations []Destination
			g, gctx := errgroup.WithContext(ctx)
			g.Go(func() error {
				var err error
				stream, err = client.GetStream(gctx, id)
				return err
			})
			g.Go(func() error {
				var err error
				destinations, err = client.ListDestinations(gctx)
				return err
			})
			if err := g.Wait(); err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), streamRow(*stream, destinationNames(destinations)))
		},
	}
	opts.setup(cmd)
	return cmd
}

// streams create

type streamsCreateOpts struct {
	IO          cmdio.Options
	Destination string
	Name        string
	Selector    string
}

func (o *streamsCreateOpts) setup(cmd *cobra.Command) {
	cmd.Flags().StringVar(&o.Destination, "destination", "", "ID of the destination to archive to (required)")
	cmd.Flags().StringVar(&o.Name, "name", "", "Stream name, unique within the destination (required)")
	cmd.Flags().StringVar(&o.Selector, "selector", "", "Log stream selector, such as '{service=\"payments\"}' (required)")
	_ = cmd.MarkFlagRequired("destination")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("selector")
	o.IO.DefaultFormat("json")
	o.IO.BindFlags(cmd.Flags())
}

func (o *streamsCreateOpts) Validate(cmd *cobra.Command) error {
	if err := rejectEmptyFlags(cmd, map[string]string{
		"destination": o.Destination,
		"name":        o.Name,
		"selector":    o.Selector,
	}); err != nil {
		return err
	}
	return o.IO.Validate()
}

func (h *archiveHelper) streamsCreateCommand() *cobra.Command {
	opts := &streamsCreateOpts{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an archive stream.",
		Long: "Create an archive stream: log lines from streams matching --selector are copied to the destination.\n\n" +
			"The selector uses label matchers only, such as '{service=\"payments\", env=~\"prod|staging\"}'. " +
			"A destination cannot have two streams with the same name or the same selector.",
		Example: `  # Archive one service's logs to a destination
  gcx logs archive streams create --destination 0190a7e1-9c4d-7a2b-8e3f-5d6c7b8a9f01 \
    --name payments --selector '{service="payments"}'

  # Archive several namespaces
  gcx logs archive streams create --destination 0190a7e1-9c4d-7a2b-8e3f-5d6c7b8a9f01 \
    --name platform --selector '{namespace=~"ingress|cert-manager"}'`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(cmd); err != nil {
				return err
			}

			ctx := cmd.Context()
			client, err := h.newClient(ctx)
			if err != nil {
				return err
			}

			destination, err := client.GetDestination(ctx, canonicalID(opts.Destination))
			if err != nil {
				return err
			}

			created, err := client.CreateStream(ctx, &StreamCreate{
				ArchiveDestinationID: destination.ID,
				Name:                 opts.Name,
				Signal:               streamSignal,
				StreamSelector:       opts.Selector,
			})
			if err != nil {
				return err
			}

			cmdio.Success(cmd.ErrOrStderr(), "Created archive stream %q (id=%s)", created.Name, created.ID)
			return opts.IO.Encode(cmd.OutOrStdout(), StreamRow{Stream: *created, ArchiveDestinationName: destination.Name})
		},
	}
	opts.setup(cmd)
	return cmd
}

// streams update

type streamsUpdateOpts struct {
	IO          cmdio.Options
	Destination string
	Name        string
	Selector    string
}

func (o *streamsUpdateOpts) setup(cmd *cobra.Command) {
	cmd.Flags().StringVar(&o.Destination, "destination", "", "ID of the destination to move the stream to")
	cmd.Flags().StringVar(&o.Name, "name", "", "New stream name")
	cmd.Flags().StringVar(&o.Selector, "selector", "", "New log stream selector")
	o.IO.DefaultFormat("json")
	o.IO.BindFlags(cmd.Flags())
}

func (o *streamsUpdateOpts) Validate(cmd *cobra.Command, args []string) error {
	if err := rejectEmptyID(cmd, "stream", args[0]); err != nil {
		return err
	}
	flags := cmd.Flags()
	if !flags.Changed("destination") && !flags.Changed("name") && !flags.Changed("selector") {
		return gcxerrors.NewCommandUsageError(cmd, "specify at least one of --destination, --name or --selector", nil)
	}
	values := map[string]string{}
	for name, value := range map[string]string{"destination": o.Destination, "name": o.Name, "selector": o.Selector} {
		if flags.Changed(name) {
			values[name] = value
		}
	}
	if err := rejectEmptyFlags(cmd, values); err != nil {
		return err
	}
	return o.IO.Validate()
}

func (h *archiveHelper) streamsUpdateCommand() *cobra.Command {
	opts := &streamsUpdateOpts{}
	cmd := &cobra.Command{
		Use:   "update ID",
		Short: "Update an archive stream.",
		Long: "Update an archive stream's name, selector or destination. Fields without a flag keep their current values.\n\n" +
			"Moving a stream to another destination affects lines archived from then on; lines already archived stay where they are.",
		Example: `  # Change a stream's selector
  gcx logs archive streams update 0190a7e2-5b1c-7d3e-9f40-1a2b3c4d5e6f --selector '{service=~"payments|billing"}'

  # Move a stream to another destination
  gcx logs archive streams update 0190a7e2-5b1c-7d3e-9f40-1a2b3c4d5e6f --destination 0190a7e1-9c4d-7a2b-8e3f-5d6c7b8a9f02`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(cmd, args); err != nil {
				return err
			}
			id := canonicalID(args[0])

			ctx := cmd.Context()
			client, err := h.newClient(ctx)
			if err != nil {
				return err
			}

			existing, err := client.GetStream(ctx, id)
			if err != nil {
				return fmt.Errorf("failed to fetch existing archive stream for merge: %w", err)
			}

			req := StreamUpdate{
				ArchiveDestinationID: existing.ArchiveDestinationID,
				Name:                 existing.Name,
				StreamSelector:       existing.StreamSelector,
			}
			destinationName := ""
			if cmd.Flags().Changed("destination") {
				destination, err := client.GetDestination(ctx, canonicalID(opts.Destination))
				if err != nil {
					return err
				}
				req.ArchiveDestinationID = destination.ID
				destinationName = destination.Name
			}
			if cmd.Flags().Changed("name") {
				req.Name = opts.Name
			}
			if cmd.Flags().Changed("selector") {
				req.StreamSelector = opts.Selector
			}

			updated, err := client.UpdateStream(ctx, id, &req)
			if err != nil {
				return err
			}

			// The update already succeeded; a failed lookup only leaves the name empty.
			if destinationName == "" {
				if destination, err := client.GetDestination(ctx, updated.ArchiveDestinationID); err == nil {
					destinationName = destination.Name
				}
			}

			cmdio.Success(cmd.ErrOrStderr(), "Updated archive stream %q (id=%s)", updated.Name, updated.ID)
			return opts.IO.Encode(cmd.OutOrStdout(), StreamRow{Stream: *updated, ArchiveDestinationName: destinationName})
		},
	}
	opts.setup(cmd)
	return cmd
}

// streams delete

type streamsDeleteOpts struct {
	IO    cmdio.Options
	Force bool
}

func (o *streamsDeleteOpts) setup(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&o.Force, "force", false, "Skip confirmation prompt")
	o.IO.RegisterCustomCodec(cmdio.FormatText, &deleteTextCodec{noun: "archive stream"})
	o.IO.DefaultFormat(cmdio.FormatText)
	o.IO.BindFlags(cmd.Flags())
}

func (o *streamsDeleteOpts) Validate(cmd *cobra.Command, args []string) error {
	if err := rejectEmptyID(cmd, "stream", args[0]); err != nil {
		return err
	}
	return o.IO.Validate()
}

func (h *archiveHelper) streamsDeleteCommand() *cobra.Command {
	opts := &streamsDeleteOpts{}
	cmd := &cobra.Command{
		Use:   "delete ID",
		Short: "Delete an archive stream.",
		Long: "Delete an archive stream. Matching lines stop being archived; objects already written to the bucket are not deleted. " +
			"An Adaptive Logs drop rule paired with the stream is not deleted; remove it with 'gcx logs adaptive drop-rules delete' " +
			"if those logs should be stored again.",
		Example: `  # Delete a stream, after a confirmation prompt
  gcx logs archive streams delete 0190a7e2-5b1c-7d3e-9f40-1a2b3c4d5e6f

  # Delete without prompting
  gcx logs archive streams delete 0190a7e2-5b1c-7d3e-9f40-1a2b3c4d5e6f --force`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(cmd, args); err != nil {
				return err
			}
			id := canonicalID(args[0])

			ctx := cmd.Context()
			client, err := h.newClient(ctx)
			if err != nil {
				return err
			}

			if err := confirmDelete(cmd, opts.Force, "archive stream", id, func() (string, error) {
				stream, err := client.GetStream(ctx, id)
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("Delete archive stream %q (%s)?", stream.Name, stream.StreamSelector), nil
			}); err != nil {
				return err
			}

			if err := client.DeleteStream(ctx, id); err != nil {
				return err
			}

			changed := true
			result := cmdio.NewSingleMutation(actionDeleted, cmdio.MutationTarget{Kind: kindStream, ID: id})
			result.Changed = &changed
			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}
	opts.setup(cmd)
	return cmd
}

func rejectEmptyFlags(cmd *cobra.Command, values map[string]string) error {
	var empty []string
	for _, name := range []string{"destination", "name", "selector"} {
		if value, ok := values[name]; ok && strings.TrimSpace(value) == "" {
			empty = append(empty, "--"+name)
		}
	}
	if len(empty) == 0 {
		return nil
	}
	return gcxerrors.NewCommandUsageError(cmd, strings.Join(empty, ", ")+" cannot be empty", nil)
}
