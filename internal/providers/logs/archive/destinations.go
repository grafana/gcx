package archive

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/grafana/gcx/internal/docs"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
)

func (h *archiveHelper) destinationsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "destinations",
		Short: "Manage archive destinations.",
		Long: "Manage archive destinations: the buckets archived logs are written to.\n\n" +
			"Destinations cannot be changed after they are created. To change one, delete it and create it again; " +
			"deleting a destination also deletes its streams.",
	}
	cmd.AddCommand(
		h.destinationsListCommand(),
		h.destinationsGetCommand(),
		h.destinationsCreateCommand(),
		h.destinationsDeleteCommand(),
	)
	return cmd
}

func destinationsTable() cmdio.Table[Destination] {
	return cmdio.Table[Destination]{
		Columns: []cmdio.Column[Destination]{
			{Header: "ID", Content: func(d Destination) string { return d.ID }},
			{Header: "NAME", Content: func(d Destination) string { return d.Name }},
			{Header: "TYPE", Content: func(d Destination) string { return d.Type }},
			{Header: "LOCATION", Content: func(d Destination) string { return cmdio.OrDash(destinationLocation(d.StorageConfig)) }},
			{Header: "REGION", Content: func(d Destination) string { return cmdio.OrDash(destinationRegion(d.StorageConfig)) }},
			{Header: "PATH PREFIX", Content: func(d Destination) string { return cmdio.OrDash(destinationPathPrefix(d.StorageConfig)) }},
			{Header: "DESCRIPTION", Visible: cmdio.WideOnly, Content: func(d Destination) string { return cmdio.OrDash(d.Description) }},
			{Header: "CREATED AT", Visible: cmdio.WideOnly, Content: func(d Destination) string { return d.CreatedAt }},
		},
	}
}

func destinationLocation(s StorageConfig) string {
	switch {
	case s.S3 != nil:
		return s.S3.BucketName
	case s.GCS != nil:
		return s.GCS.BucketName
	case s.Azure != nil:
		return s.Azure.StorageAccount + "/" + s.Azure.Container
	default:
		return ""
	}
}

func destinationRegion(s StorageConfig) string {
	if s.S3 != nil {
		return s.S3.Region
	}
	return ""
}

func destinationPathPrefix(s StorageConfig) string {
	switch {
	case s.S3 != nil:
		return s.S3.PathPrefix
	case s.GCS != nil:
		return s.GCS.PathPrefix
	case s.Azure != nil:
		return s.Azure.PathPrefix
	default:
		return ""
	}
}

// destinations list

type destinationsListOpts struct {
	IO cmdio.Options
}

func (o *destinationsListOpts) setup(cmd *cobra.Command) {
	cmdio.RegisterTable(&o.IO, destinationsTable())
	o.IO.DefaultFormat(cmdio.FormatTable)
	o.IO.BindFlags(cmd.Flags())
}

func (o *destinationsListOpts) Validate() error {
	return o.IO.Validate()
}

func (h *archiveHelper) destinationsListCommand() *cobra.Command {
	opts := &destinationsListOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List archive destinations.",
		Example: `  # List archive destinations
  gcx logs archive destinations list

  # Include descriptions and creation times
  gcx logs archive destinations list -o wide`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			client, err := h.newClient(ctx)
			if err != nil {
				return err
			}

			destinations, err := client.ListDestinations(ctx)
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), destinations)
		},
	}
	opts.setup(cmd)
	return cmd
}

// destinations get

type destinationsGetOpts struct {
	IO cmdio.Options
}

func (o *destinationsGetOpts) setup(cmd *cobra.Command) {
	registerSingleTable(&o.IO, destinationsTable())
	o.IO.DefaultFormat(cmdio.FormatTable)
	o.IO.BindFlags(cmd.Flags())
}

func (o *destinationsGetOpts) Validate(cmd *cobra.Command, args []string) error {
	if err := rejectEmptyID(cmd, "destination", args[0]); err != nil {
		return err
	}
	return o.IO.Validate()
}

func (h *archiveHelper) destinationsGetCommand() *cobra.Command {
	opts := &destinationsGetOpts{}
	cmd := &cobra.Command{
		Use:   "get ID",
		Short: "Show one archive destination.",
		Example: `  # Show a destination
  gcx logs archive destinations get 0190a7e1-9c4d-7a2b-8e3f-5d6c7b8a9f01

  # Save a destination's definition, for example to recreate it
  gcx logs archive destinations get 0190a7e1-9c4d-7a2b-8e3f-5d6c7b8a9f01 -o yaml > destination.yaml`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(cmd, args); err != nil {
				return err
			}

			ctx := cmd.Context()
			client, err := h.newClient(ctx)
			if err != nil {
				return err
			}

			destination, err := client.GetDestination(ctx, canonicalID(args[0]))
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), *destination)
		},
	}
	opts.setup(cmd)
	return cmd
}

