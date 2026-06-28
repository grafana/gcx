package datasources

import (
	"errors"
	"testing"

	dsclient "github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/gcx/internal/gcxerrors"
)

func TestSchemasGetValidate_QueryKindIsUsageError(t *testing.T) {
	opts := &schemasGetOpts{Type: "grafana-sentry-datasource", Kind: "query"}
	err := opts.Validate()
	if err == nil {
		t.Fatal("expected error for --kind query")
	}
	var de gcxerrors.DetailedError
	if !errors.As(err, &de) {
		t.Fatalf("expected DetailedError, got %T", err)
	}
	if de.ExitCode == nil || *de.ExitCode != gcxerrors.ExitUsageError {
		t.Errorf("expected exit code %d, got %v", gcxerrors.ExitUsageError, de.ExitCode)
	}
}

func TestSchemasGetValidate_RequiresType(t *testing.T) {
	opts := &schemasGetOpts{Kind: "config"}
	if err := opts.Validate(); err == nil {
		t.Fatal("expected error when --type is missing")
	}
}

func TestRedactSecrets(t *testing.T) {
	m := &dsclient.DataSourceManifest{
		Secure: map[string]dsclient.SecureValue{
			"authToken": {Create: "super-secret"},
			"gone":      {Remove: true},
		},
	}
	redactSecrets(m)
	if got := m.Secure["authToken"].Create; got != "" {
		t.Errorf("secret value must be redacted, got %q", got)
	}
	if m.Secure["authToken"].Name != "<redacted>" {
		t.Errorf("expected redaction marker, got %q", m.Secure["authToken"].Name)
	}
	if !m.Secure["gone"].Remove {
		t.Error("remove entries should be preserved")
	}
}
