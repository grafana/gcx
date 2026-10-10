package faro

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/pinot"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/pflag"
)

const (
	frontendDefaultSince = "24h"
	// frontendMaxRange is Frontend Observability's Pinot retention.
	frontendMaxRange = 30 * 24 * time.Hour

	frontendListDefaultLimit = 20
	frontendListMaxLimit     = 200
)

// frontendOpts holds the flags shared by the Pinot-backed frontend commands
// (errors, pages, query).
type frontendOpts struct {
	dsquery.TimeRangeOpts

	IO         cmdio.Options
	Share      dsquery.ExploreLinkOpts
	App        string
	Datasource string
	ShowSQL    bool

	appRequired bool
	// userTo is --to as given, before ValidateTimeRange fills it from --since.
	userTo string
}

func (o *frontendOpts) setup(flags *pflag.FlagSet, appRequired bool) {
	o.appRequired = appRequired
	o.SetupTimeFlags(flags)
	appUsage := "Frontend Observability app: numeric ID, slug-id, or name"
	if appRequired {
		appUsage += " (required)"
	}
	flags.StringVar(&o.App, "app", "", appUsage)
	flags.StringVarP(&o.Datasource, "datasource", "d", "", "Frontend Observability Pinot datasource UID (defaults to datasources.pinot in the context, then auto-discovery)")
	o.Share.Setup(flags, "primary query")
}

func (o *frontendOpts) setupSQLFlag(flags *pflag.FlagSet) {
	flags.BoolVar(&o.ShowSQL, "sql", false, "Include the PinotQL statements that ran in the output (runnable with gcx frontend query)")
}

// Validate trims inputs, applies the 24h default window, and enforces the
// 30-day retention cap.
func (o *frontendOpts) Validate() error {
	o.App = strings.TrimSpace(o.App)
	o.Datasource = strings.TrimSpace(o.Datasource)
	if o.appRequired && o.App == "" {
		return &gcxerrors.DetailedError{
			Summary:     "--app is required",
			Details:     "Pass the Frontend Observability app as a numeric ID, slug-id, or name.",
			Suggestions: []string{appsListSuggestion},
			ExitCode:    new(gcxerrors.ExitUsageError),
		}
	}
	if err := o.IO.Validate(); err != nil {
		return err
	}
	o.userTo = o.To
	if o.Since == "" && o.From == "" && o.To == "" {
		o.Since = frontendDefaultSince
	}
	if err := o.ValidateTimeRange(); err != nil {
		return err
	}
	start, end, err := o.ParseTimeRange(time.Now())
	if err != nil {
		return err
	}
	return validateFrontendRange(start, end)
}

func validateFrontendRange(start, end time.Time) error {
	if !end.After(start) {
		return errors.New("--to must be after --from")
	}
	if end.Sub(start) > frontendMaxRange {
		return fmt.Errorf("time range of %.1f days exceeds the 30-day retention of Frontend Observability Pinot data; use --since 30d or less", end.Sub(start).Hours()/24)
	}
	return nil
}

// sinceArg renders the window for next-step commands.
func (o *frontendOpts) sinceArg() []string {
	if o.Since != "" {
		if o.userTo != "" {
			return []string{"--since", o.Since, "--to", shellQuote(o.userTo)}
		}
		return []string{"--since", o.Since}
	}
	return []string{"--from", shellQuote(o.From), "--to", shellQuote(o.To)}
}

// frontendApp identifies the resolved app in command output.
type frontendApp struct {
	ID   int64  `json:"id"`
	Name string `json:"name,omitempty"`
}

type frontendRange struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

// frontendTarget is everything a frontend command resolves before querying.
type frontendTarget struct {
	cfg    config.NamespacedRESTConfig
	cfgCtx *config.Context
	faro   *Client
	pinot  *pinot.Client
	app    frontendApp
	dsUID  string
	dsType string
	start  time.Time
	end    time.Time
}

func (t *frontendTarget) appID() string { return strconv.FormatInt(t.app.ID, 10) }

// resolve loads config, resolves the app (when --app is set), the Pinot
// datasource, and the time range.
func (o *frontendOpts) resolve(ctx context.Context, loader *providers.ConfigLoader) (*frontendTarget, error) {
	cfgCtx, cfg, err := dsquery.LoadContextAndConfig(ctx, loader)
	if err != nil {
		return nil, err
	}

	faroClient, err := NewClient(cfg)
	if err != nil {
		return nil, err
	}

	t := &frontendTarget{cfg: cfg, cfgCtx: cfgCtx, faro: faroClient}

	if o.App != "" {
		app, err := resolveFrontendApp(ctx, faroClient, o.App)
		if err != nil {
			return nil, err
		}
		t.app = app
	}

	t.dsUID, t.dsType, err = dsquery.ResolveValidateAndSaveDatasource(ctx, loader, o.Datasource, cfgCtx, cfg, datasourcePinot)
	if err != nil {
		return nil, err
	}

	t.start, t.end, err = o.ParseTimeRange(time.Now())
	if err != nil {
		return nil, err
	}

	t.pinot, err = pinot.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create pinot client: %w", err)
	}
	return t, nil
}

type appGetter interface {
	Get(ctx context.Context, id string) (*FaroApp, error)
	GetByName(ctx context.Context, name string) (*FaroApp, error)
}

// resolveFrontendApp accepts a numeric ID, a slug-id, or an app name. A
// slug-shaped name whose numeric suffix is not an app ID falls back to a
// name lookup.
func resolveFrontendApp(ctx context.Context, c appGetter, ref string) (frontendApp, error) {
	var app *FaroApp
	var err error
	if id, ok := adapter.ExtractIDFromSlug(ref); ok {
		app, err = c.Get(ctx, id)
		if err != nil {
			if _, numErr := strconv.ParseInt(ref, 10, 64); numErr == nil {
				return frontendApp{}, appNotFoundError(ref, err)
			}
			app, err = c.GetByName(ctx, ref)
		}
	} else {
		app, err = c.GetByName(ctx, ref)
	}
	if err != nil {
		return frontendApp{}, appNotFoundError(ref, err)
	}
	id, err := strconv.ParseInt(app.ID, 10, 64)
	if err != nil {
		return frontendApp{}, fmt.Errorf("faro: app %q has non-numeric id %q", ref, app.ID)
	}
	return frontendApp{ID: id, Name: app.Name}, nil
}

const appsListSuggestion = "List the apps in this stack: gcx frontend apps list"

// appNotFoundError reports an --app value that matched no app, keeping the
// API error as detail.
func appNotFoundError(ref string, cause error) error {
	return &gcxerrors.DetailedError{
		Summary:     fmt.Sprintf("Frontend Observability app %q not found", ref),
		Details:     cause.Error(),
		Parent:      cause,
		Suggestions: []string{appsListSuggestion},
	}
}

// validateListLimit applies the shared list cap.
func validateListLimit(limit int) error {
	if limit < 1 || limit > frontendListMaxLimit {
		return fmt.Errorf("--limit must be between 1 and %d, got %d", frontendListMaxLimit, limit)
	}
	return nil
}

// commandLine renders a gcx invocation from parts for headers and next steps.
func commandLine(parts ...string) string {
	return strings.Join(parts, " ")
}
