package azure

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/gcx/internal/docs"
	"github.com/grafana/gcx/internal/onboard"
	"github.com/grafana/gcx/internal/plugins"
	"github.com/grafana/grafana-app-sdk/logging"
	"golang.org/x/sync/errgroup"
)

// ErrPluginUnavailable indicates the datasource plugin a selection needs is not
// installed and could not be installed (e.g. a missing Enterprise plugin, or
// the user declined installation). It is non-fatal to the overall run: the
// affected datasource is skipped while the others still provision.
var ErrPluginUnavailable = errors.New("required datasource plugin is unavailable")

// RunDeps holds the collaborators the orchestrator needs.
type RunDeps struct {
	CLI    *CLI
	DS     *datasources.Client
	Log    logging.Logger
	ErrOut io.Writer
	// Progress receives human-readable narration for long-running steps. When
	// nil (e.g. agent mode), progress is suppressed.
	Progress io.Writer
	// ConfirmRollback, when set, is asked whether to revert the listed steps
	// after a mid-run failure. It receives the step descriptions in revert
	// order. When nil (non-interactive/agent), gcx reverts automatically.
	ConfirmRollback func(steps []string) (bool, error)
	// Plugins checks/installs required datasource plugins. When nil, the plugin
	// pre-flight is skipped.
	Plugins *plugins.Client
	// ConfirmInstallPlugin, when set, is asked whether to install a missing
	// required plugin. When nil (non-interactive/agent), gcx installs it
	// automatically.
	ConfirmInstallPlugin func(pluginID string) (bool, error)
	// HealthAttempts and HealthBackoff tune the post-create health check retry
	// used to tolerate RBAC-propagation lag. Zero values fall back to defaults.
	HealthAttempts int
	HealthBackoff  time.Duration
}

const (
	defaultHealthAttempts = 4
	defaultHealthBackoff  = 2 * time.Second
)

func (d RunDeps) healthAttempts() int {
	if d.HealthAttempts > 0 {
		return d.HealthAttempts
	}
	return defaultHealthAttempts
}

func (d RunDeps) healthBackoff() time.Duration {
	if d.HealthBackoff > 0 {
		return d.HealthBackoff
	}
	return defaultHealthBackoff
}

// Selection is a chosen suggestion plus the resolved role set to assign.
type Selection struct {
	Suggestion Suggestion
	Roles      []string // resolved roles for app-registration specs (nil for key-based)
}

// ProvisionInput carries the resolved inputs for a provisioning run.
type ProvisionInput struct {
	Account     Account
	CallerOID   string
	Selections  []Selection
	Interactive bool
	Stack       string // Grafana stack slug, stamped onto artifacts for attribution
	ExpiryDays  int    // optional minted-secret expiry in days (0 = Azure default)
	DryRun      bool   // preview only: mint/create nothing
	SkipHealth  bool   // skip the post-create datasource health verification
	// Concurrency bounds how many datasources are minted/created in parallel.
	// Zero uses defaultProvisionConcurrency. Interactive runs are always serial
	// (see effectiveConcurrency) so prompts and progress stay ordered.
	Concurrency int
}

// defaultProvisionConcurrency bounds parallel minting when the caller does not
// set ProvisionInput.Concurrency. It is deliberately modest: each job shells out
// to `az` (which shares an on-disk MSAL token cache) and issues Azure ARM
// writes, so a small fan-out avoids token-cache contention and ARM throttling
// while still overlapping the slow RBAC-propagation health waits.
const defaultProvisionConcurrency = 4

// effectiveConcurrency resolves the parallelism for a run, clamped to the number
// of jobs. Interactive runs stay serial so narration and prompts remain ordered
// and readable.
func effectiveConcurrency(in ProvisionInput, jobs int) int {
	limit := in.Concurrency
	if limit <= 0 {
		limit = defaultProvisionConcurrency
	}
	if in.Interactive {
		limit = 1
	}
	if limit > jobs {
		limit = jobs
	}
	if limit < 1 {
		limit = 1
	}
	return limit
}

