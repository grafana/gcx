//nolint:testpackage // white-box tests exercise the orchestrator with unexported specs
package azure

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/gcx/internal/docs"
	"github.com/grafana/gcx/internal/onboard"
	"github.com/grafana/gcx/internal/plugins"
	"k8s.io/client-go/rest"
)

func newDSClient(t *testing.T, handler http.Handler) *datasources.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	cfg := config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}}
	client, err := datasources.NewClient(cfg)
	if err != nil {
		t.Fatalf("failed to create datasource client: %v", err)
	}
	return client
}

func mintHandler() func(args []string) ([]byte, []byte, error) {
	return func(args []string) ([]byte, []byte, error) {
		switch {
		case hasPrefix(args, []string{"ad", "app", "list"}):
			return []byte(`[]`), nil, nil
		case hasPrefix(args, []string{"ad", "app", "create"}):
			return []byte(`{"appId":"app-1","id":"obj-1"}`), nil, nil
		case hasPrefix(args, []string{"ad", "sp", "create"}):
			return []byte(`{"id":"sp-1"}`), nil, nil
		case hasPrefix(args, []string{"ad", "app", "credential", "reset"}):
			return []byte(`{"password":"sek"}`), nil, nil
		default:
			return nil, nil, nil
		}
	}
}

func TestProvision_HappyPath(t *testing.T) {
	runner := &scriptedRunner{handler: mintHandler()}
	cli := NewCLIWithRunner(runner)

	ds := newDSClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"datasource":{"uid":"uid-1","name":"gcx-azure-monitor","type":"grafana-azure-monitor-datasource"}}`))
	}))

	res, err := Provision(context.Background(), RunDeps{CLI: cli, DS: ds},
		ProvisionInput{
			Account:    Account{TenantID: "t1", SubID: "sub-1", CloudName: "AzureCloud"},
			CallerOID:  "oid",
			SkipHealth: true,
			Selections: []Selection{{
				Suggestion: Suggestion{Spec: azureMonitorSpec{}, Name: "gcx-azure-monitor", Scopes: []string{"/subscriptions/sub-1"}},
				Roles:      []string{"Reader", "Monitoring Reader"},
			}},
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Datasources) != 1 {
		t.Fatalf("expected 1 datasource, got %d", len(res.Datasources))
	}
	d := res.Datasources[0]
	if d.UID != "uid-1" || d.Credential == nil || d.Credential.ID != "app-1" {
		t.Fatalf("unexpected result: %+v", d)
	}
	if d.Status != onboard.StatusCreated {
		t.Fatalf("expected status %q, got %q", onboard.StatusCreated, d.Status)
	}
}

func TestProvision_InstallsMissingPlugin(t *testing.T) {
	runner := &scriptedRunner{handler: mintHandler()}
	cli := NewCLIWithRunner(runner)

	// Grafana server: datasource API + plugin API. The (non-core) ADX plugin is
	// reported missing (404) until it is installed.
	var installed bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/plugins/"+KindADX+"/install":
			installed = true
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/api/plugins/"+KindADX+"/settings":
			if installed {
				_, _ = w.Write([]byte(`{"id":"` + KindADX + `"}`))
				return
			}
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodGet && r.URL.Path == "/api/datasources":
			_, _ = w.Write([]byte(`[]`))
		default:
			_, _ = w.Write([]byte(`{"datasource":{"uid":"uid-1","name":"gcx-adx","type":"` + KindADX + `"}}`))
		}
	}))
	t.Cleanup(server.Close)
	cfg := config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}}
	ds, err := datasources.NewClient(cfg)
	if err != nil {
		t.Fatalf("ds client: %v", err)
	}
	pl, err := plugins.NewClient(cfg)
	if err != nil {
		t.Fatalf("plugins client: %v", err)
	}

	_, err = Provision(context.Background(), RunDeps{CLI: cli, DS: ds, Plugins: pl},
		ProvisionInput{
			Account:    Account{TenantID: "t1", SubID: "sub-1", CloudName: "AzureCloud"},
			CallerOID:  "oid",
			SkipHealth: true,
			Selections: []Selection{{
				Suggestion: Suggestion{
					Spec:   adxSpec{},
					Name:   "gcx-adx",
					Scopes: []string{"/subscriptions/sub-1"},
					Extra:  map[string]string{"clusterUrl": "https://adx1.kusto.windows.net", "rg": "rg1", "cluster": "adx1"},
				},
				Roles: []string{"Reader"},
			}},
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !installed {
		t.Fatal("expected missing plugin to be auto-installed (no confirmer set)")
	}
}

func TestProvision_UnavailablePluginSkipsOnlyThatDatasource(t *testing.T) {
	runner := &scriptedRunner{handler: mintHandler()}
	cli := NewCLIWithRunner(runner)

	// Grafana server: the (non-core) ADX plugin is missing and its install
	// fails, so the ADX datasource must be skipped — while the core Azure
	// Monitor datasource still provisions.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/plugins/"+KindADX+"/install":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"enterprise plugin not licensed"}`))
		case r.URL.Path == "/api/plugins/"+KindADX+"/settings":
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodGet && r.URL.Path == "/api/datasources":
			_, _ = w.Write([]byte(`[]`))
		default:
			_, _ = w.Write([]byte(`{"datasource":{"uid":"uid-am","name":"gcx-azure-monitor","type":"` + KindAzureMonitor + `"}}`))
		}
	}))
	t.Cleanup(server.Close)
	cfg := config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}}
	ds, err := datasources.NewClient(cfg)
	if err != nil {
		t.Fatalf("ds client: %v", err)
	}
	pl, err := plugins.NewClient(cfg)
	if err != nil {
		t.Fatalf("plugins client: %v", err)
	}

	res, err := Provision(context.Background(), RunDeps{CLI: cli, DS: ds, Plugins: pl},
		ProvisionInput{
			Account:    Account{TenantID: "t1", SubID: "sub-1", CloudName: "AzureCloud"},
			CallerOID:  "oid",
			SkipHealth: true,
			Selections: []Selection{
				{
					Suggestion: Suggestion{Spec: azureMonitorSpec{}, Name: "gcx-azure-monitor", Scopes: []string{"/subscriptions/sub-1"}},
					Roles:      []string{"Reader"},
				},
				{
					Suggestion: Suggestion{
						Spec:   adxSpec{},
						Name:   "gcx-adx",
						Scopes: []string{"/subscriptions/sub-1"},
						Extra:  map[string]string{"clusterUrl": "https://adx1.kusto.windows.net", "rg": "rg1", "cluster": "adx1"},
					},
					Roles: []string{"Reader"},
				},
			},
		})
	if err != nil {
		t.Fatalf("expected no top-level error when a plugin is unavailable, got %v", err)
	}
	if len(res.Datasources) != 2 {
		t.Fatalf("expected 2 result rows, got %d: %+v", len(res.Datasources), res.Datasources)
	}
	if res.Datasources[0].Status != onboard.StatusCreated {
		t.Fatalf("expected azure monitor to be created, got %+v", res.Datasources[0])
	}
	if res.Datasources[1].Status != onboard.StatusSkipped || res.Datasources[1].Note == "" {
		t.Fatalf("expected ADX to be skipped with a note, got %+v", res.Datasources[1])
	}
	// The skipped ADX datasource must not have minted or left an app registration.
	for _, c := range runner.calls {
		if hasPrefix(c, []string{"ad", "app", "delete"}) {
			t.Fatal("did not expect a rollback delete when a datasource is softly skipped")
		}
	}
}

