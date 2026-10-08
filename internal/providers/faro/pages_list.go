package faro

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	dsquery "github.com/grafana/gcx/internal/datasources/query"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	vitalLCP  = "lcp"
	vitalINP  = "inp"
	vitalCLS  = "cls"
	vitalFCP  = "fcp"
	vitalTTFB = "ttfb"

	pagesSortLoads  = "loads"
	pagesSortErrors = "errors"

	ratingGood             = "good"
	ratingNeedsImprovement = "needs-improvement"
	ratingPoor             = "poor"
)

// pagesVitalsSQL ports Page Performance (report §2.8) and Page Loads (§2.3)
// without the multi-stage join. p75 reads the measurementValues.* columns,
// which are null when a report lacks that key; the top-level vitals columns
// are zero-filled and would drag the percentile down.
const pagesVitalsSQL = `SELECT
  pageId AS page_id,
  count(*) FILTER (WHERE ttfb > 0) AS page_loads,
  PERCENTILETDIGEST("measurementValues.ttfb", 75) FILTER (WHERE "measurementValues.ttfb" IS NOT NULL) AS ttfb_p75,
  PERCENTILETDIGEST("measurementValues.fcp", 75) FILTER (WHERE "measurementValues.fcp" IS NOT NULL) AS fcp_p75,
  PERCENTILETDIGEST("measurementValues.lcp", 75) FILTER (WHERE "measurementValues.lcp" IS NOT NULL) AS lcp_p75,
  PERCENTILETDIGEST("measurementValues.cls", 75) FILTER (WHERE "measurementValues.cls" IS NOT NULL) AS cls_p75,
  PERCENTILETDIGEST("measurementValues.inp", 75) FILTER (WHERE "measurementValues.inp" IS NOT NULL) AS inp_p75
FROM faro_pinot_measurements_v1
WHERE appId = {{APP_ID}}
  AND measurementType = 'web-vitals'
  AND $__timeFilter("timestamp_30s")
  AND pageId <> '' AND pageId <> 'null'{{FILTERS}}
GROUP BY page_id
ORDER BY page_loads DESC
LIMIT 1000`

const pagesErrorsSQL = `SELECT pageId AS page_id, count(*) AS errors
FROM faro_pinot_exceptions_v1
WHERE appId = {{APP_ID}}
  AND $__timeFilter("timestamp_30s")
  AND pageId <> '' AND pageId <> 'null'{{FILTERS}}
GROUP BY page_id
ORDER BY errors DESC
LIMIT 1000`

// vitalThresholds returns the web.dev p75 thresholds: good at or below good,
// poor above poor.
func vitalThresholds(vital string) (float64, float64) {
	switch vital {
	case vitalLCP:
		return 2500, 4000
	case vitalINP:
		return 200, 500
	case vitalCLS:
		return 0.1, 0.25
	case vitalFCP:
		return 1800, 3000
	default:
		return 800, 1800
	}
}

func vitalRating(vital string, p75 float64) string {
	good, poor := vitalThresholds(vital)
	switch {
	case p75 <= good:
		return ratingGood
	case p75 > poor:
		return ratingPoor
	default:
		return ratingNeedsImprovement
	}
}

// vitalValue is one p75 with its rating. Timing vitals use p75_ms; CLS is
// unitless and uses p75. A missing p75 is null, with a null rating, so the
// key is always present for field selection.
type vitalValue struct {
	P75MS  *float64
	P75    *float64
	Rating *string

	unitless bool
}

func newVitalValue(vital string, v float64, ok bool) vitalValue {
	out := vitalValue{unitless: vital == vitalCLS}
	if !ok {
		return out
	}
	rating := vitalRating(vital, v)
	out.Rating = &rating
	if out.unitless {
		v = math.Round(v*1000) / 1000
		out.P75 = &v
		return out
	}
	v = math.Round(v)
	out.P75MS = &v
	return out
}

// MarshalJSON emits {"p75_ms": <n|null>, "rating": <s|null>} for timing
// vitals and {"p75": ..., "rating": ...} for CLS.
func (v vitalValue) MarshalJSON() ([]byte, error) {
	if v.unitless {
		return json.Marshal(struct {
			P75    *float64 `json:"p75"`
			Rating *string  `json:"rating"`
		}{v.P75, v.Rating})
	}
	return json.Marshal(struct {
		P75MS  *float64 `json:"p75_ms"`
		Rating *string  `json:"rating"`
	}{v.P75MS, v.Rating})
}

func (v vitalValue) value() (float64, bool) {
	switch {
	case v.P75MS != nil:
		return *v.P75MS, true
	case v.P75 != nil:
		return *v.P75, true
	default:
		return 0, false
	}
}

func (v vitalValue) String() string {
	f, ok := v.value()
	if !ok {
		return "-"
	}
	s := strconv.FormatFloat(f, 'f', -1, 64)
	if v.P75MS != nil {
		s += "ms"
	}
	return s + " " + *v.Rating
}

