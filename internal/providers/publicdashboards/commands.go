package publicdashboards

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// GrafanaConfigLoader can load a NamespacedRESTConfig from the active context.
type GrafanaConfigLoader interface {
	LoadGrafanaConfig(ctx context.Context) (config.NamespacedRESTConfig, error)
}

func boolLabel(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

// readPublicDashboardSpec reads a JSON public dashboard spec from path, or from
// stdin when path is "-".
func readPublicDashboardSpec(path string, stdin io.Reader) (*PublicDashboard, error) {
	var (
		data []byte
		err  error
	)
	if path == "-" {
		data, err = io.ReadAll(stdin)
		if err != nil {
			return nil, fmt.Errorf("reading stdin: %w", err)
		}
	} else {
		data, err = os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", path, err)
		}
	}

	var pd PublicDashboard
	if err := json.Unmarshal(data, &pd); err != nil {
		return nil, fmt.Errorf("parsing public dashboard spec: %w", err)
	}
	return &pd, nil
}

// encodeOne encodes a single PublicDashboard, wrapping it in a slice so the
// table codec can render it uniformly with list results.
func encodeOne(opts *cmdio.Options, w io.Writer, pd *PublicDashboard) error {
	codec, err := opts.Codec()
	if err != nil {
		return err
	}
	if codec.Format() == "table" {
		return codec.Encode(w, []PublicDashboard{*pd})
	}
	return opts.Encode(w, pd)
}

// ---- list ----

type listOpts struct {
	IO    cmdio.Options
	Limit int64
}

func (o *listOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &ListTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.Int64Var(&o.Limit, "limit", 50, "Maximum number of items to return (0 for unlimited)")
}

func newListCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all public dashboards.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			client, err := NewClient(restCfg)
			if err != nil {
				return err
			}

			list, err := client.List(ctx)
			if err != nil {
				return err
			}

			list = adapter.TruncateSlice(list, opts.Limit)
			return opts.IO.Encode(cmd.OutOrStdout(), list)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ListTableCodec renders public dashboards as a tabular table.
type ListTableCodec struct{}

func (c *ListTableCodec) Format() format.Format { return "table" }

func (c *ListTableCodec) Encode(w io.Writer, v any) error {
	list, ok := v.([]PublicDashboard)
	if !ok {
		return errors.New("invalid data type for table codec: expected []PublicDashboard")
	}

	t := style.NewTable("DASHBOARD_UID", "PD_UID", "ACCESS_TOKEN", "ENABLED", "ANNOTATIONS", "TIME_SELECT", "SHARE")
	for _, pd := range list {
		t.Row(
			pd.DashboardUID,
			pd.UID,
			pd.AccessToken,
			boolLabel(pd.IsEnabled),
			boolLabel(pd.AnnotationsEnabled),
			boolLabel(pd.TimeSelectionEnabled),
			pd.Share,
		)
	}
	return t.Render(w)
}

func (c *ListTableCodec) Decode(r io.Reader, v any) error {
	return errors.New("table format does not support decoding")
}

// ---- get ----

type getOpts struct {
	IO cmdio.Options
}

func (o *getOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &ListTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func newGetCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &getOpts{}
	cmd := &cobra.Command{
		Use:   "get DASHBOARD_UID",
		Short: "Get the public dashboard config for a dashboard.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			client, err := NewClient(restCfg)
			if err != nil {
				return err
			}

			pd, err := client.Get(ctx, args[0])
			if err != nil {
				return err
			}

			return encodeOne(&opts.IO, cmd.OutOrStdout(), pd)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---- create ----

type createOpts struct {
	IO           cmdio.Options
	DashboardUID string
	File         string
}

func (o *createOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &ListTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.DashboardUID, "dashboard-uid", "", "Parent dashboard UID (required)")
	flags.StringVarP(&o.File, "file", "f", "", "File containing the public dashboard spec (JSON), or '-' for stdin (required)")
}

func newCreateCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &createOpts{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a public dashboard config from a JSON file.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			pd, err := readPublicDashboardSpec(opts.File, cmd.InOrStdin())
			if err != nil {
				return err
			}

			ctx := cmd.Context()
			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			client, err := NewClient(restCfg)
			if err != nil {
				return err
			}

			created, err := client.Create(ctx, opts.DashboardUID, pd)
			if err != nil {
				return err
			}

			return encodeOne(&opts.IO, cmd.OutOrStdout(), created)
		},
	}
	opts.setup(cmd.Flags())
	_ = cmd.MarkFlagRequired("dashboard-uid")
	_ = cmd.MarkFlagRequired("file")
	return cmd
}

// ---- update ----

type updateOpts struct {
	IO           cmdio.Options
	DashboardUID string
	File         string
}

func (o *updateOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &ListTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.DashboardUID, "dashboard-uid", "", "Parent dashboard UID (required)")
	flags.StringVarP(&o.File, "file", "f", "", "File containing the public dashboard spec (JSON), or '-' for stdin (required)")
}

func newUpdateCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &updateOpts{}
	cmd := &cobra.Command{
		Use:   "update PD_UID",
		Short: "Update a public dashboard config from a JSON file.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			pd, err := readPublicDashboardSpec(opts.File, cmd.InOrStdin())
			if err != nil {
				return err
			}

			ctx := cmd.Context()
			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			client, err := NewClient(restCfg)
			if err != nil {
				return err
			}

			updated, err := client.Update(ctx, opts.DashboardUID, args[0], pd)
			if err != nil {
				return err
			}

			return encodeOne(&opts.IO, cmd.OutOrStdout(), updated)
		},
	}
	opts.setup(cmd.Flags())
	_ = cmd.MarkFlagRequired("dashboard-uid")
	_ = cmd.MarkFlagRequired("file")
	return cmd
}

// ---- delete ----

type deleteOpts struct {
	DashboardUID string
}

func (o *deleteOpts) setup(flags *pflag.FlagSet) {
	flags.StringVar(&o.DashboardUID, "dashboard-uid", "", "Parent dashboard UID (required)")
}

func newDeleteCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &deleteOpts{}
	cmd := &cobra.Command{
		Use:   "delete PD_UID",
		Short: "Delete a public dashboard config.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			client, err := NewClient(restCfg)
			if err != nil {
				return err
			}

			if err := client.Delete(ctx, opts.DashboardUID, args[0]); err != nil {
				return err
			}

			cmdio.Info(cmd.OutOrStdout(), "deleted public dashboard %s", strconv.Quote(args[0]))
			return nil
		},
	}
	opts.setup(cmd.Flags())
	_ = cmd.MarkFlagRequired("dashboard-uid")
	return cmd
}