// mintJob is a selection that survived the serial planning pass and needs its
// credentials minted and datasource created in the parallel pass. name is the
// pre-resolved, collision-free artifact name, so parallel workers never race on
// name resolution.
type mintJob struct {
	idx  int
	sel  Selection
	name string
}

// Provision creates each selected datasource, minting gcx-owned app
// registrations as needed. On any failure it rolls back all artifacts created
// during the run (datasources and app registrations) and returns the error.
//
// It is idempotent: a selection whose gcx-managed datasource already exists is
// reused (reported as "existing") rather than duplicated. With DryRun set it
// resolves the plan and reports what would be created without any side effects.
//
// Provisioning runs in two passes. The first (serial) pass resolves already-
// existing, dry-run, and skipped datasources, runs the plugin pre-flight (which
// may prompt interactively), and reserves a unique name for each datasource
// that will be minted. The second pass mints credentials and creates the
// datasources with bounded parallelism (see effectiveConcurrency), overlapping
// the slow Azure/Grafana calls. Result ordering matches the input selections.
func Provision(ctx context.Context, deps RunDeps, in ProvisionInput) (onboard.Result, error) {
	rb := &onboard.Rollback{}

	existing, err := existingDatasourcesByName(ctx, deps.DS)
	if err != nil {
		return onboard.Result{}, fmt.Errorf("failed to list existing datasources: %w", err)
	}

	// rollback handles the accumulated undo steps after a mid-run failure. It
	// asks the user (when a confirmer is set) whether to revert, listing what
	// will be undone; otherwise it reverts automatically. The revert itself is
	// both logged and narrated step-by-step.
	rollback := func() {
		if rb.Len() == 0 {
			return
		}
		onboard.Progressf(deps.Progress, "An error occurred after creating %d artifact(s).", rb.Len())
		if !shouldRollback(deps, rb) {
			onboard.Progressf(deps.Progress, "Keeping created artifacts. Remove them later with: gcx setup datasources azure --cleanup")
			return
		}
		rb.Run(ctx, deps.Log, deps.Progress)
	}

	results := make([]onboard.DatasourceResult, len(in.Selections))
	jobs, err := planJobs(ctx, deps, in, existing, results)
	if err != nil {
		return onboard.Result{}, err
	}

	if len(jobs) > 0 {
		limit := effectiveConcurrency(in, len(jobs))
		if limit > 1 {
			onboard.Progressf(deps.Progress, "Provisioning %d datasource(s), up to %d in parallel...", len(jobs), limit)
		}
		g, gctx := errgroup.WithContext(ctx)
		g.SetLimit(limit)
		for _, job := range jobs {
			g.Go(func() error {
				res, err := provisionOne(gctx, deps, in, job, rb)
				if err != nil {
					return err
				}
				// Each job owns a distinct index, so concurrent writes to
				// separate elements are safe without a lock.
				results[job.idx] = res
				return nil
			})
		}
		if err := g.Wait(); err != nil {
			rollback()
			return onboard.Result{}, err
		}
	}

	return onboard.Result{Provider: "azure", Datasources: results, DryRun: in.DryRun}, nil
}

