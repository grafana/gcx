package irm_test

import (
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/providers/irm"
	"github.com/spf13/cobra"
)

// TestOnCallNounConformance asserts the structural shape of every OnCall noun
// command: mutable resources expose create/update/delete with the standard
// flag surface, read-only ones do not. Catches regressions where a verb is
// silently dropped or a new noun forgets the CRUD pattern.
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
		"organizations":  true,
	}

	seen := map[string]bool{}
	for _, cmd := range irm.NewTestOnCallNounCmds() {
		noun := verbOf(cmd.Use)
		seen[noun] = true
		t.Run(noun, func(t *testing.T) {
			t.Parallel()
			subs := childMap(cmd)

			switch {
			case mutable[noun]:
				assertCreateShape(t, subs["create"], noun)
				assertUpdateShape(t, subs["update"], noun)
				assertDeleteShape(t, subs["delete"], noun)
			case readOnly[noun]:
				for _, verb := range []string{"create", "update", "delete"} {
					if _, ok := subs[verb]; ok {
						t.Errorf("read-only noun %q must not expose %q", noun, verb)
					}
				}
			default:
				t.Errorf("noun %q is in neither the mutable nor the read-only set; classify it", noun)
			}
		})
	}

	for noun := range mutable {
		if !seen[noun] {
			t.Errorf("mutable noun %q missing from NewTestOnCallNounCmds", noun)
		}
	}
	for noun := range readOnly {
		if !seen[noun] {
			t.Errorf("read-only noun %q missing from NewTestOnCallNounCmds", noun)
		}
	}
}

// TestOnCallDiscoverySubcommands asserts each enum catalog has a `list` verb
// under its parent noun.
func TestOnCallDiscoverySubcommands(t *testing.T) {
	t.Parallel()

	discovery := map[string][]string{
		"escalation-policies": {"steps"},
		"routes":              {"filter-types"},
		"webhooks":            {"triggers", "presets"},
	}

	all := map[string]*cobra.Command{}
	for _, c := range irm.NewTestOnCallNounCmds() {
		all[verbOf(c.Use)] = c
	}

	for parent, subnouns := range discovery {
		pc, ok := all[parent]
		if !ok {
			t.Fatalf("parent noun %q not found", parent)
		}
		for _, subnoun := range subnouns {
			sub := findChild(pc, subnoun)
			if sub == nil {
				t.Errorf("expected %q to have child %q", parent, subnoun)
				continue
			}
			if findChild(sub, "list") == nil {
				t.Errorf("expected %q %q to have a `list` subcommand", parent, subnoun)
			}
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

// verbOf returns the first whitespace-delimited token of a Use line
// (e.g. "update <id>" -> "update").
func verbOf(use string) string {
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

func assertCreateShape(t *testing.T, c *cobra.Command, noun string) {
	t.Helper()
	if c == nil {
		t.Errorf("mutable noun %q is missing create subcommand", noun)
		return
	}
	if c.Flags().Lookup("filename") == nil {
		t.Errorf("%s create is missing --filename flag", noun)
	}
}

func assertUpdateShape(t *testing.T, c *cobra.Command, noun string) {
	t.Helper()
	if c == nil {
		t.Errorf("mutable noun %q is missing update subcommand", noun)
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
		t.Errorf("mutable noun %q is missing delete subcommand", noun)
		return
	}
	if !strings.Contains(c.Use, "<id>") {
		t.Errorf("%s delete should take a positional <id>, got Use=%q", noun, c.Use)
	}
	force := c.Flags().Lookup("force")
	if force == nil {
		t.Errorf("%s delete is missing --force flag", noun)
	} else if force.Shorthand != "" {
		t.Errorf("%s delete --force must be long-only (safety.md), got shorthand -%s", noun, force.Shorthand)
	}
}