type pageVitals struct {
	LCP  vitalValue `json:"lcp"`
	INP  vitalValue `json:"inp"`
	CLS  vitalValue `json:"cls"`
	FCP  vitalValue `json:"fcp"`
	TTFB vitalValue `json:"ttfb"`
}

func (p pageVitals) get(vital string) vitalValue {
	switch vital {
	case vitalLCP:
		return p.LCP
	case vitalINP:
		return p.INP
	case vitalCLS:
		return p.CLS
	case vitalFCP:
		return p.FCP
	default:
		return p.TTFB
	}
}

type pageRow struct {
	Page   string     `json:"page"`
	Loads  int64      `json:"loads"`
	Errors int64      `json:"errors"`
	Vitals pageVitals `json:"vitals"`
}

type pagesListResult struct {
	App        frontendApp   `json:"app"`
	Range      frontendRange `json:"range"`
	Pages      []pageRow     `json:"pages"`
	TotalPages int           `json:"total_pages"`
	Next       []string      `json:"next,omitempty"`
	SQL        []sqlEntry    `json:"sql,omitempty"`
}

type pagesListOpts struct {
	frontendOpts
	filterOpts

	Sort  string
	Limit int
}

func pagesSortValues() []string {
	return []string{vitalLCP, vitalINP, vitalCLS, vitalFCP, vitalTTFB, pagesSortLoads, pagesSortErrors}
}

func (o *pagesListOpts) setup(flags *pflag.FlagSet) {
	rows := func(r *pagesListResult) []pageRow { return r.Pages }
	sql := func(r *pagesListResult) []sqlEntry { return r.SQL }
	o.IO.RegisterCustomCodec(cmdio.FormatTable, newRowsTableCodec(cmdio.FormatTable, pageTable(), rows, sql))
	o.IO.DefaultFormat(cmdio.FormatTable)
	o.IO.BindFlags(flags)

	o.frontendOpts.setup(flags, true)
	o.setupSQLFlag(flags)
	setupFilterFlags(flags, &o.filterOpts, true)
	flags.StringVar(&o.Sort, "sort", vitalLCP, "Sort order, descending: "+strings.Join(pagesSortValues(), ", ")+" (missing vitals last)")
	flags.IntVar(&o.Limit, "limit", frontendListDefaultLimit, fmt.Sprintf("Maximum number of pages to return (1-%d)", frontendListMaxLimit))
}

func (o *pagesListOpts) Validate() error {
	if err := o.frontendOpts.Validate(); err != nil {
		return err
	}
	o.filterOpts = o.trimmed()
	if !slices.Contains(pagesSortValues(), o.Sort) {
		return fmt.Errorf("--sort must be one of %s, got %q", strings.Join(pagesSortValues(), ", "), o.Sort)
	}
	return validateListLimit(o.Limit)
}

type pagesStatements struct{ vitals, errors statement }

func buildPagesStatements(appID int64, filters filterOpts, cmdline string) (pagesStatements, error) {
	params := []sqlParam{intParam("APP_ID", appID), rawParam("FILTERS", filters.clauses())}
	var s pagesStatements
	var err error
	if s.vitals, err = newStatement("vitals", tableMeasurements, cmdline, pagesVitalsSQL, params,
		"measurementValues.<vital> is null when a report lacks that key; the top-level lcp/fcp/... columns are 0 instead and must not be used for percentiles.",
		"Page loads count one TTFB report per navigation (ttfb > 0).",
		noteTimeFilter30); err != nil {
		return s, err
	}
	if s.errors, err = newStatement("page_errors", tableExceptions, cmdline, pagesErrorsSQL, params,
		noteNullString, noteTimeFilter30); err != nil {
		return s, err
	}
	return s, nil
}

func fetchPagesList(ctx context.Context, pq pinotQuerier, app frontendApp, uid string, start, end time.Time, stmts pagesStatements, sortBy string, limit int) (*pagesListResult, error) {
	resps, err := runStatements(ctx, pq, uid, start, end, []statement{stmts.vitals, stmts.errors})
	if err != nil {
		return nil, err
	}

	byPage := map[string]*pageRow{}
	var order []string
	row := func(page string) *pageRow {
		if r, ok := byPage[page]; ok {
			return r
		}
		r := &pageRow{Page: page}
		byPage[page] = r
		order = append(order, page)
		return r
	}

	for _, m := range responseRows(resps[0]) {
		page := cellString(m, "page_id")
		if page == "" {
			continue
		}
		r := row(page)
		r.Loads = cellInt(m, "page_loads")
		vital := func(name string) vitalValue {
			v, ok := cellFloat(m, name+"_p75")
			return newVitalValue(name, v, ok)
		}
		r.Vitals = pageVitals{LCP: vital(vitalLCP), INP: vital(vitalINP), CLS: vital(vitalCLS), FCP: vital(vitalFCP), TTFB: vital(vitalTTFB)}
	}
	for _, m := range responseRows(resps[1]) {
		page := cellString(m, "page_id")
		if page == "" {
			continue
		}
		row(page).Errors = cellInt(m, "errors")
	}

	pages := make([]pageRow, 0, len(order))
	for _, p := range order {
		pages = append(pages, *byPage[p])
	}
	sortPages(pages, sortBy)
	total := len(pages)
	if len(pages) > limit {
		pages = pages[:limit]
	}
	return &pagesListResult{
		App:        app,
		Range:      frontendRange{From: start.UTC(), To: end.UTC()},
		Pages:      pages,
		TotalPages: total,
	}, nil
}

