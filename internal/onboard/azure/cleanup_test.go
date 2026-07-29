//nolint:testpackage // white-box test exercises unexported cleanup helpers
package azure

import (
	"context"
	"net/http"
	"slices"
	"testing"
)

func TestCleanup_RemovesADXAssignments(t *testing.T) {
	runner := &scriptedRunner{
		handler: func(args []string) ([]byte, []byte, error) {
			switch {
			case hasPrefix(args, []string{"ad", "app", "list"}):
				return []byte(`[]`), nil, nil
			case hasPrefix(args, []string{"kusto", "cluster", "list"}):
				return []byte(`[{"name":"adx1","uri":"https://adx1.kusto.windows.net","resourceGroup":"rg1","state":"Running"}]`), nil, nil
			case hasPrefix(args, []string{"kusto", "cluster-principal-assignment", "list"}):
				// One gcx-created assignment and one unrelated assignment.
				return []byte(`[{"name":"adx1/gcx-abc123"},{"name":"adx1/someone-else"}]`), nil, nil
			default:
				return nil, nil, nil
			}
		},
	}
	cli := NewCLIWithRunner(runner)

	// Datasource API: no datasources to remove.
	ds := newDSClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	}))

	res, err := Cleanup(context.Background(), RunDeps{CLI: cli, DS: ds}, CleanupInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var gotAssignment bool
	for _, c := range res.Cleaned {
		if c.Kind == "adx-assignment" {
			gotAssignment = true
			if c.Name != "gcx-abc123" {
				t.Fatalf("unexpected assignment name %q", c.Name)
			}
		}
	}
	if !gotAssignment {
		t.Fatal("expected the gcx- ADX assignment to be cleaned up")
	}

	// Verify the unrelated assignment was NOT deleted and the gcx one was.
	var deletedGcx, deletedOther bool
	for _, call := range runner.calls {
		if !hasPrefix(call, []string{"kusto", "cluster-principal-assignment", "delete"}) {
			continue
		}
		switch {
		case slices.Contains(call, "gcx-abc123"):
			deletedGcx = true
		case slices.Contains(call, "someone-else"):
			deletedOther = true
		}
	}
	if !deletedGcx {
		t.Fatal("expected gcx assignment to be deleted")
	}
	if deletedOther {
		t.Fatal("did not expect the unrelated assignment to be deleted")
	}
}

func TestCleanup_OnlyRemovesGcxManagedAppsForCaller(t *testing.T) {
	runner := &scriptedRunner{
		handler: func(args []string) ([]byte, []byte, error) {
			switch {
			case hasPrefix(args, []string{"ad", "app", "list"}):
				// Three gcx-prefixed apps: ours, another owner's, and an untagged one.
				return []byte(`[
					{"appId":"mine","displayName":"gcx-mine","tags":["gcx:managed","gcx:owner=me"]},
					{"appId":"theirs","displayName":"gcx-theirs","tags":["gcx:managed","gcx:owner=someone-else"]},
					{"appId":"legacy","displayName":"gcx-legacy","tags":[]}
				]`), nil, nil
			case hasPrefix(args, []string{"kusto", "cluster", "list"}):
				return []byte(`[]`), nil, nil
			default:
				return nil, nil, nil
			}
		},
	}
	cli := NewCLIWithRunner(runner)
	ds := newDSClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	}))

	res, err := Cleanup(context.Background(), RunDeps{CLI: cli, DS: ds}, CleanupInput{CallerOID: "me"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(res.Cleaned) != 1 || res.Cleaned[0].ID != "mine" {
		t.Fatalf("expected only the caller's gcx-managed app to be cleaned, got %+v", res.Cleaned)
	}
	for _, c := range runner.calls {
		if hasPrefix(c, []string{"ad", "app", "delete"}) {
			if slices.Contains(c, "theirs") || slices.Contains(c, "legacy") {
				t.Fatalf("must not delete apps not attributable to the caller: %v", c)
			}
		}
	}
}

func TestCleanup_DryRunDeletesNothing(t *testing.T) {
	runner := &scriptedRunner{
		handler: func(args []string) ([]byte, []byte, error) {
			switch {
			case hasPrefix(args, []string{"ad", "app", "list"}):
				return []byte(`[{"appId":"mine","displayName":"gcx-mine","tags":["gcx:managed"]}]`), nil, nil
			case hasPrefix(args, []string{"kusto", "cluster", "list"}):
				return []byte(`[]`), nil, nil
			default:
				return nil, nil, nil
			}
		},
	}
	cli := NewCLIWithRunner(runner)
	ds := newDSClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"uid":"u1","name":"gcx-azure-monitor","type":"grafana-azure-monitor-datasource"}]`))
	}))

	res, err := Cleanup(context.Background(), RunDeps{CLI: cli, DS: ds}, CleanupInput{DryRun: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.DryRun {
		t.Fatal("expected result to be marked as a dry run")
	}
	for _, c := range res.Cleaned {
		if !c.Planned {
			t.Fatalf("dry-run entries must be marked planned, got %+v", c)
		}
	}
	for _, c := range runner.calls {
		if hasPrefix(c, []string{"ad", "app", "delete"}) {
			t.Fatal("must not delete any app during --dry-run")
		}
	}
}