func TestProvision_PluginPreflightDedupedPerKind(t *testing.T) {
	runner := &scriptedRunner{handler: mintHandler()}
	cli := NewCLIWithRunner(runner)

	// Two ADX datasources, plugin missing. The install attempt must happen at
	// most once for the shared plugin kind — not once per datasource.
	var installs int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/plugins/"+KindADX+"/install":
			installs++
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"enterprise plugin not licensed"}`))
		case r.URL.Path == "/api/plugins/"+KindADX+"/settings":
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodGet && r.URL.Path == "/api/datasources":
			_, _ = w.Write([]byte(`[]`))
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(server.Close)
	cfg := config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}}
	ds, err := datasources.NewClient(cfg)
	if err != nil {
		t.Fatalf("ds client: %v", err)
	}
	pl, err := plugins.NewClient(cfg)
	if err != nil {
		t.Fatalf("plugins client: %v", err)
	}

	var confirms int
	adx := func(name string) Selection {
		return Selection{
			Suggestion: Suggestion{
				Spec:   adxSpec{},
				Name:   name,
				Scopes: []string{"/subscriptions/sub-1"},
				Extra:  map[string]string{"clusterUrl": "https://" + name + ".kusto.windows.net", "rg": "rg1", "cluster": name},
			},
			Roles: []string{"Reader"},
		}
	}

	res, err := Provision(context.Background(), RunDeps{
		CLI: cli, DS: ds, Plugins: pl,
		ConfirmInstallPlugin: func(string) (bool, error) { confirms++; return true, nil },
	}, ProvisionInput{
		Account:    Account{TenantID: "t1", SubID: "sub-1", CloudName: "AzureCloud"},
		CallerOID:  "oid",
		SkipHealth: true,
		Selections: []Selection{adx("gcx-adx1"), adx("gcx-adx2")},
	})
	if err != nil {
		t.Fatalf("expected no top-level error, got %v", err)
	}
	if len(res.Datasources) != 2 ||
		res.Datasources[0].Status != onboard.StatusSkipped ||
		res.Datasources[1].Status != onboard.StatusSkipped {
		t.Fatalf("expected both ADX datasources skipped, got %+v", res.Datasources)
	}
	if installs != 1 {
		t.Fatalf("expected exactly one install attempt for the shared plugin kind, got %d", installs)
	}
	if confirms != 1 {
		t.Fatalf("expected exactly one install prompt for the shared plugin kind, got %d", confirms)
	}
}

func TestEnsurePlugin_SkipsCoreAzureMonitor(t *testing.T) {
	// A plugins client pointed at a server that fails every request; the core
	// Azure Monitor plugin must be skipped without any call being made.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("plugin API should not be called for a core datasource")
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	pl, err := plugins.NewClient(config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}})
	if err != nil {
		t.Fatalf("plugins client: %v", err)
	}

	if err := ensurePlugin(context.Background(), RunDeps{Plugins: pl}, KindAzureMonitor); err != nil {
		t.Fatalf("expected core plugin to be skipped, got %v", err)
	}
}

func TestProvision_RollsBackAppRegOnDatasourceFailure(t *testing.T) {
	runner := &scriptedRunner{handler: mintHandler()}
	cli := NewCLIWithRunner(runner)

	ds := newDSClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"boom"}`))
	}))

	_, err := Provision(context.Background(), RunDeps{CLI: cli, DS: ds},
		ProvisionInput{
			Account:    Account{TenantID: "t1", SubID: "sub-1", CloudName: "AzureCloud"},
			CallerOID:  "oid",
			SkipHealth: true,
			Selections: []Selection{{
				Suggestion: Suggestion{Spec: azureMonitorSpec{}, Name: "gcx-azure-monitor", Scopes: []string{"/subscriptions/sub-1"}},
				Roles:      []string{"Reader"},
			}},
		})
	if err == nil {
		t.Fatal("expected error when datasource creation fails")
	}

	var deleted bool
	for _, c := range runner.calls {
		if hasPrefix(c, []string{"ad", "app", "delete"}) {
			deleted = true
		}
	}
	if !deleted {
		t.Fatal("expected app registration to be deleted during rollback")
	}
}

