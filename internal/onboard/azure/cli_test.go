//nolint:testpackage // white-box tests exercise unexported CLI sequencing helpers
package azure

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// scriptedRunner returns canned stdout/err based on the leading args and
// records the order of calls.
type scriptedRunner struct {
	calls   [][]string
	handler func(args []string) (stdout, stderr []byte, err error)
	lookErr error
}

func (s *scriptedRunner) LookPath(string) error { return s.lookErr }

func (s *scriptedRunner) Run(_ context.Context, _ string, args ...string) ([]byte, []byte, error) {
	s.calls = append(s.calls, args)
	if s.handler != nil {
		return s.handler(args)
	}
	return nil, nil, nil
}

func hasPrefix(args, prefix []string) bool {
	if len(args) < len(prefix) {
		return false
	}
	for i := range prefix {
		if args[i] != prefix[i] {
			return false
		}
	}
	return true
}

func TestCreateOwnedAppRegistration_SetsOwnersAtCreationTime(t *testing.T) {
	runner := &scriptedRunner{
		handler: func(args []string) ([]byte, []byte, error) {
			switch {
			case hasPrefix(args, []string{"ad", "app", "create"}):
				return []byte(`{"appId":"app-123","id":"obj-1"}`), nil, nil
			case hasPrefix(args, []string{"ad", "sp", "create"}):
				return []byte(`{"id":"sp-1"}`), nil, nil
			case hasPrefix(args, []string{"ad", "app", "credential", "reset"}):
				return []byte(`{"password":"s3cret"}`), nil, nil
			default:
				return nil, nil, nil
			}
		},
	}
	cli := NewCLIWithRunner(runner)

	cred, err := cli.CreateOwnedAppRegistration(context.Background(), AppRegistrationRequest{
		Name:      "gcx-azure-monitor",
		Roles:     []string{"Reader", "Monitoring Reader"},
		Scopes:    []string{"/subscriptions/sub-1"},
		CallerOID: "caller-oid",
		Tenant:    "tenant-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cred.AppID != "app-123" || cred.Password != "s3cret" || cred.Tenant != "tenant-1" {
		t.Fatalf("unexpected credential: %+v", cred)
	}

	// Verify ordering: app create -> app owner add -> sp create -> sp owner (rest)
	// -> role assignments -> credential reset.
	idx := func(prefix ...string) int {
		for i, c := range runner.calls {
			if hasPrefix(c, prefix) {
				return i
			}
		}
		return -1
	}
	appCreate := idx("ad", "app", "create")
	appOwner := idx("ad", "app", "owner", "add")
	spCreate := idx("ad", "sp", "create")
	spOwner := idx("rest", "--method", "POST")
	roleAssign := idx("role", "assignment", "create")
	credReset := idx("ad", "app", "credential", "reset")

	ordered := appCreate < appOwner && appOwner < spCreate && spCreate < spOwner &&
		spOwner < roleAssign && roleAssign < credReset
	if !ordered {
		t.Fatalf("unexpected call ordering: appCreate=%d appOwner=%d spCreate=%d spOwner=%d roleAssign=%d credReset=%d",
			appCreate, appOwner, spCreate, spOwner, roleAssign, credReset)
	}

	// Both roles should be assigned over the single scope.
	roleAssignments := 0
	for _, c := range runner.calls {
		if hasPrefix(c, []string{"role", "assignment", "create"}) {
			roleAssignments++
		}
	}
	if roleAssignments != 2 {
		t.Fatalf("expected 2 role assignments, got %d", roleAssignments)
	}
}

func TestCreateOwnedAppRegistration_RegistersRollback(t *testing.T) {
	runner := &scriptedRunner{
		handler: func(args []string) ([]byte, []byte, error) {
			if hasPrefix(args, []string{"ad", "app", "create"}) {
				return []byte(`{"appId":"app-xyz","id":"obj-1"}`), nil, nil
			}
			// Fail at owner add to ensure the rollback was already registered.
			if hasPrefix(args, []string{"ad", "app", "owner", "add"}) {
				return nil, []byte("AuthorizationFailed"), errors.New("exit 1")
			}
			return nil, nil, nil
		},
	}
	cli := NewCLIWithRunner(runner)

	var undos []string
	addUndo := func(desc string, _ func(ctx context.Context) error) { undos = append(undos, desc) }

	_, err := cli.CreateOwnedAppRegistration(context.Background(), AppRegistrationRequest{
		Name:      "gcx-adx-foo",
		Roles:     []string{"Reader"},
		Scopes:    []string{"/subscriptions/sub-1"},
		CallerOID: "caller-oid",
		Tenant:    "tenant-1",
		AddUndo:   addUndo,
	})
	if !errors.Is(err, ErrInsufficientPrivilege) {
		t.Fatalf("expected ErrInsufficientPrivilege, got %v", err)
	}
	if len(undos) != 1 || !strings.Contains(undos[0], "gcx-adx-foo") {
		t.Fatalf("expected rollback registered for the app reg, got %v", undos)
	}
}

func TestGrantADXClusterViewer_ClassifiesAuthFailure(t *testing.T) {
	runner := &scriptedRunner{
		handler: func([]string) ([]byte, []byte, error) {
			return nil, []byte("ERROR: AuthorizationFailed"), errors.New("exit 1")
		},
	}
	cli := NewCLIWithRunner(runner)

	err := cli.GrantADXClusterViewer(context.Background(), "rg", "cluster", "gcx-assign", "app-1", "tenant-1")
	if !errors.Is(err, ErrInsufficientPrivilege) {
		t.Fatalf("expected ErrInsufficientPrivilege, got %v", err)
	}
}

func TestAppExists(t *testing.T) {
	runner := &scriptedRunner{
		handler: func([]string) ([]byte, []byte, error) {
			return []byte(`["app-1"]`), nil, nil
		},
	}
	cli := NewCLIWithRunner(runner)

	exists, err := cli.AppExists(context.Background(), "gcx-azure-monitor")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists {
		t.Fatal("expected app to exist")
	}
}