// destinations create

//nolint:gochecknoglobals // Static flag list.
var destinationFieldFlags = []string{
	"name", "description", "type", "bucket", "region", "path-prefix",
	"storage-account", "container", "azure-tenant-id",
}

//nolint:gochecknoglobals // Static lookup table.
var destinationTypeFlags = map[string][]string{
	TypeS3:    {"bucket", "region"},
	TypeGCS:   {"bucket"},
	TypeAzure: {"storage-account", "container", "azure-tenant-id"},
}

type destinationsCreateOpts struct {
	IO             cmdio.Options
	File           string
	DryRun         bool
	Name           string
	Description    string
	Type           string
	Bucket         string
	Region         string
	PathPrefix     string
	StorageAccount string
	Container      string
	AzureTenantID  string
}

func (o *destinationsCreateOpts) setup(cmd *cobra.Command) {
	flags := cmd.Flags()
	flags.StringVarP(&o.File, "filename", "f", "", "File containing the destination definition (YAML or JSON; use - for stdin). Cannot be combined with the field flags")
	flags.BoolVar(&o.DryRun, "dry-run", false, "Check the bucket with the archive API without creating the destination")
	flags.StringVar(&o.Name, "name", "", "Destination name, unique in the stack")
	flags.StringVar(&o.Description, "description", "", "Destination description")
	flags.StringVar(&o.Type, "type", "", "Destination type: s3, gcs or azure")
	flags.StringVar(&o.Bucket, "bucket", "", "Bucket name (s3, gcs)")
	flags.StringVar(&o.Region, "region", "", "Bucket region (s3)")
	flags.StringVar(&o.PathPrefix, "path-prefix", "", "Prefix for archived objects in the bucket")
	flags.StringVar(&o.StorageAccount, "storage-account", "", "Storage account name (azure)")
	flags.StringVar(&o.Container, "container", "", "Blob container name (azure)")
	flags.StringVar(&o.AzureTenantID, "azure-tenant-id", "", "Azure AD tenant ID that owns the storage account (azure)")
	o.IO.DefaultFormat("json")
	o.IO.BindFlags(flags)
}

func (o *destinationsCreateOpts) Validate(cmd *cobra.Command) error {
	if err := o.IO.Validate(); err != nil {
		return err
	}
	if o.File == "" {
		return o.checkTypeFlags(cmd)
	}
	var changed []string
	for _, name := range destinationFieldFlags {
		if cmd.Flags().Changed(name) {
			changed = append(changed, "--"+name)
		}
	}
	if len(changed) > 0 {
		return gcxerrors.NewCommandUsageError(cmd,
			"--filename cannot be combined with "+strings.Join(changed, ", "), nil)
	}
	return nil
}

func (o *destinationsCreateOpts) request(cmd *cobra.Command) (*DestinationCreate, error) {
	var req *DestinationCreate
	if o.File != "" {
		var err error
		req, err = o.requestFromFile(cmd)
		if err != nil {
			return nil, err
		}
	} else {
		req = o.requestFromFlags()
	}

	if err := validateDestinationCreate(req); err != nil {
		return nil, gcxerrors.NewCommandUsageError(cmd, err.Error(), err)
	}
	return req, nil
}

func (o *destinationsCreateOpts) requestFromFile(cmd *cobra.Command) (*DestinationCreate, error) {
	data, err := readInputFile(o.File, cmd.InOrStdin())
	if err != nil {
		return nil, err
	}
	var spec Destination
	if err := format.NewYAMLCodec().Decode(bytes.NewReader(data), &spec); err != nil {
		return nil, fmt.Errorf("decoding destination file: %w", err)
	}
	// The codec decodes only the first document.
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var doc any
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("decoding destination file: %w", err)
	}
	if err := dec.Decode(&doc); !errors.Is(err, io.EOF) {
		return nil, errors.New("the file must contain exactly one destination")
	}
	return &DestinationCreate{
		Name:          spec.Name,
		Description:   spec.Description,
		Type:          spec.Type,
		StorageConfig: spec.StorageConfig,
	}, nil
}

