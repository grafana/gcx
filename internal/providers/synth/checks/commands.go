package checks

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/grafana/grafanactl/internal/format"
	cmdio "github.com/grafana/grafanactl/internal/output"
	"github.com/grafana/grafanactl/internal/providers/synth/smcfg"
	"github.com/grafana/grafanactl/internal/resources"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Commands returns the checks command group with CRUD subcommands.
func Commands(loader smcfg.StatusLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "checks",
		Short:   "Manage Synthetic Monitoring checks.",
		Aliases: []string{"check"},
	}
	cmd.AddCommand(
		newListCommand(loader),
		newGetCommand(loader),
		newPushCommand(loader),
		newPullCommand(loader),
		newDeleteCommand(loader),
		newStatusCommand(loader),
		newTimelineCommand(loader),
	)
	return cmd
}

// ---------------------------------------------------------------------------
// list
// ---------------------------------------------------------------------------

type listOpts struct {
	IO cmdio.Options
}

func (o *listOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &checkTableCodec{})
	o.IO.RegisterCustomCodec("wide", &checkWideTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func newListCommand(loader smcfg.Loader) *cobra.Command {
	opts := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List Synthetic Monitoring checks.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			baseURL, token, namespace, err := loader.LoadSMConfig(ctx)
			if err != nil {
				return err
			}

			client := NewClient(baseURL, token)

			checkList, err := client.List(ctx)
			if err != nil {
				return err
			}

			codec, err := opts.IO.Codec()
			if err != nil {
				return err
			}

			if codec.Format() == "table" || codec.Format() == "wide" {
				return codec.Encode(cmd.OutOrStdout(), checkList)
			}

			probeRefs, err := client.ListProbes(ctx)
			if err != nil {
				return fmt.Errorf("listing probes for name resolution: %w", err)
			}
			names := probeRefMap(probeRefs)

			var objs []unstructured.Unstructured
			for _, c := range checkList {
				res, err := ToResource(c, namespace, names)
				if err != nil {
					return fmt.Errorf("converting check %d: %w", c.ID, err)
				}
				objs = append(objs, res.ToUnstructured())
			}
			return codec.Encode(cmd.OutOrStdout(), objs)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type checkTableCodec struct{}

func (c *checkTableCodec) Format() format.Format { return "table" }

func (c *checkTableCodec) Encode(w io.Writer, v any) error {
	checkList, ok := v.([]Check)
	if !ok {
		return errors.New("invalid data type for table codec: expected []Check")
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tJOB\tTARGET\tTYPE")

	for _, c := range checkList {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\n",
			c.ID, c.Job, c.Target, c.Settings.CheckType())
	}

	return tw.Flush()
}

func (c *checkTableCodec) Decode(r io.Reader, v any) error {
	return errors.New("table format does not support decoding")
}

type checkWideTableCodec struct{}

func (c *checkWideTableCodec) Format() format.Format { return "wide" }

func (c *checkWideTableCodec) Encode(w io.Writer, v any) error {
	checkList, ok := v.([]Check)
	if !ok {
		return errors.New("invalid data type for wide codec: expected []Check")
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tJOB\tTARGET\tTYPE\tENABLED\tFREQ\tTIMEOUT\tPROBES")

	for _, c := range checkList {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%v\t%ds\t%ds\t%d\n",
			c.ID, c.Job, c.Target, c.Settings.CheckType(), c.Enabled,
			c.Frequency/1000, c.Timeout/1000, len(c.Probes))
	}

	return tw.Flush()
}

func (c *checkWideTableCodec) Decode(r io.Reader, v any) error {
	return errors.New("wide format does not support decoding")
}

// ---------------------------------------------------------------------------
// get
// ---------------------------------------------------------------------------

type getOpts struct {
	IO cmdio.Options
}

func (o *getOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &checkTableCodec{})
	o.IO.RegisterCustomCodec("wide", &checkWideTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func newGetCommand(loader smcfg.Loader) *cobra.Command {
	opts := &getOpts{}
	cmd := &cobra.Command{
		Use:   "get ID",
		Short: "Get a single Synthetic Monitoring check.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid check ID %q: must be a number", args[0])
			}

			baseURL, token, namespace, err := loader.LoadSMConfig(ctx)
			if err != nil {
				return err
			}

			client := NewClient(baseURL, token)

			c, err := client.Get(ctx, id)
			if err != nil {
				return err
			}

			codec, err := opts.IO.Codec()
			if err != nil {
				return err
			}

			if codec.Format() == "table" || codec.Format() == "wide" {
				return codec.Encode(cmd.OutOrStdout(), []Check{*c})
			}

			probeRefs, err := client.ListProbes(ctx)
			if err != nil {
				return fmt.Errorf("listing probes: %w", err)
			}
			names := probeRefMap(probeRefs)

			res, err := ToResource(*c, namespace, names)
			if err != nil {
				return fmt.Errorf("converting check: %w", err)
			}

			obj := res.ToUnstructured()
			return codec.Encode(cmd.OutOrStdout(), &obj)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// pull
// ---------------------------------------------------------------------------

type pullOpts struct {
	OutputDir string
}

func (o *pullOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&o.OutputDir, "output", "d", ".", "Directory to write check YAML files to")
}

func newPullCommand(loader smcfg.Loader) *cobra.Command {
	opts := &pullOpts{}
	cmd := &cobra.Command{
		Use:   "pull",
		Short: "Pull Synthetic Monitoring checks to disk.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			baseURL, token, namespace, err := loader.LoadSMConfig(ctx)
			if err != nil {
				return err
			}

			client := NewClient(baseURL, token)

			checkList, err := client.List(ctx)
			if err != nil {
				return err
			}

			probeRefs, err := client.ListProbes(ctx)
			if err != nil {
				return fmt.Errorf("listing probes: %w", err)
			}
			names := probeRefMap(probeRefs)

			outputDir := filepath.Join(opts.OutputDir, "checks")
			if err := os.MkdirAll(outputDir, 0755); err != nil {
				return fmt.Errorf("creating output directory %s: %w", outputDir, err)
			}

			yamlCodec := format.NewYAMLCodec()

			for _, c := range checkList {
				res, err := ToResource(c, namespace, names)
				if err != nil {
					return fmt.Errorf("converting check %d: %w", c.ID, err)
				}

				filePath := filepath.Join(outputDir, strconv.FormatInt(c.ID, 10)+".yaml")
				f, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
				if err != nil {
					return fmt.Errorf("opening file %s: %w", filePath, err)
				}

				obj := res.ToUnstructured()
				if err := yamlCodec.Encode(f, &obj); err != nil {
					f.Close()
					return fmt.Errorf("writing check %d: %w", c.ID, err)
				}
				f.Close()
			}

			cmdio.Success(cmd.OutOrStdout(), "Pulled %d checks to %s/", len(checkList), outputDir)
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// push
// ---------------------------------------------------------------------------

type pushOpts struct {
	DryRun bool
}

func (o *pushOpts) setup(flags *pflag.FlagSet) {
	flags.BoolVar(&o.DryRun, "dry-run", false, "Preview changes without applying them")
}

func newPushCommand(loader smcfg.Loader) *cobra.Command {
	opts := &pushOpts{}
	cmd := &cobra.Command{
		Use:   "push FILE...",
		Short: "Push Synthetic Monitoring checks from files.",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			baseURL, token, namespace, err := loader.LoadSMConfig(ctx)
			if err != nil {
				return err
			}

			client := NewClient(baseURL, token)

			// Fetch tenant ID and probe list once, shared across all files.
			tenant, err := client.GetTenant(ctx)
			if err != nil {
				return fmt.Errorf("fetching tenant: %w", err)
			}

			probeRefs, err := client.ListProbes(ctx)
			if err != nil {
				return fmt.Errorf("listing probes: %w", err)
			}

			probeIDMap := make(map[string]int64, len(probeRefs))
			for _, p := range probeRefs {
				probeIDMap[p.Name] = p.ID
			}

			yamlCodec := format.NewYAMLCodec()

			for _, filePath := range args {
				data, err := os.ReadFile(filePath)
				if err != nil {
					return fmt.Errorf("reading %s: %w", filePath, err)
				}

				var obj unstructured.Unstructured
				if err := yamlCodec.Decode(strings.NewReader(string(data)), &obj); err != nil {
					return fmt.Errorf("parsing %s: %w", filePath, err)
				}

				res, err := resources.FromUnstructured(&obj)
				if err != nil {
					return fmt.Errorf("building resource from %s: %w", filePath, err)
				}

				spec, id, err := FromResource(res)
				if err != nil {
					return fmt.Errorf("converting resource from %s: %w", filePath, err)
				}

				// Set namespace from context if missing.
				if res.Raw.GetNamespace() == "" {
					obj.SetNamespace(namespace)
				}

				// Resolve probe names to IDs.
				probeIDs := make([]int64, 0, len(spec.Probes))
				for _, name := range spec.Probes {
					pid, ok := probeIDMap[name]
					if !ok {
						return fmt.Errorf("probe %q not found (file %s)", name, filePath)
					}
					probeIDs = append(probeIDs, pid)
				}

				if opts.DryRun {
					action := "create"
					if id > 0 {
						action = "update"
					}
					cmdio.Info(cmd.OutOrStdout(), "[dry-run] Would %s check %q (id=%d)", action, spec.Job, id)
					continue
				}

				apiCheck := SpecToCheck(spec, id, tenant.ID, probeIDs)

				if id == 0 {
					created, err := client.Create(ctx, apiCheck)
					if err != nil {
						return fmt.Errorf("creating check %q: %w", spec.Job, err)
					}
					cmdio.Success(cmd.OutOrStdout(), "Created check %q (id=%d)", spec.Job, created.ID)

					// Update the local YAML file with the server-assigned ID.
					if err := updateNameInFile(filePath, strconv.FormatInt(created.ID, 10)); err != nil {
						cmdio.Warning(cmd.OutOrStdout(), "Check created but could not update %s: %v", filePath, err)
					}
				} else {
					if _, err := client.Update(ctx, apiCheck); err != nil {
						return fmt.Errorf("updating check %d: %w", id, err)
					}
					cmdio.Success(cmd.OutOrStdout(), "Updated check %q (id=%d)", spec.Job, id)
				}
			}
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// updateNameInFile rewrites metadata.name in a YAML file to newName.
// This is used after a create to persist the server-assigned numeric ID.
func updateNameInFile(filePath, newName string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	lines := strings.Split(string(data), "\n")
	inMetadata := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "metadata:" {
			inMetadata = true
			continue
		}
		if inMetadata {
			if strings.HasPrefix(trimmed, "name:") {
				lines[i] = strings.Replace(line, trimmed, "name: "+strconv.Quote(newName), 1)
				break
			}
			// Stop searching if we leave the metadata block (new top-level key).
			if len(trimmed) > 0 && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
				break
			}
		}
	}

	return os.WriteFile(filePath, []byte(strings.Join(lines, "\n")), 0600)
}

// ---------------------------------------------------------------------------
// delete
// ---------------------------------------------------------------------------

type deleteOpts struct {
	Force bool
}

func (o *deleteOpts) setup(flags *pflag.FlagSet) {
	flags.BoolVarP(&o.Force, "force", "f", false, "Skip confirmation prompt")
}

func newDeleteCommand(loader smcfg.Loader) *cobra.Command {
	opts := &deleteOpts{}
	cmd := &cobra.Command{
		Use:   "delete ID...",
		Short: "Delete Synthetic Monitoring checks.",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			if !opts.Force {
				fmt.Fprintf(cmd.OutOrStdout(), "Delete %d check(s)? [y/N] ", len(args))
				reader := bufio.NewReader(cmd.InOrStdin())
				answer, err := reader.ReadString('\n')
				if err != nil {
					return fmt.Errorf("reading confirmation: %w", err)
				}
				answer = strings.TrimSpace(strings.ToLower(answer))
				if answer != "y" && answer != "yes" {
					cmdio.Info(cmd.OutOrStdout(), "Aborted.")
					return nil
				}
			}

			baseURL, token, _, err := loader.LoadSMConfig(ctx)
			if err != nil {
				return err
			}

			client := NewClient(baseURL, token)

			for _, arg := range args {
				id, err := strconv.ParseInt(arg, 10, 64)
				if err != nil {
					return fmt.Errorf("invalid check ID %q: must be a number", arg)
				}

				if err := client.Delete(ctx, id); err != nil {
					return fmt.Errorf("deleting check %d: %w", id, err)
				}
				cmdio.Success(cmd.OutOrStdout(), "Deleted check %d", id)
			}
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// probeRefMap converts a []ProbeRef to a map of ID → name.
func probeRefMap(refs []ProbeRef) map[int64]string {
	m := make(map[int64]string, len(refs))
	for _, p := range refs {
		m[p.ID] = p.Name
	}
	return m
}
