package irm_test

import (
	"bytes"
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/irm"
	"github.com/spf13/cobra"
	"k8s.io/client-go/rest"
)

func runIncidentUpdateCmd(t *testing.T, srv *severityServer, args ...string) (string, error) {
	t.Helper()
	t.Setenv("GCX_AGENT_MODE", "false")
	agent.ResetForTesting()
	t.Cleanup(agent.ResetForTesting)

	server := httptest.NewServer(srv.handler(t))
	t.Cleanup(server.Close)

	loader := fakeGrafanaConfigLoader{cfg: config.NamespacedRESTConfig{
		Config:    rest.Config{Host: server.URL},
		Namespace: "stack-123",
	}}

	cmd := irm.NewUpdateCommand(loader)
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(context.Background())
	return out.String(), err
}

func TestIncidentUpdateCommand(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantCalls []string
		wantOut   []string
	}{
		// The command reaches the backend through IncidentClient.Update, so
		// every case starts with the read that method does.
		{
			name: "severity only",
			args: []string{"4", "--severity", "Critical"},
			wantCalls: []string{
				"IncidentsService.GetIncident",
				"IncidentsService.UpdateSeverity",
			},
			wantOut: []string{"Updated incident 4 (severity)"},
		},
		{
			name: "title only",
			args: []string{"4", "--title", "Checkout latency above the objective"},
			wantCalls: []string{
				"IncidentsService.GetIncident",
				"IncidentsService.UpdateTitle",
			},
			wantOut: []string{"Updated incident 4 (title)"},
		},
		{
			name: "both fields",
			args: []string{"4", "--title", "new title", "--severity", "Major"},
			// The title runs before the severity.
			wantCalls: []string{
				"IncidentsService.GetIncident",
				"IncidentsService.UpdateTitle",
				"IncidentsService.UpdateSeverity",
			},
			wantOut: []string{"Updated incident 4 (title, severity)"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := &severityServer{title: "old title"}
			out, err := runIncidentUpdateCmd(t, srv, tt.args...)
			if err != nil {
				t.Fatal(err)
			}

			if len(srv.calls) != len(tt.wantCalls) {
				t.Fatalf("got calls %v, want %v", srv.calls, tt.wantCalls)
			}
			for i, want := range tt.wantCalls {
				if srv.calls[i] != want {
					t.Errorf("call %d: got %q, want %q", i, srv.calls[i], want)
				}
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out, want) {
					t.Errorf("expected %q in the output, got %q", want, out)
				}
			}
		})
	}
}

// TestIncidentUpdateCommandRejectsBadFlags covers the omitted flag and the
// explicit empty value. An unset shell variable produces the second one, and
// silence there loses the request of the caller.
func TestIncidentUpdateCommandRejectsBadFlags(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "no flag at all",
			args:    []string{"4"},
			wantErr: "at least one of --severity or --title",
		},
		{
			name:    "an empty title next to a severity",
			args:    []string{"4", "--severity", "Critical", "--title", ""},
			wantErr: "--title must not be empty",
		},
		{
			name:    "an empty severity",
			args:    []string{"4", "--severity", ""},
			wantErr: "--severity must not be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := &severityServer{title: "old title"}
			_, err := runIncidentUpdateCmd(t, srv, tt.args...)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("expected %q, got %v", tt.wantErr, err)
			}
			if len(srv.calls) != 0 {
				t.Errorf("the command called the backend on a bad flag: %v", srv.calls)
			}
		})
	}
}

// TestIncidentUpdateCommandOutputFormats covers the two output cases that
// TestIncidentUpdateCommand does not reach: a run that changes nothing, and
// the manifest that -o yaml emits.
func TestIncidentUpdateCommandOutputFormats(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    string
		notWant string
	}{
		{
			name:    "a value that already matches changes nothing",
			args:    []string{"4", "--title", "old title"},
			want:    "Incident 4 already carries the requested values\n",
			notWant: "Updated incident",
		},
		{
			name: "-o yaml emits the manifest",
			args: []string{"4", "--severity", "Critical", "-o", "yaml"},
			want: "apiVersion: incident.ext.grafana.app/v1alpha1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := &severityServer{title: "old title"}
			out, err := runIncidentUpdateCmd(t, srv, tt.args...)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out, tt.want) {
				t.Errorf("expected %q in the output, got %q", tt.want, out)
			}
			if tt.notWant != "" && strings.Contains(out, tt.notWant) {
				t.Errorf("did not expect %q in the output, got %q", tt.notWant, out)
			}
		})
	}
}

func TestIncidentUpdateCommandShape(t *testing.T) {
	cmd := irm.NewUpdateCommand(fakeGrafanaConfigLoader{})
	assertFlag(t, cmd, "severity")
	assertFlag(t, cmd, "title")
	if !strings.Contains(cmd.Use, "<id>") {
		t.Errorf("update should take a positional <id>, got Use=%q", cmd.Use)
	}
}

func assertFlag(t *testing.T, cmd *cobra.Command, name string) {
	t.Helper()
	if cmd.Flags().Lookup(name) == nil {
		t.Errorf("incidents update is missing the --%s flag", name)
	}
}
