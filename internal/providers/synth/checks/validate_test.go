package checks_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/providers/synth/checks"
)

func TestValidateCheckSpec(t *testing.T) {
	probeMap := map[string]int64{"Oregon": 1, "Paris": 2}

	tests := []struct {
		name        string
		spec        checks.CheckSpec
		wantErrStrs []string // substrings that must appear in errors
		wantNoErrs  bool
	}{
		{
			name: "valid http check",
			spec: checks.CheckSpec{
				Target:   "https://example.com",
				Settings: checks.CheckSettings{"http": map[string]any{}},
				Probes:   []string{"Oregon"},
			},
			wantNoErrs: true,
		},
		{
			name: "missing target",
			spec: checks.CheckSpec{
				Settings: checks.CheckSettings{"http": map[string]any{}},
				Probes:   []string{"Oregon"},
			},
			wantErrStrs: []string{"target is required"},
		},
		{
			name: "unknown check type",
			spec: checks.CheckSpec{
				Target:   "example.com",
				Settings: checks.CheckSettings{},
				Probes:   []string{"Oregon"},
			},
			wantErrStrs: []string{"unknown check type"},
		},
		{
			name: "probe not found",
			spec: checks.CheckSpec{
				Target:   "example.com",
				Settings: checks.CheckSettings{"ping": map[string]any{}},
				Probes:   []string{"Oregon", "Tokyo"},
			},
			wantErrStrs: []string{`probe "Tokyo" not found`},
		},
		{
			name: "dns check with URL target",
			spec: checks.CheckSpec{
				Target:   "https://example.com",
				Settings: checks.CheckSettings{"dns": map[string]any{}},
				Probes:   []string{"Oregon"},
			},
			wantErrStrs: []string{"dns check target should be a hostname"},
		},
		{
			name: "dns check with correct hostname target",
			spec: checks.CheckSpec{
				Target:   "example.com",
				Settings: checks.CheckSettings{"dns": map[string]any{}},
				Probes:   []string{"Oregon"},
			},
			wantNoErrs: true,
		},
		{
			name: "multiple errors at once",
			spec: checks.CheckSpec{
				Settings: checks.CheckSettings{},
				Probes:   []string{"Tokyo"},
			},
			wantErrStrs: []string{"target is required", "unknown check type", `probe "Tokyo" not found`},
		},
		{
			name: "empty probe list is valid",
			spec: checks.CheckSpec{
				Target:   "example.com",
				Settings: checks.CheckSettings{"ping": map[string]any{}},
				Probes:   []string{},
			},
			wantNoErrs: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := checks.ValidateCheckSpec(&tt.spec, probeMap)
			if tt.wantNoErrs {
				if len(errs) > 0 {
					t.Errorf("expected no errors, got: %v", errs)
				}
				return
			}
			for _, want := range tt.wantErrStrs {
				found := false
				for _, got := range errs {
					if containsStr(got, want) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected error containing %q, got: %v", want, errs)
				}
			}
		})
	}
}

func TestAllProbesOffline(t *testing.T) {
	tests := []struct {
		name       string
		probes     []string
		onlineMap  map[string]bool
		wantResult bool
	}{
		{
			name:       "all online",
			probes:     []string{"Oregon", "Paris"},
			onlineMap:  map[string]bool{"Oregon": true, "Paris": true},
			wantResult: false,
		},
		{
			name:       "all offline",
			probes:     []string{"Oregon", "Paris"},
			onlineMap:  map[string]bool{"Oregon": false, "Paris": false},
			wantResult: true,
		},
		{
			name:       "mixed online/offline",
			probes:     []string{"Oregon", "Paris"},
			onlineMap:  map[string]bool{"Oregon": false, "Paris": true},
			wantResult: false,
		},
		{
			name:       "empty probe list",
			probes:     []string{},
			onlineMap:  map[string]bool{"Oregon": false},
			wantResult: false,
		},
		{
			name:       "probe not in online map treated as offline",
			probes:     []string{"Unknown"},
			onlineMap:  map[string]bool{"Oregon": true},
			wantResult: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := checks.AllProbesOffline(tt.probes, tt.onlineMap)
			if got != tt.wantResult {
				t.Errorf("AllProbesOffline() = %v, want %v", got, tt.wantResult)
			}
		})
	}
}

func TestValidateHTTPTarget(t *testing.T) {
	tests := []struct {
		name       string
		checkType  string
		statusCode int
		wantErr    bool
	}{
		{
			name:       "non-http check type skipped",
			checkType:  "dns",
			statusCode: 0, // server not started
			wantErr:    false,
		},
		{
			name:       "http 200 OK",
			checkType:  "http",
			statusCode: http.StatusOK,
			wantErr:    false,
		},
		{
			name:       "http 404 not found is acceptable",
			checkType:  "http",
			statusCode: http.StatusNotFound,
			wantErr:    false,
		},
		{
			name:       "http 500 server error is reported",
			checkType:  "http",
			statusCode: http.StatusInternalServerError,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := "http://localhost:0" // unreachable default
			if tt.checkType == "http" {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(tt.statusCode)
				}))
				defer srv.Close()
				target = srv.URL
			}

			err := checks.ValidateHTTPTarget(tt.checkType, target, 5*time.Second)
			if tt.wantErr && err == nil {
				t.Error("expected an error but got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// containsStr returns true if s contains substr.
func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
			return false
		}())
}