// planJobs runs the serial planning pass: it fills results[i] for every
// selection that is already existing, dry-run-planned, or skipped, and returns
// the remaining selections (with reserved, collision-free names) that must be
// minted in the parallel pass. It is kept serial because it may prompt (plugin
// install) and because name reservation must not race.
func planJobs(
	ctx context.Context,
	deps RunDeps,
	in ProvisionInput,
	existing map[string]*datasources.Datasource,
	results []onboard.DatasourceResult,
) ([]mintJob, error) {
	// ensurePluginOnce memoizes the plugin pre-flight per plugin kind for the
	// span of this run: several datasources of the same kind (e.g. two ADX
	// clusters) share a single install prompt/attempt instead of re-prompting
	// for each one.
	ensured := map[string]error{}
	ensurePluginOnce := func(kind string) error {
		if e, ok := ensured[kind]; ok {
			return e
		}
		e := ensurePlugin(ctx, deps, kind)
		ensured[kind] = e
		return e
	}

	// reserved tracks names claimed by earlier selections in this run so two
	// jobs never resolve to the same minted name before either exists.
	reserved := map[string]bool{}
	var jobs []mintJob
	for i, sel := range in.Selections {
		base := sel.Suggestion.Name

		// Idempotency: reuse an existing gcx-managed datasource for this resource
		// instead of creating a "-2" duplicate.
		if d := existing[strings.ToLower(base)]; d != nil {
			onboard.Progressf(deps.Progress, "→ %s already exists (uid %s); skipping", d.Name, d.UID)
			result := onboard.DatasourceResult{
				Name: d.Name, Type: d.Type, UID: d.UID, Status: onboard.StatusExisting,
			}
			applyPrivateHint(in, sel, &result)
			results[i] = result
			continue
		}

		if in.DryRun {
			onboard.Progressf(deps.Progress, "→ would create %s", sel.Suggestion.Label)
			result := plannedResult(sel, base)
			applyPrivateHint(in, sel, &result)
			results[i] = result
			continue
		}

		// Pre-flight the datasource plugin before minting any credentials, so we
		// never create Azure artifacts for a datasource that can't be created. A
		// missing/uninstallable plugin fails only this datasource (nothing has
		// been minted yet): record it as skipped and carry on with the others.
		if err := ensurePluginOnce(sel.Suggestion.Spec.Kind()); err != nil {
			if errors.Is(err, ErrPluginUnavailable) {
				onboard.Progressf(deps.Progress, "  skipping %s: %v", sel.Suggestion.Label, err)
				warn(deps.ErrOut, fmt.Sprintf("skipping %s: %v", sel.Suggestion.Label, err))
				results[i] = skippedResult(sel, base, err)
				continue
			}
			// Nothing has been minted yet in this pass, so there is nothing to
			// roll back — surface the hard error directly.
			return nil, err
		}

		name, err := onboard.EnsureUniqueName(base, func(n string) (bool, error) {
			key := strings.ToLower(n)
			if existing[key] != nil || reserved[key] {
				return true, nil
			}
			return deps.CLI.AppExists(ctx, n)
		})
		if err != nil {
			return nil, err
		}
		reserved[strings.ToLower(name)] = true
		jobs = append(jobs, mintJob{idx: i, sel: sel, name: name})
	}
	return jobs, nil
}

// provisionOne mints credentials (as needed) and creates a single datasource,
// then tags the backing app registration and verifies datasource health. It is
// safe to run concurrently for distinct jobs: the name is pre-resolved, the
// rollback accumulator is concurrency-safe, and no shared maps are mutated.
func provisionOne(
	ctx context.Context,
	deps RunDeps,
	in ProvisionInput,
	job mintJob,
	rb *onboard.Rollback,
) (onboard.DatasourceResult, error) {
	sel, name := job.sel, job.name
	onboard.Progressf(deps.Progress, "→ %s", sel.Suggestion.Label)

	prov, err := sel.Suggestion.Spec.AcquireAndBuild(ctx, SpecInput{
		CLI:         deps.CLI,
		Account:     in.Account,
		Name:        name,
		Roles:       sel.Roles,
		Scopes:      sel.Suggestion.Scopes,
		CallerOID:   in.CallerOID,
		Extra:       sel.Suggestion.Extra,
		Stack:       in.Stack,
		ExpiryDays:  in.ExpiryDays,
		Rollback:    rb,
		Interactive: in.Interactive,
		Progress:    deps.Progress,
		Log:         deps.Log,
	})
	if err != nil {
		return onboard.DatasourceResult{}, err
	}

	onboard.Progressf(deps.Progress, "  creating Grafana datasource %q...", name)
	ds, err := deps.DS.Create(ctx, &prov.Request)
	if err != nil {
		return onboard.DatasourceResult{}, fmt.Errorf("failed to create datasource %q: %w", name, err)
	}
	onboard.Progressf(deps.Progress, "  created datasource %q (uid %s)", name, ds.UID)

	uid := ds.UID
	rb.Add("delete datasource "+name, func(ctx context.Context) error {
		return deps.DS.Delete(ctx, uid)
	})

	// Stamp the app registration with the datasource UID so cleanup and rotation
	// can correlate credentials to datasources reliably (only when gcx minted
	// the credential — supplied creds are not ours to tag).
	if prov.AppID != "" {
		attr := Attribution{Stack: in.Stack, OwnerOID: in.CallerOID, DatasourceUID: uid}
		if err := deps.CLI.SetAppTags(ctx, prov.AppID, attr.Tags()); err != nil {
			return onboard.DatasourceResult{}, fmt.Errorf("failed to tag app registration %q: %w", prov.AppID, err)
		}
	}

	result := onboard.DatasourceResult{
		Name: name, Type: ds.Type, UID: uid, Status: onboard.StatusCreated,
	}
	if prov.AppID != "" {
		result.Credential = &onboard.CloudCredential{ID: prov.AppID}
	}
	if !in.SkipHealth {
		applyHealth(ctx, deps, &result, uid)
	}
	applyPrivateHint(in, sel, &result)
	return result, nil
}

