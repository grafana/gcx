package irm_test

import (
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/providers/irm"
	"github.com/spf13/cobra"
)

// TestOnCallNounConformance asserts the structural shape of every OnCall noun
// command. New mutable resources gain create/update/delete; read-only ones do
// not. Catches regressions where a verb is silently dropped or where a new
// noun forgets to follow the project's CRUD pattern.
func TestOnCallNounConformance(t *testing.T) {
	t.Parallel()

	mutable := map[string]bool{
		"integrations":        true,
		"escalation-chains":   true,
		"escalation-policies": true,
		"schedules":           true,
		"shifts":              true,
		"routes":              true,
		"webhooks":            true,
		"resolution-notes":    true,
		"shift-swaps":         true,
	}
	readOnly := map[string]bool{
		"users":          true,
		"teams":          true,
		"user-groups":    true,
		"slack-channels": true,
		"alerts":         true,
		"organizations":  true,
	}

	for _, cmd := range irm.NewTestOnCallNounCmds() {
		noun := cmd.Use
		t.Run(noun, func(t *testing.T) {
			t.Parallel()
			subs := childMap(cmd)

			switch {
			case mutable[noun]:
				assertHasVerb(t, subs, "create", noun)
				assertCreateShape(t, subs["create"], noun)
				assertHasVerb(t, subs, "update", noun)
				assertUpdateShape(t, subs["update"], noun)
				assertHasVerb(t, subs, "delete", noun)
				assertDeleteShape(t, subs["delete"], noun)
			case readOnly[noun]:
				for _, verb := range []string{"create", "update", "delete"} {
					if _, ok := subs[verb]; ok {
						t.Errorf("read-only noun %q must not expose %q", noun, verb)
					}
				}
			}
		})
	}
}

// TestOnCallDiscoverySubcommands asserts each magic-value catalog has a `list`
// verb under its parent noun.
func TestOnCallDiscoverySubcommands(t *testing.T) {
	t.Parallel()

	parents := map[string]string{
		"escalation-policies": "steps",
		"routes":              "filter-types",
		"webhooks":            "triggers", // also "presets" — checked below
	}

	all := map[string]*cobra.Command{}
	for _, c := range irm.NewTestOnCallNounCmds() {
		all[c.Use] = c
	}

	for parent, subnoun := range parents {
		pc, ok := all[parent]
		if !ok {
			t.Fatalf("parent noun %q not found", parent)
		}
		sub := findChild(pc, subnoun)
		if sub == nil {
			t.Errorf("expected %q to have child %q", parent, subnoun)
			continue
		}
		if findChild(sub, "list") == nil {
			t.Errorf("expected %q %q to have a `list` subcommand", parent, subnoun)
		}
	}

	// webhooks has both triggers and presets
	if wc := all["webhooks"]; wc != nil {
		if presets := findChild(wc, "presets"); presets == nil {
			t.Error("expected webhooks to have child `presets`")
		} else if findChild(presets, "list") == nil {
			t.Error("expected webhooks presets to have a `list` subcommand")
		}
	}
}

// --- helpers ---

func childMap(c *cobra.Command) map[string]*cobra.Command {
	out := make(map[string]*cobra.Command, len(c.Commands()))
	for _, sub := range c.Commands() {
		out[verbOf(sub.Use)] = sub
	}
	return out
}

func verbOf(use string) string {
	// Use is e.g. "create" or "update <id>". Take the first whitespace-delimited token.
	return strings.SplitN(use, " ", 2)[0]
}

func findChild(c *cobra.Command, name string) *cobra.Command {
	for _, sub := range c.Commands() {
		if verbOf(sub.Use) == name {
			return sub
		}
	}
	return nil
}

func assertHasVerb(t *testing.T, subs map[string]*cobra.Command, verb, noun string) {
	t.Helper()
	if _, ok := subs[verb]; !ok {
		t.Errorf("mutable noun %q is missing %q subcommand", noun, verb)
	}
}

func assertCreateShape(t *testing.T, c *cobra.Command, noun string) {
	t.Helper()
	if c == nil {
		return
	}
	if c.Flags().Lookup("filename") == nil {
		t.Errorf("%s create is missing --filename flag", noun)
	}
}

func assertUpdateShape(t *testing.T, c *cobra.Command, noun string) {
	t.Helper()
	if c == nil {
		return
	}
	if !strings.Contains(c.Use, "<id>") {
		t.Errorf("%s update should take a positional <id>, got Use=%q", noun, c.Use)
	}
	if c.Flags().Lookup("filename") == nil {
		t.Errorf("%s update is missing --filename flag", noun)
	}
}

func assertDeleteShape(t *testing.T, c *cobra.Command, noun string) {
	t.Helper()
	if c == nil {
		return
	}
	if !strings.Contains(c.Use, "<id>") {
		t.Errorf("%s delete should take a positional <id>, got Use=%q", noun, c.Use)
	}
	if c.Flags().Lookup("force") == nil {
		t.Errorf("%s delete is missing --force flag", noun)
	}
}
