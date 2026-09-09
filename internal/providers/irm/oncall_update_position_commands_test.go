//nolint:testpackage // white-box tests require access to unexported IRM command builders
package irm

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// fakeMoveAPI stubs the position surface of the ordered OnCall resources.
// Unimplemented OnCallAPI methods panic via the embedded nil interface.
//
// Both methods take the same arguments, so a separate flag per method is the
// only way the tests detect a cross-wire between the two nouns.
type fakeMoveAPI struct {
	OnCallAPI

	gotID       string
	gotPosition int
	called      bool
	movedPolicy bool
	movedRoute  bool
	err         error
}

func (f *fakeMoveAPI) MoveEscalationPolicy(_ context.Context, id string, position int) error {
	f.called = true
	f.movedPolicy = true
	f.gotID = id
	f.gotPosition = position
	return f.err
}

func (f *fakeMoveAPI) MoveRoute(_ context.Context, id string, position int) error {
	f.called = true
	f.movedRoute = true
	f.gotID = id
	f.gotPosition = position
	return f.err
}

func runUpdatePositionCmd(t *testing.T, noun string, fake *fakeMoveAPI, args ...string) (string, error) {
	t.Helper()
	var cmd = newRoutesCmd(&fakeLoader{client: fake})
	if noun == "escalation-policies" {
		cmd = newEscalationPoliciesCmd(&fakeLoader{client: fake})
	}
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(context.Background())
	return out.String(), err
}

func TestUpdatePositionCommand(t *testing.T) {
	tests := []struct {
		name       string
		noun       string
		args       []string
		wantID     string
		wantPos    int
		wantPolicy bool
		wantRoute  bool
		wantLine   string
	}{
		{
			name:       "escalation policy to the first step",
			noun:       "escalation-policies",
			args:       []string{"update-position", "EP1", "--position", "0"},
			wantID:     "EP1",
			wantPos:    0,
			wantPolicy: true,
			wantLine:   "Updated the position of escalation policy EP1 to 0",
		},
		{
			name:      "route to the third position",
			noun:      "routes",
			args:      []string{"update-position", "R7", "--position", "2"},
			wantID:    "R7",
			wantPos:   2,
			wantRoute: true,
			wantLine:  "Updated the position of route R7 to 2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetAgentMode(t)

			fake := &fakeMoveAPI{}
			out, err := runUpdatePositionCmd(t, tt.noun, fake, tt.args...)
			if err != nil {
				t.Fatal(err)
			}
			if fake.gotID != tt.wantID || fake.gotPosition != tt.wantPos {
				t.Errorf("got position update of %q to %d, want %q to %d", fake.gotID, fake.gotPosition, tt.wantID, tt.wantPos)
			}
			// Each noun must reach its own API method.
			if fake.movedPolicy != tt.wantPolicy {
				t.Errorf("MoveEscalationPolicy ran: %t, want %t", fake.movedPolicy, tt.wantPolicy)
			}
			if fake.movedRoute != tt.wantRoute {
				t.Errorf("MoveRoute ran: %t, want %t", fake.movedRoute, tt.wantRoute)
			}
			if !strings.Contains(out, tt.wantLine) {
				t.Errorf("expected %q in the output, got %q", tt.wantLine, out)
			}
		})
	}
}

// Cobra enforces the flag, so an omitted --position fails before Validate
// runs. Validate only rejects a negative value. An explicit --position 0 is a
// valid request for the first slot, so neither check refuses it.
func TestUpdatePositionCommandRejectsInvalidPosition(t *testing.T) {
	tests := []struct {
		name    string
		noun    string
		args    []string
		wantErr string
	}{
		{
			name:    "escalation policy without --position",
			noun:    "escalation-policies",
			args:    []string{"update-position", "EP1"},
			wantErr: `required flag(s) "position" not set`,
		},
		{
			name:    "route with a negative --position",
			noun:    "routes",
			args:    []string{"update-position", "R1", "--position", "-1"},
			wantErr: "--position must be zero or greater",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetAgentMode(t)

			fake := &fakeMoveAPI{}
			_, err := runUpdatePositionCmd(t, tt.noun, fake, tt.args...)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("expected an error that contains %q, got %v", tt.wantErr, err)
			}
			if fake.called {
				t.Error("the command called the backend despite an invalid position")
			}
		})
	}
}

// MarkFlagRequired states the requirement only in the runtime error. Cobra
// adds nothing to --help or to the generated reference page, so the flag help
// must carry the marker itself.
func TestUpdatePositionCommandFlagHelp(t *testing.T) {
	resetAgentMode(t)

	out, err := runUpdatePositionCmd(t, "escalation-policies", &fakeMoveAPI{}, "update-position", "--help")
	if err != nil {
		t.Fatal(err)
	}

	var line string
	for l := range strings.SplitSeq(out, "\n") {
		if strings.Contains(l, "--position") {
			line = l
			break
		}
	}
	if line == "" {
		t.Fatalf("the help misses the --position flag: %q", out)
	}
	if !strings.Contains(line, "(required)") {
		t.Errorf("the flag help misses the required marker: %q", line)
	}
}

func TestUpdatePositionCommandStructuredResult(t *testing.T) {
	resetAgentMode(t)

	fake := &fakeMoveAPI{}
	out, err := runUpdatePositionCmd(t, "escalation-policies", fake, "update-position", "EP1", "--position", "2", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}

	var got struct {
		Type          string `json:"type"`
		SchemaVersion string `json:"schema_version"`
		Action        string `json:"action"`
		Position      int    `json:"position"`
		Target        struct {
			Kind string `json:"kind"`
			ID   string `json:"id"`
		} `json:"target"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if got.Type != "gcx.mutation" || got.SchemaVersion != "1" {
		t.Errorf("missing mutation discriminators: %+v", got)
	}
	if got.Action != "updated-position" {
		t.Errorf("unexpected mutation document: %+v", got)
	}
	// The position is the whole point of the verb, so a caller must be able to
	// read it back from the structured result.
	if got.Position != 2 {
		t.Errorf("got position %d, want 2", got.Position)
	}
	if got.Target.Kind != "EscalationPolicy" || got.Target.ID != "EP1" {
		t.Errorf("unexpected target: %+v", got.Target)
	}

	// The embedded SingleMutation carries no struct tag, so encoding/json
	// flattens it. The document must stay flat, and it must omit changed,
	// because the command cannot tell a real change from a no-op.
	var raw map[string]any
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatalf("output is not a JSON object: %v\n%s", err, out)
	}
	wantKeys := []string{"type", "schema_version", "action", "target", "position"}
	if len(raw) != len(wantKeys) {
		t.Errorf("got keys %v, want exactly %v", raw, wantKeys)
	}
	for _, k := range wantKeys {
		if _, ok := raw[k]; !ok {
			t.Errorf("the document misses the %q key: %v", k, raw)
		}
	}
	if _, ok := raw["changed"]; ok {
		t.Errorf("the document reports changed although the command cannot tell: %v", raw)
	}
}
