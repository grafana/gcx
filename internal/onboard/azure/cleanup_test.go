//nolint:testpackage // white-box test exercises unexported cleanup helpers
package azure

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"testing"
)

func TestCleanup_RemovesOnlyOwnedADXAssignments(t *testing.T) {
	// onboardAssignmentName("abc123") == "gcx-abc123", so the caller's owned app
	// backs the assignment "gcx-abc123". A second gcx assignment ("gcx-deadbeef")
	// belongs to no owned app and must be left alone, as must a non-gcx one.
	runner := &scriptedRunner{
		handler: func(args []string) ([]byte, []byte, error) {
			switch {
			case hasPrefix(args, []string{"ad", "app", "list"}):
				return []byte(`[{"appId":"abc123","displayName":"gcx-adx","tags":["gcx:managed","gcx:owner=me"]}]`), nil, nil
			case hasPrefix(args, []string{"kusto", "cluster", "list"}):
				return []byte(`[{"name":"adx1","uri":"https://adx1.kusto.windows.net","resourceGroup":"rg1","state":"Running"}]`), nil, nil
			case hasPrefix(args, []string{"kusto", "cluster-principal-assignment", "list"}):
				return []byte(`[{"name":"adx1/gcx-abc123"},{"name":"adx1/gcx-deadbeef"},{"name":"adx1/someone-else"}]`), nil, nil
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

	res, err := Cleanup(context.Background(), RunDeps{CLI: cli, DS: ds}, CleanupInput{CallerOID: "me"})
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
		t.Fatal("expected the caller's owned ADX assignment to be cleaned up")
	}

	// Only the owned assignment may be deleted; the other gcx app's assignment
	// and the unrelated one must be untouched.
	var deletedOwned, deletedOtherGcx, deletedUnrelated bool
	for _, call := range runner.calls {
		if !hasPrefix(call, []string{"kusto", "cluster-principal-assignment", "delete"}) {
			continue
		}
		switch {
		case slices.Contains(call, "gcx-abc123"):
			deletedOwned = true
		case slices.Contains(call, "gcx-deadbeef"):
			deletedOtherGcx = true
		case slices.Contains(call, "someone-else"):
			deletedUnrelated = true
		}
	}
	if !deletedOwned {
		t.Fatal("expected the caller's owned assignment to be deleted")
	}
	if deletedOtherGcx {
		t.Fatal("must not delete a gcx assignment backing another owner's app")
	}
	if deletedUnrelated {
		t.Fatal("must not delete an unrelated assignment")
	}
}

func TestCleanup_RefusesWithoutCallerScope(t *testing.T) {
	// A real (non-dry-run) cleanup with no caller OID must refuse rather than
	// sweep every gcx-managed app in the tenant.
	runner := &scriptedRunner{handler: func(args []string) ([]byte, []byte, error) {
		if hasPrefix(args, []string{"ad", "app", "list"}) {
			return []byte(`[]`), nil, nil
		}
		return nil, nil, nil
	}}
	cli := NewCLIWithRunner(runner)
	ds := newDSClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		t.Error("cleanup must not mutate anything in this test")
		w.WriteHeader(http.StatusInternalServerError)
	}))

	_, err := Cleanup(context.Background(), RunDeps{CLI: cli, DS: ds}, CleanupInput{})
	if !errors.Is(err, ErrUnscopedDestructive) {
		t.Fatalf("expected ErrUnscopedDestructive, got %v", err)
	}

	// A dry-run with no caller OID is still allowed (read-only).
	if _, err := Cleanup(context.Background(), RunDeps{CLI: cli, DS: ds}, CleanupInput{DryRun: true}); err != nil {
		t.Fatalf("dry-run cleanup should be allowed without a caller OID, got %v", err)
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