func (o *destinationsCreateOpts) checkTypeFlags(cmd *cobra.Command) error {
	allowed, ok := destinationTypeFlags[o.Type]
	if !ok {
		return nil // validateDestinationCreate reports the bad type
	}
	var wrong []string
	for _, typeFlags := range destinationTypeFlags {
		for _, name := range typeFlags {
			if cmd.Flags().Changed(name) && !slices.Contains(allowed, name) && !slices.Contains(wrong, "--"+name) {
				wrong = append(wrong, "--"+name)
			}
		}
	}
	if len(wrong) == 0 {
		return nil
	}
	slices.Sort(wrong)
	return gcxerrors.NewCommandUsageError(cmd,
		fmt.Sprintf("%s cannot be used with --type %s", strings.Join(wrong, ", "), o.Type), nil)
}

func (o *destinationsCreateOpts) requestFromFlags() *DestinationCreate {
	req := &DestinationCreate{
		Name:        o.Name,
		Description: o.Description,
		Type:        o.Type,
	}
	switch o.Type {
	case TypeS3:
		req.StorageConfig.S3 = &S3Config{BucketName: o.Bucket, Region: o.Region, PathPrefix: o.PathPrefix}
	case TypeGCS:
		req.StorageConfig.GCS = &GCSConfig{BucketName: o.Bucket, PathPrefix: o.PathPrefix}
	case TypeAzure:
		req.StorageConfig.Azure = &AzureConfig{
			StorageAccount: o.StorageAccount,
			Container:      o.Container,
			PathPrefix:     o.PathPrefix,
			TenantID:       o.AzureTenantID,
		}
	}
	return req
}

func validateDestinationCreate(d *DestinationCreate) error {
	var problems []string
	if strings.TrimSpace(d.Name) == "" {
		problems = append(problems, "a name is required")
	}

	s := d.StorageConfig
	otherSet := false
	switch d.Type {
	case TypeS3:
		if s.S3 == nil || strings.TrimSpace(s.S3.BucketName) == "" || strings.TrimSpace(s.S3.Region) == "" {
			problems = append(problems, "an s3 destination needs a bucket name and a region")
		}
		otherSet = s.GCS != nil || s.Azure != nil
	case TypeGCS:
		if s.GCS == nil || strings.TrimSpace(s.GCS.BucketName) == "" {
			problems = append(problems, "a gcs destination needs a bucket name")
		}
		otherSet = s.S3 != nil || s.Azure != nil
	case TypeAzure:
		if s.Azure == nil || strings.TrimSpace(s.Azure.StorageAccount) == "" ||
			strings.TrimSpace(s.Azure.Container) == "" || strings.TrimSpace(s.Azure.TenantID) == "" {
			problems = append(problems, "an azure destination needs a storage account, a container and an Azure tenant ID")
		}
		otherSet = s.S3 != nil || s.GCS != nil
	default:
		problems = append(problems, fmt.Sprintf("type must be one of %s, %s or %s, not %q", TypeS3, TypeGCS, TypeAzure, d.Type))
	}
	if otherSet {
		problems = append(problems, "storage_config must set only the provider matching the type")
	}

	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

func (h *archiveHelper) destinationsCreateCommand() *cobra.Command {
	opts := &destinationsCreateOpts{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an archive destination.",
		Long: "Create an archive destination from flags or from a file (--filename).\n\n" +
			"Before saving, the archive API checks the bucket: it must be on the cell's cloud provider and, for S3 and GCS, " +
			"in the cell's region; it must contain an empty grafana-archive-ownership object under " +
			"<path-prefix>/tenant=<logs-instance-id>/; and the Grafana principal must be able to list, read and write it. " +
			"Run 'gcx logs archive principal get' for the provider, region and principal, and see " +
			docs.AdaptiveLogsArchive + " for the exact permissions each provider needs. " +
			"Use --dry-run to run the check without creating anything.\n\n" +
			"Destinations cannot be updated; delete and recreate one to change it.",
		Example: `  # Check an S3 bucket without creating anything
  gcx logs archive destinations create --name prod-archive --type s3 \
    --bucket acme-logs --region us-east-1 --path-prefix grafana --dry-run

  # Create an S3 destination
  gcx logs archive destinations create --name prod-archive --type s3 \
    --bucket acme-logs --region us-east-1 --path-prefix grafana

  # Create a GCS destination
  gcx logs archive destinations create --name prod-archive --type gcs --bucket acme-logs

  # Create an Azure destination
  gcx logs archive destinations create --name prod-archive --type azure \
    --storage-account acmelogs --container archive --azure-tenant-id 00000000-0000-0000-0000-000000000000

  # Create a destination from a file, such as the output of 'destinations get -o yaml'
  gcx logs archive destinations create -f destination.yaml`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(cmd); err != nil {
				return err
			}
			req, err := opts.request(cmd)
			if err != nil {
				return err
			}

			ctx := cmd.Context()
			client, err := h.newClient(ctx)
			if err != nil {
				return err
			}

			if opts.DryRun {
				if err := client.ValidateDestination(ctx, req); err != nil {
					return explainDestinationCheck(err)
				}
				cmdio.Success(cmd.ErrOrStderr(), "Destination %q passed the bucket check (dry run: nothing was created)", req.Name)
				changed := false
				result := cmdio.NewSingleMutation(actionValidated, cmdio.MutationTarget{Kind: kindDestination, Name: req.Name})
				result.Changed = &changed
				result.DryRun = true
				return opts.IO.Encode(cmd.OutOrStdout(), result)
			}

			created, err := client.CreateDestination(ctx, req)
			if err != nil {
				return explainDestinationCheck(err)
			}

			cmdio.Success(cmd.ErrOrStderr(), "Created archive destination %q (id=%s)", created.Name, created.ID)
			return opts.IO.Encode(cmd.OutOrStdout(), *created)
		},
	}
	opts.setup(cmd)
	return cmd
}

