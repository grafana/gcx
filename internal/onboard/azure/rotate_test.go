//nolint:testpackage // white-box tests exercise the rotate orchestrator
package azure

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/gcx/internal/onboard"
)

// isByUID reports whether the request targets the single-datasource by-uid GET
// (which returns jsonData) rather than the list (which omits it).
func isByUID(r *http.Request) bool {
	return strings.Contains(r.URL.Path, "/uid/")
}

// rotateHandler answers the az calls a rotation makes: tag read, credential
// reset (append), credential list, credential delete.
func rotateHandler(tags string) func(args []string) ([]byte, []byte, error) {
	return func(args []string) ([]byte, []byte, error) {
		switch {
		case hasPrefix(args, []string{"ad", "app", "show"}):
			return []byte(tags), nil, nil
		case hasPrefix(args, []string{"ad", "app", "credential", "reset"}):
			return []byte(`{"password":"new-secret","keyId":"key-new"}`), nil, nil
		case hasPrefix(args, []string{"ad", "app", "credential", "list"}):
			return []byte(`[{"keyId":"key-old"},{"keyId":"key-new"}]`), nil, nil
		default:
			return nil, nil, nil
		}
	}
}

func TestRotate_RotatesGcxManagedDatasource(t *testing.T) {
	runner := &scriptedRunner{handler: rotateHandler(`["gcx:managed","gcx:owner=owner-oid"]`)}
	cli := NewCLIWithRunner(runner)

	var updated bool
	ds := newDSClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && isByUID(r):
			_, _ = w.Write([]byte(`{"uid":"uid-1","name":"gcx-azure-monitor","type":"grafana-azure-monitor-datasource","jsonData":{"clientId":"app-1"}}`))
		case r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`[{"uid":"uid-1","name":"gcx-azure-monitor","type":"grafana-azure-monitor-datasource"}]`))
		case r.Method == http.MethodPut:
			updated = true
			_, _ = w.Write([]byte(`{"datasource":{"uid":"uid-1","name":"gcx-azure-monitor","type":"grafana-azure-monitor-datasource"}}`))
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))

	res, err := Rotate(context.Background(), RunDeps{CLI: cli, DS: ds},
		RotateInput{CallerOID: "owner-oid", SkipHealth: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !updated {
		t.Fatal("expected the datasource to be updated with the new secret")
	}
	if len(res.Datasources) != 1 || res.Datasources[0].Status != onboard.StatusRotated {
		t.Fatalf("expected one rotated datasource, got %+v", res.Datasources)
	}

	// The superseded secret (key-old) must be pruned, the new one (key-new) kept.
	var deletedOld, deletedNew bool
	for _, c := range runner.calls {
		if hasPrefix(c, []string{"ad", "app", "credential", "delete"}) {
			if slices.Contains(c, "key-old") {
				deletedOld = true
			}
			if slices.Contains(c, "key-new") {
				deletedNew = true
			}
		}
	}
	if !deletedOld {
		t.Fatal("expected the old secret to be pruned")
	}
	if deletedNew {
		t.Fatal("must not prune the newly minted secret")
	}
}

func TestRotate_SkipsNonGcxManagedApp(t *testing.T) {
	// The app registration carries no gcx-managed tag: rotation must not touch it.
	runner := &scriptedRunner{handler: rotateHandler(`["someone-else"]`)}
	cli := NewCLIWithRunner(runner)

	ds := newDSClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && isByUID(r):
			_, _ = w.Write([]byte(`{"uid":"uid-1","name":"gcx-azure-monitor","type":"grafana-azure-monitor-datasource","jsonData":{"clientId":"app-1"}}`))
		case r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`[{"uid":"uid-1","name":"gcx-azure-monitor","type":"grafana-azure-monitor-datasource"}]`))
		default:
			t.Errorf("did not expect a datasource write for a non-gcx-managed app")
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))

	res, err := Rotate(context.Background(), RunDeps{CLI: cli, DS: ds}, RotateInput{CallerOID: "owner-oid"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Datasources) != 1 || res.Datasources[0].Status != onboard.StatusSkipped {
		t.Fatalf("expected the datasource to be skipped, got %+v", res.Datasources)
	}
	for _, c := range runner.calls {
		if hasPrefix(c, []string{"ad", "app", "credential", "reset"}) {
			t.Fatal("must not reset a secret on a non-gcx-managed app")
		}
	}
}

func TestRotate_DryRunChangesNothing(t *testing.T) {
	runner := &scriptedRunner{handler: rotateHandler(`["gcx:managed"]`)}
	cli := NewCLIWithRunner(runner)

	ds := newDSClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && isByUID(r):
			_, _ = w.Write([]byte(`{"uid":"uid-1","name":"gcx-adx","type":"` + KindADX + `","jsonData":{"azureCredentials":{"clientId":"app-1"}}}`))
		case r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`[{"uid":"uid-1","name":"gcx-adx","type":"` + KindADX + `"}]`))
		default:
			t.Errorf("did not expect a datasource write during rotate --dry-run")
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))

	res, err := Rotate(context.Background(), RunDeps{CLI: cli, DS: ds}, RotateInput{DryRun: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.DryRun || len(res.Datasources) != 1 || res.Datasources[0].Status != onboard.StatusPlanned {
		t.Fatalf("expected a planned dry-run result, got %+v", res)
	}
	for _, c := range runner.calls {
		if hasPrefix(c, []string{"ad", "app", "credential", "reset"}) {
			t.Fatal("must not mint a secret during --dry-run")
		}
	}
}

func TestCredentialRef(t *testing.T) {
	cases := []struct {
		name      string
		ds        map[string]any
		dsType    string
		wantApp   string
		wantField string
	}{
		{
			name:      "azure monitor flat clientId",
			dsType:    KindAzureMonitor,
			ds:        map[string]any{"clientId": "app-am"},
			wantApp:   "app-am",
			wantField: "clientSecret",
		},
		{
			name:      "adx nested clientId",
			dsType:    KindADX,
			ds:        map[string]any{"azureCredentials": map[string]any{"clientId": "app-adx"}},
			wantApp:   "app-adx",
			wantField: "azureClientSecret",
		},
		{
			name:      "cosmos has no rotatable secret",
			dsType:    KindCosmos,
			ds:        map[string]any{},
			wantApp:   "",
			wantField: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			app, field := credentialRef(&datasources.Datasource{Type: c.dsType, JSONData: c.ds})
			if app != c.wantApp || field != c.wantField {
				t.Fatalf("credentialRef = (%q,%q), want (%q,%q)", app, field, c.wantApp, c.wantField)
			}
		})
	}
}
