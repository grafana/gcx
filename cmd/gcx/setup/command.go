package setup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/grafana/gcx/cmd/gcx/fail"
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
	Error         *setupStatusError    `json:"error,omitempty" yaml:"error,omitempty"`
}

// setupProductStatus is the aggregated status of a single product.
type setupProductStatus struct {
	Product string `json:"product" yaml:"product"`
	Enabled *bool  `json:"enabled" yaml:"enabled"` // nil means the check could not determine enablement.
	Health  string `json:"health" yaml:"health"`
	Details string `json:"details,omitempty" yaml:"details,omitempty"`
}

// setupStatusError keeps the converted failure in the same document as the rows.
type setupStatusError struct {
	Summary     string   `json:"summary" yaml:"summary"`
	ExitCode    int      `json:"exitCode" yaml:"exitCode"`
	Details     string   `json:"details,omitempty" yaml:"details,omitempty"`
	Suggestions []string `json:"suggestions,omitempty" yaml:"suggestions,omitempty"`
	DocsLink    string   `json:"docsLink,omitempty" yaml:"docsLink,omitempty"`
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
			state, err := fleetbase.CheckCollectorApp(ctx, cfg.Host, httpClient)
			if err != nil {
				return fmt.Errorf("setup: %w", err)
			}

			products := []setupProductStatus{collectorAppRow(state)}

			var row setupProductStatus
			var statusErr error
			if !state.MayServe() {
				statusErr = &gcxerrors.DetailedError{
					Summary:     gcxerrors.SummaryEndpointNotAvailable,
					Details:     "needs the " + fleetbase.CollectorAppID + " plugin",
					Suggestions: []string{"Enable the " + fleetbase.CollectorAppID + " plugin on this stack"},
				}
			} else {
				row, statusErr = instrumentationRow(ctx, loader)
			}

			doc := newSetupStatus(products)
			if statusErr != nil {
				detailed := fail.ErrorToDetailedError(statusErr)
				code := gcxerrors.ExitGeneralError
				if detailed.ExitCode != nil {
					code = *detailed.ExitCode
				}
				row = unknownInstrumentationRow(detailed)
				doc.Error = &setupStatusError{
					Summary:     detailed.Summary,
					ExitCode:    code,
					Details:     detailed.Details,
					Suggestions: detailed.Suggestions,
					DocsLink:    detailed.DocsLink,
				}
			}
			doc.Products = append(doc.Products, row)
			if err := opts.IO.Encode(cmd.OutOrStdout(), doc); err != nil {
				return err
			}
			if doc.Error != nil {
				return gcxerrors.NewEmittedError(doc.Error.ExitCode, statusErr)
			}
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// instrumentationRow reads the Instrumentation Hub state through the collector
// app plugin proxy. The caller converts failures before rendering an unknown row.
func instrumentationRow(ctx context.Context, loader *providers.ConfigLoader) (setupProductStatus, error) {
	r, err := fleetbase.LoadClientWithStack(ctx, loader)
	if err != nil {
		return setupProductStatus{}, fmt.Errorf("setup: %w", err)
	}

	monResp, err := instrum.NewClient(r.Client).RunK8sMonitoring(ctx, instrum.PromHeadersFromStack(r.Stack))
	if err != nil {
		return setupProductStatus{}, fmt.Errorf("setup: %w", err)
	}

	return setupProductStatus{
		Product: "instrumentation",
		Enabled: new(len(monResp.Clusters) > 0),
		Health:  "healthy",
		Details: fmt.Sprintf("%d clusters", len(monResp.Clusters)),
	}, nil
}

// unknownInstrumentationRow renders a failed instrumentation check as a product
// row, so the user sees the cause next to the Fleet Management row.
func unknownInstrumentationRow(err *gcxerrors.DetailedError) setupProductStatus {
	details := "the check failed: " + err.Summary
	if reason := strings.TrimSpace(err.Details); reason != "" {
		details += ": " + reason
	}
	return setupProductStatus{
		Product: "instrumentation",
		Health:  "unknown",
		Details: details,
	}
}

// setupStatusTextCodec is the human "text" codec for setupStatus values: it
// renders the PRODUCT/ENABLED/HEALTH/DETAILS table, with unknown enablement
// when a check could not determine the state.
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
		enabled := "unknown"
		if p.Enabled != nil {
			enabled = "no"
			if *p.Enabled {
				enabled = "yes"
			}
		}
		t.Row(p.Product, enabled, p.Health, p.Details)
	}
	if err := t.Render(w); err != nil {
		return err
	}
	if status.Error != nil {
		_, err := fmt.Fprint(w, (gcxerrors.DetailedError{
			Summary:     status.Error.Summary,
			Details:     status.Error.Details,
			Suggestions: status.Error.Suggestions,
			DocsLink:    status.Error.DocsLink,
		}).Error())
		return err
	}
	return nil
}
