package irm

import (
	"fmt"
	"strings"
	"time"

	fail "github.com/grafana/gcx/internal/gcxerrors"
	"github.com/spf13/pflag"
)

func opModelUsage(message string) error {
	code := fail.ExitUsageError
	return &fail.DetailedError{Summary: message, ExitCode: &code, Suggestions: []string{"Check the flag values against the command help (--help)"}}
}

type observationContextOpts struct {
	ObservedStart  string
	ObservedEnd    string
	Scope          string
	AuthorName     string
	AuthorSourceID string
	flags          *pflag.FlagSet
	start          *time.Time
	end            *time.Time
}

func (o *observationContextOpts) bind(flags *pflag.FlagSet) {
	o.flags = flags
	flags.StringVar(&o.ObservedStart, "observed-start", "", "Observed interval start in RFC3339 (optional; omission means unknown)")
	flags.StringVar(&o.ObservedEnd, "observed-end", "", "Observed interval end in RFC3339 (requires start; must not precede it)")
	flags.StringVar(&o.Scope, "scope", "", "Observation scope (optional; empty is an explicitly empty scope)")
	flags.StringVar(&o.AuthorName, "original-author-name", "", "Original author's display name, not the recorder (requires a name or source ID when supplied)")
	flags.StringVar(&o.AuthorSourceID, "original-author-source-id", "", "Original author's source-specific ID (a name is not required)")
}

func (o *observationContextOpts) validate() error {
	for _, field := range []struct {
		name, value string
		target      **time.Time
	}{
		{"observed-start", o.ObservedStart, &o.start}, {"observed-end", o.ObservedEnd, &o.end},
	} {
		if o.flags.Changed(field.name) {
			parsed, err := time.Parse(time.RFC3339Nano, field.value)
			if err != nil {
				return opModelUsage(fmt.Sprintf("--%s must be RFC3339, got %q", field.name, field.value))
			}
			*field.target = &parsed
		}
	}
	if o.end != nil && (o.start == nil || o.end.Before(*o.start)) {
		return opModelUsage("--observed-end requires --observed-start and must not precede it")
	}
	if o.flags.Changed("original-author-name") || o.flags.Changed("original-author-source-id") {
		if strings.TrimSpace(o.AuthorName) == "" && strings.TrimSpace(o.AuthorSourceID) == "" {
			return opModelUsage("original author requires a nonblank name or source ID")
		}
	}
	return nil
}

func (o *observationContextOpts) content(text, sourceRef string) observationContent {
	c := observationContent{Text: text, SourceRef: sourceRef, ObservedStart: o.start, ObservedEnd: o.end}
	if o.flags.Changed("scope") {
		c.Scope = &o.Scope
	}
	if o.AuthorName != "" || o.AuthorSourceID != "" {
		c.OriginalAuthor = &originalAuthor{DisplayName: o.AuthorName, SourceID: o.AuthorSourceID}
	}
	return c
}

type actionContentOpts struct {
	ExpectedResult string
	flags          *pflag.FlagSet
}

func (o *actionContentOpts) bind(flags *pflag.FlagSet) {
	o.flags = flags
	flags.StringVar(&o.ExpectedResult, "expected-result", "", "Expected result (optional; empty records empty text, omission clears it on full content edit)")
}
func (o *actionContentOpts) content(text string) actionContent {
	c := actionContent{Text: text}
	if o.flags.Changed("expected-result") {
		c.ExpectedResult = &o.ExpectedResult
	}
	return c
}

type assigneeOpts struct {
	Kind  string
	ID    string
	Name  string
	Clear bool
	flags *pflag.FlagSet
}

func (o *assigneeOpts) bind(flags *pflag.FlagSet, allowClear bool) {
	o.flags = flags
	flags.StringVar(&o.Kind, "assignee-kind", "", "Assignee kind: human or agent (requires --assignee-id)")
	flags.StringVar(&o.ID, "assignee-id", "", "Assignee stable ID (requires --assignee-kind)")
	flags.StringVar(&o.Name, "assignee-name", "", "Optional assignee display name; empty records no name")
	if allowClear {
		flags.BoolVar(&o.Clear, "clear-assignee", false, "Clear the assignee; cannot be combined with assignee fields")
	}
}
func (o *assigneeOpts) supplied() bool {
	return o.flags.Changed("assignee-kind") || o.flags.Changed("assignee-id") || o.flags.Changed("assignee-name")
}
func (o *assigneeOpts) validate() error {
	if !o.supplied() {
		return nil
	}
	if o.Clear {
		return opModelUsage("--clear-assignee cannot be combined with assignee fields")
	}
	if err := validateEnumFlag("assignee-kind", o.Kind, actorKindVocab); err != nil {
		return err
	}
	return requireOpModelFlag("assignee-id", o.ID)
}
func (o *assigneeOpts) actor() *opModelActorInput {
	if !o.supplied() {
		return nil
	}
	return &opModelActorInput{Kind: o.Kind, ID: o.ID, DisplayName: o.Name}
}