func sortPages(pages []pageRow, by string) {
	slices.SortStableFunc(pages, func(a, b pageRow) int {
		switch by {
		case pagesSortLoads:
			if c := cmpDesc(a.Loads, b.Loads); c != 0 {
				return c
			}
		case pagesSortErrors:
			if c := cmpDesc(a.Errors, b.Errors); c != 0 {
				return c
			}
		default:
			av, aok := a.Vitals.get(by).value()
			bv, bok := b.Vitals.get(by).value()
			switch {
			case aok && !bok:
				return -1
			case !aok && bok:
				return 1
			case aok && bok && av != bv:
				if av > bv {
					return -1
				}
				return 1
			}
		}
		if c := cmpDesc(a.Loads, b.Loads); c != 0 {
			return c
		}
		return strings.Compare(a.Page, b.Page)
	})
}

// pageWithMostErrors picks the page to suggest errors list for. ok is false
// when no listed page has errors, so no next step is suggested.
func pageWithMostErrors(pages []pageRow) (string, bool) {
	best, ok := "", false
	var most int64
	for _, p := range pages {
		if p.Errors > most {
			best, most, ok = p.Page, p.Errors, true
		}
	}
	return best, ok
}

func pageTable() cmdio.Table[pageRow] {
	vital := func(name string) func(pageRow) string {
		return func(p pageRow) string { return p.Vitals.get(name).String() }
	}
	return cmdio.Table[pageRow]{
		Columns: []cmdio.Column[pageRow]{
			{Header: "PAGE", Content: func(p pageRow) string { return p.Page }},
			{Header: "LOADS", Content: func(p pageRow) string { return formatInt(p.Loads) }},
			{Header: "ERRORS", Content: func(p pageRow) string { return formatInt(p.Errors) }},
			{Header: "LCP P75", Content: vital(vitalLCP)},
			{Header: "INP P75", Content: vital(vitalINP)},
			{Header: "CLS P75", Content: vital(vitalCLS)},
			{Header: "FCP P75", Content: vital(vitalFCP)},
			{Header: "TTFB P75", Content: vital(vitalTTFB)},
		},
	}
}

func newPagesListCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &pagesListOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List pages with Web Vitals p75, page loads, and error counts.",
		Long: `List the pages (pageId) of a Frontend Observability app with page loads, error
counts, and the p75 of each Core Web Vital (LCP, INP, CLS, FCP, TTFB).

Each p75 carries a rating from the web.dev thresholds: good, needs-improvement,
or poor (LCP 2500/4000 ms, INP 200/500 ms, CLS 0.1/0.25, FCP 1800/3000 ms,
TTFB 800/1800 ms). A vital with no reports in the window has a null p75 and a
null rating, and sorts last. The filter flags match stored values exactly.

next suggests errors list for the listed page with the most errors, and is
omitted when none has any. The window defaults to the last 24 hours and cannot
exceed 30 days. --sql adds the PinotQL statements that ran.`,
		Example: `  # Slowest pages by LCP
  gcx frontend pages list --app checkout-web

  # Pages with the most errors in one release
  gcx frontend pages list --app 187 --version 4.18.0 --sort errors

  # One page, with the SQL that ran
  gcx frontend pages list --app 187 --page /cart --sql -o json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			ctx := cmd.Context()
			t, err := opts.resolve(ctx, loader)
			if err != nil {
				return err
			}

			cmdline := commandLine(append([]string{"gcx frontend pages list --app", t.appID()}, append(opts.sinceArg(), opts.flagArgs()...)...)...)
			stmts, err := buildPagesStatements(t.app.ID, opts.filterOpts, cmdline)
			if err != nil {
				return err
			}
			result, err := fetchPagesList(ctx, t.pinot, t.app, t.dsUID, t.start, t.end, stmts, opts.Sort, opts.Limit)
			if err != nil {
				return err
			}
			if page, ok := pageWithMostErrors(result.Pages); ok {
				result.Next = []string{commandLine(append([]string{"gcx frontend errors list --app", t.appID(), "-d", t.dsUID, "--page", shellQuote(page)}, opts.sinceArg()...)...)}
			}
			if opts.ShowSQL {
				result.SQL = sqlEntries(stmts.vitals, stmts.errors)
			}

			return dsquery.EncodeAndHandleExplore(cmd, func() error {
				return opts.IO.Encode(cmd.OutOrStdout(), result)
			}, opts.Share, opts.exploreLink(t, stmts.vitals, "pages list"))
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}
