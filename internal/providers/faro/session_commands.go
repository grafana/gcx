package faro

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/loki"
	"github.com/grafana/gcx/internal/query/pinot"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type sessionsGetOpts struct {
	dsquery.TimeRangeOpts

	App        string
	AppType    string
	Datasource string
	Save       string
}

func (o *sessionsGetOpts) setup(flags *pflag.FlagSet) {
	o.SetupTimeFlags(flags)
	flags.StringVar(&o.App, "app", "", "Frontend Observability app slug-id or numeric id (required)")
	flags.StringVar(&o.AppType, "app-type", "", "web or mobile. Optional: inferred from sdkName/osName when omitted")
	flags.StringVar(&o.Datasource, "datasource", datasourceLoki, "Telemetry backend: loki or pinot (default loki)")
	flags.StringVar(&o.Save, "save", "", "Write the session dump to this path instead of stdout")
}

func (o *sessionsGetOpts) Validate() error {
	if strings.TrimSpace(o.App) == "" {
		return errors.New("--app is required")
	}
	switch strings.ToLower(o.AppType) {
	case "", appTypeWeb, appTypeMobile:
		o.AppType = strings.ToLower(o.AppType)
	default:
		return fmt.Errorf("--app-type must be %s or %s, got %q", appTypeWeb, appTypeMobile, o.AppType)
	}
	switch strings.ToLower(o.Datasource) {
	case datasourceLoki, datasourcePinot:
		o.Datasource = strings.ToLower(o.Datasource)
	default:
		return fmt.Errorf("--datasource must be %s or %s, got %q", datasourceLoki, datasourcePinot, o.Datasource)
	}
	if err := o.ValidateTimeRange(); err != nil {
		return err
	}
	if !o.IsRange() {
		return errors.New("--since or --from/--to is required")
	}
	if agent.IsAgentMode() && o.Save == "" {
		return errors.New("--save is required in agent mode so the session dump is not written to stdout")
	}
	return nil
}

func newSessionsGetCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &sessionsGetOpts{}
	cmd := &cobra.Command{
		Use:   "get <session-id>",
		Short: "Write Frontend Observability session telemetry to a text file.",
		Long: `Fetch one Frontend Observability session and write it as plain text.

Two labeled blocks are produced: session metadata (once) and events (the user
journey). Without --save, Pinot metadata prints as tables and the journey as
TSV; Loki metadata is named fields once (sdk, app, user, os, geo, browser,
device, session_id, session_attr), and events are timestamp then the log line
with those envelope keys stripped. --save writes Pinot TSV or the Loki stream.
There is no JSON or YAML encoding of the dump.

Use --save so agents receive a small artifact receipt on stdout and then read
the file.

--datasource selects the backend (loki or pinot), not a Grafana UID. The UID
is resolved from config or auto-discovery.

Faro apps do not store web vs mobile on the app resource. Omit --app-type and
gcx infers it from sdkName / osName on the session (so mobile journeys exclude
app_memory / app_cpu_usage). Pass --app-type to override.`,
		Example: `  # Pinot on stdout (metadata tables, journey TSV)
  gcx frontend sessions get 7TiMbCCvby --app 66 --datasource pinot --since 7d

  # Pinot dump to a file; app type inferred from telemetry
  gcx frontend sessions get 7TiMbCCvby --app 66 --datasource pinot --since 7d \
    --save /tmp/session-7TiMbCCvby.txt

  # Loki dump
  gcx frontend sessions get 7TiMbCCvby --app 66 --datasource loki --since 7d \
    --save /tmp/session-7TiMbCCvby.txt

  # Force mobile SQL (app_memory / app_cpu_usage excluded)
  gcx frontend sessions get kwwAkkXwas --app 96 --app-type mobile \
    --datasource pinot --since 7d --save /tmp/session-kwwAkkXwas.txt`,
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(1)(cmd, args); err != nil {
				return err
			}
			if strings.TrimSpace(args[0]) == "" {
				return errors.New("session-id is required")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			sessionID := strings.TrimSpace(args[0])

			cfgCtx, cfg, err := dsquery.LoadContextAndConfig(ctx, loader)
			if err != nil {
				return err
			}

			faroClient, err := NewClient(cfg)
			if err != nil {
				return err
			}

			appID := resolveAppID(opts.App)
			app, err := faroClient.Get(ctx, appID)
			if err != nil {
				return err
			}
			if app.ID != "" {
				appID = app.ID
			}

			now := time.Now()
			start, end, err := opts.ParseTimeRange(now)
			if err != nil {
				return err
			}

			params := sessionQueryParams{
				AppID:     appID,
				SessionID: sessionID,
				AppType:   opts.AppType,
			}

			var result interface {
				dump() string
				writeTables(io.Writer) error
			}
			switch opts.Datasource {
			case datasourcePinot:
				uid, _, resolveErr := dsquery.ResolveValidateAndSaveDatasource(ctx, loader, "", cfgCtx, cfg, datasourcePinot)
				if resolveErr != nil {
					return resolveErr
				}
				client, clientErr := pinot.NewClient(cfg)
				if clientErr != nil {
					return fmt.Errorf("failed to create pinot client: %w", clientErr)
				}
				result, err = fetchPinotSession(ctx, client, uid, params, start, end)
			case datasourceLoki:
				uid, _, resolveErr := dsquery.ResolveValidateAndSaveDatasource(ctx, loader, "", cfgCtx, cfg, datasourceLoki)
				if resolveErr != nil {
					return resolveErr
				}
				client, clientErr := loki.NewClient(cfg)
				if clientErr != nil {
					return fmt.Errorf("failed to create loki client: %w", clientErr)
				}
				result, err = fetchLokiSession(ctx, client, uid, params, start, end)
			}
			if err != nil {
				return err
			}

			if opts.Save == "" {
				return result.writeTables(cmd.OutOrStdout())
			}

			if err = os.WriteFile(opts.Save, []byte(result.dump()), 0o600); err != nil {
				return fmt.Errorf("writing session dump: %w", err)
			}

			receipt := cmdio.NewArtifactReceipt("get", "txt")
			receipt.Files = append(receipt.Files, cmdio.ArtifactFile{Path: opts.Save})
			receipt.Summary = cmdio.MutationSummary{Succeeded: 1}
			return cmdio.EmitArtifactResult(cmd.OutOrStdout(), receipt, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "Wrote %s\n", opts.Save)
				return err
			})
		},
	}

	opts.setup(cmd.Flags())
	return cmd
}
