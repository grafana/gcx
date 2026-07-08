//nolint:testpackage // white-box tests require access to unexported IRM types and helpers
package irm

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// fakeComplianceAPI stubs the compliance-rules methods of OnCallAPI. It embeds
// OnCallAPI so unrelated methods panic if called.
type fakeComplianceAPI struct {
	OnCallAPI

	got    ComplianceRules
	stored ComplianceRules
}

func (f *fakeComplianceAPI) GetComplianceRules(_ context.Context) (*ComplianceRules, error) {
	r := f.stored
	return &r, nil
}

func (f *fakeComplianceAPI) SetComplianceRules(_ context.Context, rules ComplianceRules) (*ComplianceRules, error) {
	f.got = rules
	return &rules, nil
}

func runComplianceCmd(t *testing.T, cmd *cobra.Command, args ...string) string {
	t.Helper()
	stdout := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stdout)
	cmd.SetArgs(args)
	cmd.SetContext(context.Background())
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute %v: %v\noutput=%s", args, err, stdout.String())
	}
	return stdout.String()
}

func TestComplianceRulesSet_BuildsBodyFromFlags(t *testing.T) {
	fake := &fakeComplianceAPI{}
	cmd := newComplianceRulesSetCmd(&fakeLoader{client: fake})

	out := runComplianceCmd(t, cmd,
		"--required-channels", "phone,slack",
		"--default", "notify_by_slack",
		"--important", "notify_by_sms,notify_by_slack",
		"-o", "json",
	)

	want := ComplianceRules{
		RequiredChannels: []string{"phone", "slack"},
		RequiredNotificationRules: ComplianceNotificationRules{
			Default:   []string{"notify_by_slack"},
			Important: []string{"notify_by_sms", "notify_by_slack"},
		},
	}
	if !reflectEqualRules(fake.got, want) {
		t.Errorf("posted rules = %+v, want %+v", fake.got, want)
	}

	var got ComplianceRules
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw=%s", err, out)
	}
	if !reflectEqualRules(got, want) {
		t.Errorf("json output = %+v, want %+v", got, want)
	}
}

func TestComplianceRulesSet_RequiresAtLeastOneFlag(t *testing.T) {
	cmd := newComplianceRulesSetCmd(&fakeLoader{client: &fakeComplianceAPI{}})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(nil)
	cmd.SetContext(context.Background())
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "at least one of") {
		t.Fatalf("expected validation error about required flags, got %v", err)
	}
}

func TestComplianceRulesGet_RendersText(t *testing.T) {
	resetAgentMode(t) // otherwise agent-mode detection overrides the text default with JSON
	fake := &fakeComplianceAPI{stored: ComplianceRules{
		RequiredChannels: []string{"phone"},
		RequiredNotificationRules: ComplianceNotificationRules{
			Important: []string{"notify_by_sms"},
		},
	}}
	cmd := newComplianceRulesGetCmd(&fakeLoader{client: fake})

	out := runComplianceCmd(t, cmd) // default -o text

	for _, want := range []string{"Required channels: Phone", "Default policy:    -", "Important policy:  SMS"} {
		if !strings.Contains(out, want) {
			t.Errorf("text output missing %q\ngot:\n%s", want, out)
		}
	}
}

func TestDoctorResultFor(t *testing.T) {
	eval := &ComplianceEvaluation{
		Compliant: []string{"U_OK"},
		NonCompliant: []UserComplianceViolation{
			{UserID: "U_BAD", Violations: []string{"phone channel not configured", "missing notify_by_slack in default policy"}},
		},
	}
	tests := []struct {
		name          string
		userID        string
		wantCompliant bool
		wantFound     bool
		wantViols     int
	}{
		{"compliant", "U_OK", true, true, 0},
		{"non-compliant", "U_BAD", false, true, 2},
		{"absent", "U_MISSING", false, false, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := doctorResultFor(tc.userID, eval)
			if got.Compliant != tc.wantCompliant || got.Found != tc.wantFound || len(got.Violations) != tc.wantViols {
				t.Errorf("doctorResultFor(%q) = %+v", tc.userID, got)
			}
		})
	}
}

func TestFriendlyViolation(t *testing.T) {
	got := friendlyViolation("missing notify_by_slack in default policy")
	if want := "missing Slack in default policy"; got != want {
		t.Errorf("friendlyViolation = %q, want %q", got, want)
	}
}

func reflectEqualRules(a, b ComplianceRules) bool {
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	return string(ja) == string(jb)
}
