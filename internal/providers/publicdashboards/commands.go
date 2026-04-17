package publicdashboards

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// GrafanaConfigLoader is the loader interface used by command constructors.
// It is an alias for RESTConfigLoader to keep existing test names stable.
type GrafanaConfigLoader = RESTConfigLoader

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

// encodeOne encodes a single TypedObject[PublicDashboard], wrapping it in a
// slice so the table codec can render it uniformly with list results.
func encodeOne(opts *cmdio.Options, w io.Writer, obj *adapter.TypedObject[PublicDashboard]) error {
	codec, err := opts.Codec()
	if err != nil {
		return err
	}
	if codec.Format() == "table" {
		return codec.Encode(w, []adapter.TypedObject[PublicDashboard]{*obj})
	}
	return opts.Encode(w, obj)
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
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			crud, _, err := NewTypedCRUD(ctx, loader)
			if err != nil {
				return err
			}

			typedObjs, err := crud.List(ctx, opts.Limit)
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), typedObjs)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ListTableCodec renders public dashboards as a tabular table.
// It consumes []adapter.TypedObject[PublicDashboard].
type ListTableCodec struct{}

// Format returns the output format name.
func (c *ListTableCodec) Format() format.Format { return "table" }

// Encode writes public dashboards to w as a table.
func (c *ListTableCodec) Encode(w io.Writer, v any) error {
	objs, ok := v.([]adapter.TypedObject[PublicDashboard])
	if !ok {
		return errors.New("invalid data type for table codec: expected []TypedObject[PublicDashboard]")
	}

	t := style.NewTable("DASHBOARD_UID", "PD_UID", "ACCESS_TOKEN", "ENABLED", "ANNOTATIONS", "TIME_SELECT", "SHARE")
	for _, obj := range objs {
		pd := obj.Spec
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

// Decode is not supported for table format.
func (c *ListTableCodec) Decode(_ io.Reader, _ any) error {
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
		Use:   "get PD_UID",
		Short: "Get a public dashboard by its UID.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			crud, _, err := NewTypedCRUD(ctx, loader)
			if err != nil {
				return err
			}

			obj, err := crud.Get(ctx, args[0])
			if err != nil {
				return err
			}

			return encodeOne(&opts.IO, cmd.OutOrStdout(), obj)
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
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			spec, err := readPublicDashboardSpec(opts.File, cmd.InOrStdin())
			if err != nil {
				return err
			}
			// The --dashboard-uid flag is authoritative for the parent dashboard.
			spec.DashboardUID = opts.DashboardUID

			ctx := cmd.Context()
			crud, restCfg, err := NewTypedCRUD(ctx, loader)
			if err != nil {
				return err
			}

			typedObj := &adapter.TypedObject[PublicDashboard]{Spec: *spec}
			typedObj.SetName(spec.UID)
			typedObj.SetNamespace(restCfg.Namespace)

			created, err := crud.Create(ctx, typedObj)
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

			spec, err := readPublicDashboardSpec(opts.File, cmd.InOrStdin())
			if err != nil {
				return err
			}
			// The --dashboard-uid flag is authoritative for the parent dashboard.
			spec.DashboardUID = opts.DashboardUID

			ctx := cmd.Context()
			name := args[0]

			crud, restCfg, err := NewTypedCRUD(ctx, loader)
			if err != nil {
				return err
			}

			typedObj := &adapter.TypedObject[PublicDashboard]{Spec: *spec}
			typedObj.SetName(name)
			typedObj.SetNamespace(restCfg.Namespace)

			updated, err := crud.Update(ctx, name, typedObj)
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

type deleteOpts struct{}

func (o *deleteOpts) setup(_ *pflag.FlagSet) {}

func newDeleteCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &deleteOpts{}
	cmd := &cobra.Command{
		Use:   "delete PD_UID",
		Short: "Delete a public dashboard config.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			name := args[0]

			crud, _, err := NewTypedCRUD(ctx, loader)
			if err != nil {
				return err
			}

			if err := crud.Delete(ctx, name); err != nil {
				return err
			}

			cmdio.Info(cmd.OutOrStdout(), "deleted public dashboard %s", strconv.Quote(name))
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}