func TestProvision_IdempotentReusesExistingDatasource(t *testing.T) {
	runner := &scriptedRunner{handler: mintHandler()}
	cli := NewCLIWithRunner(runner)

	// The datasource already exists; a re-run must reuse it (no create, no mint).
	ds := newDSClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`[{"uid":"uid-existing","name":"gcx-azure-monitor","type":"grafana-azure-monitor-datasource"}]`))
			return
		}
		t.Errorf("did not expect a write to the datasource API on an idempotent re-run, got %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	}))

	res, err := Provision(context.Background(), RunDeps{CLI: cli, DS: ds},
		ProvisionInput{
			Account:    Account{TenantID: "t1", SubID: "sub-1", CloudName: "AzureCloud"},
			CallerOID:  "oid",
			SkipHealth: true,
			Selections: []Selection{{
				Suggestion: Suggestion{Spec: azureMonitorSpec{}, Name: "gcx-azure-monitor", Scopes: []string{"/subscriptions/sub-1"}},
				Roles:      []string{"Reader"},
			}},
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Datasources) != 1 || res.Datasources[0].Status != onboard.StatusExisting {
		t.Fatalf("expected one existing datasource, got %+v", res.Datasources)
	}
	if res.Datasources[0].UID != "uid-existing" {
		t.Fatalf("expected existing uid, got %q", res.Datasources[0].UID)
	}
	for _, c := range runner.calls {
		if hasPrefix(c, []string{"ad", "app", "create"}) {
			t.Fatal("did not expect any app registration to be minted on an idempotent re-run")
		}
	}
}

func TestProvision_DryRunMintsNothing(t *testing.T) {
	runner := &scriptedRunner{handler: mintHandler()}
	cli := NewCLIWithRunner(runner)

	ds := newDSClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		t.Errorf("did not expect a datasource write during --dry-run")
		w.WriteHeader(http.StatusInternalServerError)
	}))

	res, err := Provision(context.Background(), RunDeps{CLI: cli, DS: ds},
		ProvisionInput{
			Account:   Account{TenantID: "t1", SubID: "sub-1", CloudName: "AzureCloud"},
			CallerOID: "oid",
			DryRun:    true,
			Selections: []Selection{{
				Suggestion: Suggestion{Spec: azureMonitorSpec{}, Name: "gcx-azure-monitor", Scopes: []string{"/subscriptions/sub-1"}},
				Roles:      []string{"Reader", "Monitoring Reader"},
			}},
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.DryRun {
		t.Fatal("expected result to be marked as a dry run")
	}
	if len(res.Datasources) != 1 || res.Datasources[0].Status != onboard.StatusPlanned {
		t.Fatalf("expected one planned datasource, got %+v", res.Datasources)
	}
	if cred := res.Datasources[0].Credential; cred == nil || len(cred.Roles) != 2 {
		t.Fatalf("expected planned roles to be surfaced, got %+v", res.Datasources[0].Credential)
	}
	for _, c := range runner.calls {
		if hasPrefix(c, []string{"ad", "app", "create"}) {
			t.Fatal("did not expect any Azure mutation during --dry-run")
		}
	}
}

func TestProvision_TagsAppRegistrationWithDatasourceUID(t *testing.T) {
	runner := &scriptedRunner{handler: mintHandler()}
	cli := NewCLIWithRunner(runner)

	ds := newDSClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		_, _ = w.Write([]byte(`{"datasource":{"uid":"uid-42","name":"gcx-azure-monitor","type":"grafana-azure-monitor-datasource"}}`))
	}))

	_, err := Provision(context.Background(), RunDeps{CLI: cli, DS: ds},
		ProvisionInput{
			Account:    Account{TenantID: "t1", SubID: "sub-1", CloudName: "AzureCloud"},
			CallerOID:  "owner-oid",
			Stack:      "mystack",
			SkipHealth: true,
			Selections: []Selection{{
				Suggestion: Suggestion{Spec: azureMonitorSpec{}, Name: "gcx-azure-monitor", Scopes: []string{"/subscriptions/sub-1"}},
				Roles:      []string{"Reader"},
			}},
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var tagged bool
	for _, c := range runner.calls {
		if hasPrefix(c, []string{"rest", "--method", "PATCH"}) {
			joined := strings.Join(c, " ")
			if !strings.Contains(joined, tagDatasourcePrefix+"uid-42") ||
				!strings.Contains(joined, tagOwnerPrefix+"owner-oid") ||
				!strings.Contains(joined, tagStackPrefix+"mystack") ||
				!strings.Contains(joined, TagManaged) {
				t.Fatalf("tag PATCH missing expected attribution: %s", joined)
			}
			tagged = true
		}
	}
	if !tagged {
		t.Fatal("expected the app registration to be tagged with attribution after datasource creation")
	}
}

func TestProvision_PrivateNetworkAddsPDCHint(t *testing.T) {
	newDS := func(t *testing.T) *datasources.Client {
		t.Helper()
		return newDSClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet {
				_, _ = w.Write([]byte(`[]`))
				return
			}
			_, _ = w.Write([]byte(`{"datasource":{"uid":"uid-1","name":"gcx-azure-monitor","type":"grafana-azure-monitor-datasource"}}`))
		}))
	}

	sel := Selection{
		Suggestion: Suggestion{
			Spec:           azureMonitorSpec{},
			Name:           "gcx-azure-monitor",
			Scopes:         []string{"/subscriptions/sub-1"},
			PrivateNetwork: true,
		},
		Roles: []string{"Reader"},
	}

	// Cloud stack (non-empty Stack) → advisory PDC hint with docs link.
	runner := &scriptedRunner{handler: mintHandler()}
	res, err := Provision(context.Background(), RunDeps{CLI: NewCLIWithRunner(runner), DS: newDS(t)},
		ProvisionInput{
			Account:    Account{TenantID: "t1", SubID: "sub-1", CloudName: "AzureCloud"},
			CallerOID:  "oid",
			Stack:      "mystack",
			SkipHealth: true,
			Selections: []Selection{sel},
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d := res.Datasources[0]; d.Hint == "" || d.HintDocs != docs.PrivateDataSourceConnect {
		t.Fatalf("expected PDC hint with docs link on a private resource for a cloud stack, got %+v", d)
	}

	// Self-managed target (empty Stack) → no PDC hint (PDC is Cloud-only).
	runner = &scriptedRunner{handler: mintHandler()}
	res, err = Provision(context.Background(), RunDeps{CLI: NewCLIWithRunner(runner), DS: newDS(t)},
		ProvisionInput{
			Account:    Account{TenantID: "t1", SubID: "sub-1", CloudName: "AzureCloud"},
			CallerOID:  "oid",
			SkipHealth: true,
			Selections: []Selection{sel},
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d := res.Datasources[0]; d.Hint != "" || d.HintDocs != "" {
		t.Fatalf("did not expect a PDC hint for a self-managed target, got %+v", d)
	}
}

func TestProvision_HealthCheckRecordsStatus(t *testing.T) {
	runner := &scriptedRunner{handler: mintHandler()}
	cli := NewCLIWithRunner(runner)

	ds := newDSClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/health"):
			_, _ = w.Write([]byte(`{"status":"OK","message":"ok"}`))
		case r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`[]`))
		default:
			_, _ = w.Write([]byte(`{"datasource":{"uid":"uid-1","name":"gcx-azure-monitor","type":"grafana-azure-monitor-datasource"}}`))
		}
	}))

	res, err := Provision(context.Background(), RunDeps{CLI: cli, DS: ds, HealthAttempts: 1},
		ProvisionInput{
			Account:   Account{TenantID: "t1", SubID: "sub-1", CloudName: "AzureCloud"},
			CallerOID: "oid",
			Selections: []Selection{{
				Suggestion: Suggestion{Spec: azureMonitorSpec{}, Name: "gcx-azure-monitor", Scopes: []string{"/subscriptions/sub-1"}},
				Roles:      []string{"Reader"},
			}},
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := res.Datasources[0].Health; !strings.EqualFold(got, "OK") {
		t.Fatalf("expected health OK, got %q", got)
	}
}
