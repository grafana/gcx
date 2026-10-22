package fleet_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/fleet"
)

func TestReadErrorBody_LimitsDiagnosticBody(t *testing.T) {
	resp := &http.Response{Body: io.NopCloser(strings.NewReader(strings.Repeat("x", (1<<20)+100)))}

	body := fleet.ReadErrorBody(resp)

	if len(body) != 1<<20 {
		t.Fatalf("body length = %d, want %d", len(body), 1<<20)
	}
}

func TestIsResourceNotFoundBody(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{"Connect pipeline not found", `{"code":"not_found","message":"pipeline not found"}`, true},
		{"Connect code only", `{"code":"not_found"}`, true},
		{"JSON whitespace", " \n {\"code\":\"not_found\"} \n", true},
		{"plugin route", `{"message":"plugin route match not found"}`, false},
		{"plugin absent", `{"message":"Plugin not found"}`, false},
		{"plugin disabled", `{"message":"plugin is not enabled"}`, false},
		{"RPC route absent", "404 page not found", false},
		{"message lacks code", `{"message":"pipeline not found"}`, false},
		{"wrong Connect code", `{"code":"unimplemented","message":"not found"}`, false},
		{"case-sensitive Connect value", `{"code":"NOT_FOUND"}`, false},
		{"nested code", `{"error":{"code":"not_found"}}`, false},
		{"malformed JSON", `{"code":"not_found"`, false},
		{"trailing text", `{"code":"not_found"} other response`, false},
		{"non-string code", `{"code":404}`, false},
		{"JSON null", "null", false},
		{"empty body", "", false},
		{"HTML", "<html><body>404 not_found</body></html>", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fleet.IsResourceNotFoundBody(tt.body); got != tt.want {
				t.Fatalf("IsResourceNotFoundBody(%q) = %v, want %v", tt.body, got, tt.want)
			}
		})
	}
}