// plannedResult builds the --dry-run preview row for a selection.
func plannedResult(sel Selection, base string) onboard.DatasourceResult {
	return onboard.DatasourceResult{
		Name:   base,
		Type:   sel.Suggestion.Spec.Kind(),
		Status: onboard.StatusPlanned,
		Credential: &onboard.CloudCredential{
			Roles:  sel.Roles,
			Scopes: sel.Suggestion.Scopes,
		},
	}
}

// applyPrivateHint attaches an advisory Private Data source Connect (PDC) hint
// when the backing resource has public network access disabled. It is gated to
// Grafana Cloud stacks (a non-empty stack slug): PDC is a Cloud feature, and a
// self-managed Grafana may already sit inside the resource's network. The hint
// is purely informational — the datasource and its credentials are still
// created; it only tells the user extra connectivity setup is likely needed.
func applyPrivateHint(in ProvisionInput, sel Selection, r *onboard.DatasourceResult) {
	if !sel.Suggestion.PrivateNetwork || in.Stack == "" {
		return
	}
	r.Hint = "public network access is disabled on this resource; your Grafana Cloud stack can only query it via Private Data source Connect (PDC)"
	r.HintDocs = docs.PrivateDataSourceConnect
}

// skippedResult builds the row for a selection that was intentionally not
// provisioned (e.g. its plugin was unavailable), carrying a short reason.
func skippedResult(sel Selection, base string, cause error) onboard.DatasourceResult {
	return onboard.DatasourceResult{
		Name:   base,
		Type:   sel.Suggestion.Spec.Kind(),
		Status: onboard.StatusSkipped,
		Note:   cause.Error(),
	}
}

// applyHealth runs the health check (with retry) and records the outcome on the
// result. Health failures never fail the run — a datasource can be briefly
// unhealthy while Azure RBAC propagates — but they are surfaced to the user.
func applyHealth(ctx context.Context, deps RunDeps, result *onboard.DatasourceResult, uid string) {
	onboard.Progressf(deps.Progress, "  verifying datasource health...")
	h := checkHealth(ctx, deps, uid)
	if h == nil {
		return
	}
	result.Health = h.Status
	if !strings.EqualFold(h.Status, "ok") {
		result.HealthMessage = h.Message
		onboard.Progressf(deps.Progress, "  health: %s — %s", h.Status, h.Message)
	} else {
		onboard.Progressf(deps.Progress, "  health: OK")
	}
}

