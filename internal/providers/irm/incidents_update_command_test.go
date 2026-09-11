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
	"k8s.io/client-go/rest"
)

func runIncidentUpdateCmdWithMode(t *testing.T, srv *severityServer, agentMode string, args ...string) (string, error) {
	t.Helper()
	t.Setenv("GCX_AGENT_MODE", agentMode)
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

func runIncidentUpdateCmd(t *testing.T, srv *severityServer, args ...string) (string, error) {
	t.Helper()
	return runIncidentUpdateCmdWithMode(t, srv, "false", args...)
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
				"SeveritiesService.GetOrgSeverities",
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
				"SeveritiesService.GetOrgSeverities",
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
				if strings.HasPrefix(srv.calls[i], "IncidentsService.") && srv.bodies[i]["incidentID"] != "4" {
					t.Errorf("call %d sent incidentID %v, want 4", i, srv.bodies[i]["incidentID"])
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

// TestIncidentUpdateCommandOutputFormats covers a run that changes nothing
// and the structured mutation result in the explicit machine formats.
func TestIncidentUpdateCommandOutputFormats(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    []string
		notWant string
	}{
		{
			name:    "a value that already matches changes nothing",
			args:    []string{"4", "--title", "old title"},
			want:    []string{"Incident 4 already carries the requested values\n"},
			notWant: "Updated incident",
		},
		{
			name: "-o yaml emits a changed mutation result",
			args: []string{"4", "--severity", "Critical", "-o", "yaml"},
			want: []string{"type: gcx.mutation", "action: updated", "id: \"4\"", "changed: true"},
		},
		{
			name: "-o json emits an unchanged mutation result",
			args: []string{"4", "--title", "old title", "-o", "json"},
			want: []string{`"type": "gcx.mutation"`, `"action": "updated"`, `"id": "4"`, `"changed": false`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := &severityServer{title: "old title"}
			out, err := runIncidentUpdateCmd(t, srv, tt.args...)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range tt.want {
				if !strings.Contains(out, want) {
					t.Errorf("expected %q in the output, got %q", want, out)
				}
			}
			if tt.notWant != "" && strings.Contains(out, tt.notWant) {
				t.Errorf("did not expect %q in the output, got %q", tt.notWant, out)
			}
		})
	}
}

func TestIncidentUpdateCommandAgentOutputReportsNoChange(t *testing.T) {
	srv := &severityServer{title: "old title"}
	out, err := runIncidentUpdateCmdWithMode(t, srv, "true", "4", "--title", "old title")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"type":"gcx.mutation"`, `"id":"4"`, `"changed":false`} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in the agent output, got %q", want, out)
		}
	}
}
