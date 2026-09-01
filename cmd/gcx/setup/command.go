package setup

import (
	"context"
	"errors"
	"fmt"
	"io"

	fleetbase "github.com/grafana/gcx/internal/fleet"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	instrum "github.com/grafana/gcx/internal/providers/instrumentation"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/client-go/rest"
)

// Command returns the setup command area for onboarding and configuring
// Grafana Cloud products.
func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Onboard and configure Grafana Cloud products.",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			// Chain the root's PersistentPreRun (root command sets up logging/context).
			if root := cmd.Root(); root != nil && root.PersistentPreRun != nil {
				root.PersistentPreRun(cmd, args)
			}
		},
	}

	loader := &providers.ConfigLoader{}
	loader.BindFlags(cmd.PersistentFlags())

	cmd.AddCommand(newStatusCommand(loader))

	return cmd
}

// Discriminators for the aggregated setup status document, so consumers can
// dispatch on the shape without heuristics.
const (
	setupStatusType          = "gcx.setup.status"
	setupStatusSchemaVersion = "1"
)

// setupStatus is the finite result of `gcx setup status`: one row per
// Grafana Cloud product with its enablement and health.
type setupStatus struct {
	Type          string               `json:"type" yaml:"type"`
	SchemaVersion string               `json:"schema_version" yaml:"schema_version"`
	Products      []setupProductStatus `json:"products" yaml:"products"`
}

// setupProductStatus is the aggregated status of a single product.
type setupProductStatus struct {
	Product string `json:"product" yaml:"product"`
	Enabled bool   `json:"enabled" yaml:"enabled"`
	Health  string `json:"health" yaml:"health"`
	Details string `json:"details,omitempty" yaml:"details,omitempty"`
}

// newSetupStatus returns a setupStatus with the discriminators set.
func newSetupStatus(products []setupProductStatus) setupStatus {
	return setupStatus{
		Type:          setupStatusType,
		SchemaVersion: setupStatusSchemaVersion,
		Products:      products,
	}
}

type setupStatusOpts struct {
	IO cmdio.Options
}

func (o *setupStatusOpts) setup(flags *pflag.FlagSet) {
	// The status result is a setupStatus document through the codec system:
	// the default text codec renders the familiar fixed table; agent mode and
	// explicit -o json/yaml get the structured document.
	o.IO.RegisterCustomCodec("text", &setupStatusTextCodec{})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
}

func (o *setupStatusOpts) Validate() error {
	return o.IO.Validate()
}

func newStatusCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &setupStatusOpts{}
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show aggregated setup status across all products.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			// Fleet Management reaches its API through the collector app plugin
			// proxy. Report the plugin and the permissions first, so a user
			// learns the cause without a failed RPC.
			cfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return fmt.Errorf("setup: %w", err)
			}
			httpClient, err := rest.HTTPClientFor(&cfg.Config)
			if err != nil {
				return fmt.Errorf("setup: %w", err)
			}
			state, err := checkCollectorApp(ctx, cfg, httpClient)
			if err != nil {
				return fmt.Errorf("setup: %w", err)
			}

			products := []setupProductStatus{state.row()}

			if !state.mayServe() {
				products = append(products, setupProductStatus{
					Product: "instrumentation",
					Enabled: false,
					Health:  "unknown",
					Details: "needs the " + collectorAppID + " plugin",
				})
				return opts.IO.Encode(cmd.OutOrStdout(), newSetupStatus(products))
			}

			// A failed instrumentation call must not discard the Fleet
			// Management row: that row names the cause. The command reports
			// the failure in the instrumentation row, then carries the exit
			// code with EmittedError, so stdout holds exactly one document.
			row, statusErr := instrumentationRow(ctx, loader)
			products = append(products, row)
			if err := opts.IO.Encode(cmd.OutOrStdout(), newSetupStatus(products)); err != nil {
				return err
			}
			if statusErr != nil {
				return gcxerrors.NewEmittedError(gcxerrors.ExitPartialFailure, statusErr)
			}
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// instrumentationRow reads the Instrumentation Hub state through the collector
// app plugin proxy. It returns a product row in every case: the row carries the
// reason when the call fails, together with the error for the exit code.
func instrumentationRow(ctx context.Context, loader *providers.ConfigLoader) (setupProductStatus, error) {
	r, err := fleetbase.LoadClientWithStack(ctx, loader)
	if err != nil {
		return unknownInstrumentationRow(err), fmt.Errorf("setup: %w", err)
	}

	monResp, err := instrum.NewClient(r.Client).RunK8sMonitoring(ctx, instrum.PromHeadersFromStack(r.Stack))
	if err != nil {
		return unknownInstrumentationRow(err), fmt.Errorf("setup: %w", err)
	}

	return setupProductStatus{
		Product: "instrumentation",
		Enabled: len(monResp.Clusters) > 0,
		Health:  "healthy",
		Details: fmt.Sprintf("%d clusters", len(monResp.Clusters)),
	}, nil
}

// unknownInstrumentationRow renders a failed instrumentation check as a product
// row, so the user sees the cause next to the Fleet Management row.
func unknownInstrumentationRow(err error) setupProductStatus {
	return setupProductStatus{
		Product: "instrumentation",
		Enabled: false,
		Health:  "unknown",
		Details: "the check failed: " + err.Error(),
	}
}

// setupStatusTextCodec is the human "text" codec for setupStatus values: it
// renders exactly the PRODUCT/ENABLED/HEALTH/DETAILS table setup status has
// always printed, so default human stdout stays byte-identical to the
// pre-codec output.
type setupStatusTextCodec struct{}

func (c *setupStatusTextCodec) Format() format.Format { return "text" }

func (c *setupStatusTextCodec) Decode(io.Reader, any) error {
	return errors.New("setup status text codec does not support decoding")
}

func (c *setupStatusTextCodec) Encode(w io.Writer, value any) error {
	status, ok := value.(setupStatus)
	if !ok {
		return errors.New("invalid data type for setup status text codec: expected setupStatus")
	}

	t := style.NewTable("PRODUCT", "ENABLED", "HEALTH", "DETAILS")
	for _, p := range status.Products {
		enabled := "no"
		if p.Enabled {
			enabled = "yes"
		}
		t.Row(p.Product, enabled, p.Health, p.Details)
	}
	return t.Render(w)
}