// checkHealth polls the datasource health endpoint, retrying with backoff to
// tolerate RBAC-propagation lag, until it reports OK or attempts are exhausted.
func checkHealth(ctx context.Context, deps RunDeps, uid string) *datasources.HealthResult {
	attempts := deps.healthAttempts()
	var last *datasources.HealthResult
	for i := range attempts {
		h, err := deps.DS.Health(ctx, uid)
		switch {
		case err != nil:
			last = &datasources.HealthResult{UID: uid, Status: "UNKNOWN", Message: err.Error()}
		default:
			last = h
			if strings.EqualFold(h.Status, "ok") {
				return h
			}
		}
		if i < attempts-1 {
			select {
			case <-ctx.Done():
				return last
			case <-time.After(deps.healthBackoff()):
			}
		}
	}
	return last
}

// corePlugins are datasource plugins bundled with Grafana; they are always
// present and must be skipped by the plugin pre-flight.
var corePlugins = map[string]bool{ //nolint:gochecknoglobals
	KindAzureMonitor: true,
}

func isCorePlugin(pluginID string) bool {
	return corePlugins[pluginID]
}

// ensurePlugin verifies the datasource plugin is installed, installing it
// (after confirmation in interactive mode) when missing. A check that cannot be
// performed (e.g. insufficient permissions to read /api/plugins) is treated as
// non-blocking so provisioning can still proceed.
func ensurePlugin(ctx context.Context, deps RunDeps, pluginID string) error {
	if deps.Plugins == nil || pluginID == "" || isCorePlugin(pluginID) {
		return nil
	}

	installed, err := deps.Plugins.IsInstalled(ctx, pluginID)
	if err != nil {
		if deps.Log != nil {
			deps.Log.Debug("plugin check skipped", "plugin", pluginID, "error", err.Error())
		}
		return nil
	}
	if installed {
		return nil
	}

	onboard.Progressf(deps.Progress, "  required plugin %q is not installed", pluginID)

	install := true
	if deps.ConfirmInstallPlugin != nil {
		ok, cerr := deps.ConfirmInstallPlugin(pluginID)
		if cerr != nil {
			return cerr
		}
		install = ok
	}
	if !install {
		return fmt.Errorf("%w: %q is not installed; install it and re-run, or accept installation when prompted", ErrPluginUnavailable, pluginID)
	}

	onboard.Progressf(deps.Progress, "  installing plugin %q...", pluginID)
	if err := deps.Plugins.Install(ctx, pluginID, ""); err != nil {
		return fmt.Errorf("%w: failed to install %q: %w", ErrPluginUnavailable, pluginID, err)
	}
	if deps.Log != nil {
		deps.Log.Info("installed plugin", "plugin", pluginID)
	}
	onboard.Progressf(deps.Progress, "  installed plugin %q", pluginID)
	return nil
}

// shouldRollback decides whether to revert. With no confirmer (non-interactive
// /agent) it reverts automatically. With a confirmer it asks the user; a
// confirmation error falls back to reverting so artifacts are never silently
// left behind.
func shouldRollback(deps RunDeps, rb *onboard.Rollback) bool {
	if deps.ConfirmRollback == nil {
		return true
	}
	ok, err := deps.ConfirmRollback(rb.Descriptions())
	if err != nil {
		if deps.Log != nil {
			deps.Log.Warn("rollback confirmation failed; reverting to be safe", "error", err.Error())
		}
		return true
	}
	if !ok && deps.Log != nil {
		deps.Log.Info("user declined rollback; keeping artifacts", "steps", rb.Len())
	}
	return ok
}

// existingDatasourcesByName indexes the current datasources by lower-cased name
// so the orchestrator can detect (and reuse) already-onboarded resources.
func existingDatasourcesByName(ctx context.Context, ds *datasources.Client) (map[string]*datasources.Datasource, error) {
	list, err := ds.List(ctx)
	if err != nil {
		return nil, err
	}
	byName := make(map[string]*datasources.Datasource, len(list))
	for _, d := range list {
		byName[strings.ToLower(d.Name)] = d
	}
	return byName, nil
}