// destinations delete

type destinationsDeleteOpts struct {
	IO    cmdio.Options
	Force bool
}

func (o *destinationsDeleteOpts) setup(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&o.Force, "force", false, "Skip confirmation prompt")
	o.IO.RegisterCustomCodec(cmdio.FormatText, &deleteTextCodec{noun: "archive destination"})
	o.IO.DefaultFormat(cmdio.FormatText)
	o.IO.BindFlags(cmd.Flags())
}

func (o *destinationsDeleteOpts) Validate(cmd *cobra.Command, args []string) error {
	if err := rejectEmptyID(cmd, "destination", args[0]); err != nil {
		return err
	}
	return o.IO.Validate()
}

func (h *archiveHelper) destinationsDeleteCommand() *cobra.Command {
	opts := &destinationsDeleteOpts{}
	cmd := &cobra.Command{
		Use:   "delete ID",
		Short: "Delete an archive destination and its streams.",
		Long: "Delete an archive destination. Its streams are deleted with it, and pending replays from it will fail. " +
			"Objects already written to the bucket are not deleted, and neither are Adaptive Logs drop rules that " +
			"were paired with its streams: logs those rules drop are then neither stored nor archived.",
		Example: `  # Delete a destination, after a confirmation prompt
  gcx logs archive destinations delete 0190a7e1-9c4d-7a2b-8e3f-5d6c7b8a9f01

  # Delete without prompting
  gcx logs archive destinations delete 0190a7e1-9c4d-7a2b-8e3f-5d6c7b8a9f01 --force`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(cmd, args); err != nil {
				return err
			}

			ctx := cmd.Context()
			client, err := h.newClient(ctx)
			if err != nil {
				return err
			}

			destination, err := client.GetDestination(ctx, canonicalID(args[0]))
			if err != nil {
				return err
			}

			if err := confirmDelete(cmd, opts.Force, "archive destination", destination.Name, func() (string, error) {
				return destinationDeletePrompt(ctx, client, destination)
			}); err != nil {
				return err
			}

			if err := client.DeleteDestination(ctx, destination.ID); err != nil {
				return err
			}

			changed := true
			result := cmdio.NewSingleMutation(actionDeleted, cmdio.MutationTarget{Kind: kindDestination, ID: destination.ID, Name: destination.Name})
			result.Changed = &changed
			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}
	opts.setup(cmd)
	return cmd
}

func destinationDeletePrompt(ctx context.Context, client *Client, d *Destination) (string, error) {
	streams, err := client.ListStreams(ctx)
	if err != nil {
		return "", err
	}
	count := 0
	for _, s := range streams {
		if s.ArchiveDestinationID == d.ID {
			count++
		}
	}
	noun := "streams"
	if count == 1 {
		noun = "stream"
	}
	return fmt.Sprintf("Delete archive destination %q? Its %d %s will be deleted too, and pending replays from it will fail.", d.Name, count, noun), nil
}
