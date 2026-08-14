//nolint:testpackage // white-box tests require access to unexported IRM command builders
package irm

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// fakeMoveAPI stubs the move surface of the ordered OnCall resources.
// Unimplemented OnCallAPI methods panic via the embedded nil interface.
type fakeMoveAPI struct {
	OnCallAPI

	gotID       string
	gotPosition int
	called      bool
	err         error
}

func (f *fakeMoveAPI) MoveEscalationPolicy(_ context.Context, id string, position int) error {
	f.called = true
	f.gotID = id
	f.gotPosition = position
	return f.err
}

func (f *fakeMoveAPI) MoveRoute(_ context.Context, id string, position int) error {
	f.called = true
	f.gotID = id
	f.gotPosition = position
	return f.err
}

func runMoveCmd(t *testing.T, noun string, fake *fakeMoveAPI, args ...string) (string, error) {
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

func TestMoveCommand(t *testing.T) {
	tests := []struct {
		name     string
		noun     string
		args     []string
		wantID   string
		wantPos  int
		wantLine string
	}{
		{
			name:     "escalation policy to the first step",
			noun:     "escalation-policies",
			args:     []string{"move", "EP1", "--position", "0"},
			wantID:   "EP1",
			wantPos:  0,
			wantLine: "Moved escalation policy EP1 to position 0",
		},
		{
			name:     "route to the third position",
			noun:     "routes",
			args:     []string{"move", "R7", "--position", "2"},
			wantID:   "R7",
			wantPos:  2,
			wantLine: "Moved route R7 to position 2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetAgentMode(t)

			fake := &fakeMoveAPI{}
			out, err := runMoveCmd(t, tt.noun, fake, tt.args...)
			if err != nil {
				t.Fatal(err)
			}
			if fake.gotID != tt.wantID || fake.gotPosition != tt.wantPos {
				t.Errorf("got move of %q to %d, want %q to %d", fake.gotID, fake.gotPosition, tt.wantID, tt.wantPos)
			}
			if !strings.Contains(out, tt.wantLine) {
				t.Errorf("expected %q in the output, got %q", tt.wantLine, out)
			}
		})
	}
}

func TestMoveCommandRequiresPosition(t *testing.T) {
	tests := []struct {
		name string
		noun string
		args []string
	}{
		{name: "escalation policy without --position", noun: "escalation-policies", args: []string{"move", "EP1"}},
		{name: "route with a negative --position", noun: "routes", args: []string{"move", "R1", "--position", "-1"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetAgentMode(t)

			fake := &fakeMoveAPI{}
			_, err := runMoveCmd(t, tt.noun, fake, tt.args...)
			if err == nil || !strings.Contains(err.Error(), "--position is required") {
				t.Errorf("expected a missing-position error, got %v", err)
			}
			if fake.called {
				t.Error("the command called the backend despite an invalid position")
			}
		})
	}
}

func TestMoveCommandStructuredResult(t *testing.T) {
	resetAgentMode(t)

	fake := &fakeMoveAPI{}
	out, err := runMoveCmd(t, "escalation-policies", fake, "move", "EP1", "--position", "2", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}

	var got struct {
		Type          string `json:"type"`
		SchemaVersion string `json:"schema_version"`
		Action        string `json:"action"`
		Changed       bool   `json:"changed"`
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
	if got.Action != "moved" || !got.Changed {
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
}
