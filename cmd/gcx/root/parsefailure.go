package root

// Parse-failure capture (#578): classify invocations cobra rejected before any
// hook ran (unknown command, unknown flag, argument validation) into an
// anonymous usage event. Privacy invariant: only command-shaped tokens and
// flag names are ever recorded, never argument or flag values.

import (
	"math"
	"net"
	"net/url"
	"regexp"
	"strings"
	"sync/atomic"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// ParseFailure kinds.
const (
	parseErrorUnknownCommand = "unknown_command"
	parseErrorUnknownFlag    = "unknown_flag"
	parseErrorInvalidArgs    = "invalid_args"
)

// redactedToken replaces tokens that fail the command-shape filter.
const redactedToken = "<redacted>"

// suggestionsMinimumDistance mirrors cobra's default SuggestionsMinimumDistance;
// cobra only assigns it lazily while rendering an unknown-command error.
const suggestionsMinimumDistance = 2

// ParseFailure describes a failed command-line parse for the usage event
// (#578). Token and Flags carry shape-filtered names only, never values.
type ParseFailure struct {
	Kind      string // unknown_command, unknown_flag or invalid_args
	Parent    string // deepest valid command path reached; "" is the root
	Token     string // first unknown token; redactedToken if it fails the shape filter
	Attempted string // Parent plus Token, truncated at the unknown token
	Flags     string // unknown flag names only, never values
	Nearest   string // nearest real command or flag; "" if no near match
	Distance  int    // Levenshtein distance to Nearest; -1 if no near match
}

// flagFailure records the command and error from cobra's flag-error hook so a
// failed parse can be classified as an unknown flag. pflag aborts at the first
// bad flag, so at most one failure is seen per invocation.
//
//nolint:gochecknoglobals // written once per process from the flag-error hook.
var flagFailure atomic.Pointer[flagFailureRecord]

type flagFailureRecord struct {
	cmd *cobra.Command
	err error
}

func recordFlagFailure(cmd *cobra.Command, err error) {
	if cmd == nil || err == nil {
		return
	}
	flagFailure.CompareAndSwap(nil, &flagFailureRecord{cmd: cmd, err: err})
}

// parseFailureTelemetryInfo classifies a failed parse into telemetry info. It
// runs only when PersistentPreRun never recorded an event and the exit code is
// nonzero, which means cobra rejected the invocation before any hook ran.
func parseFailureTelemetryInfo(rootCmd *cobra.Command, args []string) *TelemetryInfo {
	if ff := flagFailure.Load(); ff != nil {
		return flagFailureTelemetryInfo(ff)
	}

	parent, token := deepestCommand(rootCmd, args)
	if telemetrySuppressed(parent) {
		return &TelemetryInfo{Suppress: true}
	}

	parentPath := trimCommandRoot(parent)
	pf := &ParseFailure{Parent: parentPath, Attempted: parentPath, Distance: -1}
	if token != "" && !parent.Runnable() {
		// A positional on a pure group command can only be a command guess:
		// nothing here takes argument values.
		pf.Kind = parseErrorUnknownCommand
		pf.Nearest, pf.Distance = nearestCommand(parent, token)
		pf.Token = filterCommandToken(token)
		pf.Attempted = strings.TrimSpace(parentPath + " " + pf.Token)
	} else {
		// Runnable commands take argument values, which must never be
		// recorded, so any leftover token is dropped and the failure counts
		// as argument validation.
		pf.Kind = parseErrorInvalidArgs
	}

	return &TelemetryInfo{Command: parentPath, ParseError: pf}
}

func flagFailureTelemetryInfo(ff *flagFailureRecord) *TelemetryInfo {
	if telemetrySuppressed(ff.cmd) {
		return &TelemetryInfo{Suppress: true}
	}

	parentPath := trimCommandRoot(ff.cmd)
	pf := &ParseFailure{Parent: parentPath, Attempted: parentPath, Distance: -1}
	if name, ok := unknownFlagName(ff.err); ok {
		pf.Kind = parseErrorUnknownFlag
		pf.Nearest, pf.Distance = nearestFlag(ff.cmd, name)
		pf.Flags = filterCommandToken(name)
	} else {
		// Any other flag parse failure (missing or malformed value on a real
		// flag). The pflag error message embeds the value, so nothing from it
		// is recorded.
		pf.Kind = parseErrorInvalidArgs
	}
	return &TelemetryInfo{Command: parentPath, ParseError: pf}
}

// unknownFlagName extracts the flag name from pflag's unknown-flag errors:
// "unknown flag: --frmat" and "unknown shorthand flag: 'q' in -qv". pflag
// strips any "=value" before building the message. Every other flag error
// returns false: those messages can embed flag values.
func unknownFlagName(err error) (string, bool) {
	msg := err.Error()
	if name, ok := strings.CutPrefix(msg, "unknown flag: --"); ok {
		return name, true
	}
	if rest, ok := strings.CutPrefix(msg, "unknown shorthand flag: '"); ok {
		if i := strings.IndexByte(rest, '\''); i > 0 {
			return rest[:i], true
		}
	}
	return "", false
}

// deepestCommand resolves args against the command tree the way cobra's Find
// does and returns the deepest valid command plus the first token that did not
// match a subcommand ("" when every positional resolved). Flags are skipped
// with cobra's stripFlags rules so a flag value can never be mistaken for the
// unknown token; unrecognised flags are conservatively assumed to take a value.
func deepestCommand(rootCmd *cobra.Command, args []string) (*cobra.Command, string) {
	cmd := rootCmd
	inFlagValue := false
	for _, arg := range args {
		switch {
		case inFlagValue:
			inFlagValue = false
		case arg == "--":
			return cmd, ""
		case strings.HasPrefix(arg, "--") && !strings.Contains(arg, "="):
			inFlagValue = longFlagTakesValue(cmd, arg[2:])
		case strings.HasPrefix(arg, "-") && !strings.Contains(arg, "=") && len(arg) == 2:
			inFlagValue = shortFlagTakesValue(cmd, arg[1:])
		case arg == "" || strings.HasPrefix(arg, "-"):
			// A flag with an inline value, combined shorthands, or a bare
			// "-": consumes nothing and is never a command token.
		default:
			next := findSubcommand(cmd, arg)
			if next == nil {
				return cmd, arg
			}
			cmd = next
		}
	}
	return cmd, ""
}

func longFlagTakesValue(cmd *cobra.Command, name string) bool {
	for _, fs := range flagSets(cmd) {
		if f := fs.Lookup(name); f != nil {
			return f.NoOptDefVal == ""
		}
	}
	// Unknown flag: assume it takes a value. Dropping a command token is
	// safer than recording a value as the token.
	return true
}

func shortFlagTakesValue(cmd *cobra.Command, shorthand string) bool {
	for _, fs := range flagSets(cmd) {
		if f := fs.ShorthandLookup(shorthand); f != nil {
			return f.NoOptDefVal == ""
		}
	}
	return true
}

func flagSets(cmd *cobra.Command) []*pflag.FlagSet {
	return []*pflag.FlagSet{cmd.Flags(), cmd.PersistentFlags(), cmd.InheritedFlags()}
}

func findSubcommand(cmd *cobra.Command, name string) *cobra.Command {
	for _, sub := range cmd.Commands() {
		if sub.Name() == name || sub.HasAlias(name) {
			return sub
		}
	}
	return nil
}

// nearestCommand finds the closest real subcommand for the unknown token,
// reusing cobra's suggestion rule (Levenshtein within SuggestionsMinimumDistance,
// or prefix match) and ranking the result by distance.
func nearestCommand(parent *cobra.Command, token string) (string, int) {
	if parent.SuggestionsMinimumDistance <= 0 {
		parent.SuggestionsMinimumDistance = suggestionsMinimumDistance
	}
	return nearestString(token, parent.SuggestionsFor(token))
}

// nearestFlag finds the closest real flag name for the unknown flag, applying
// the same near-match rule cobra uses for commands.
func nearestFlag(cmd *cobra.Command, name string) (string, int) {
	var near []string
	seen := map[string]bool{}
	for _, fs := range flagSets(cmd) {
		fs.VisitAll(func(f *pflag.Flag) {
			if f.Hidden || seen[f.Name] {
				return
			}
			seen[f.Name] = true
			byDistance := levenshtein(strings.ToLower(name), strings.ToLower(f.Name)) <= suggestionsMinimumDistance
			byPrefix := strings.HasPrefix(strings.ToLower(f.Name), strings.ToLower(name))
			if byDistance || byPrefix {
				near = append(near, f.Name)
			}
		})
	}
	return nearestString(name, near)
}

// nearestString returns the candidate with the smallest case-insensitive
// Levenshtein distance to token, or ("", -1) when there are no candidates.
func nearestString(token string, candidates []string) (string, int) {
	best, bestDist := "", -1
	for _, c := range candidates {
		d := levenshtein(strings.ToLower(token), strings.ToLower(c))
		if bestDist == -1 || d < bestDist {
			best, bestDist = c, d
		}
	}
	return best, bestDist
}

// levenshtein is the classic edit distance. Cobra keeps its implementation
// unexported, so this mirrors it for ranking suggestions.
func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}

var (
	commandShapeRe = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	uuidShapeRe    = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
)

const maxTokenLength = 20

// maxTokenEntropy bounds the Shannon entropy (bits per character) of an
// emitted token. Real command words stay around 3.2 bits; exceeding 3.7
// requires 13+ distinct characters, which reads as a random identifier, not a
// command name.
const maxTokenEntropy = 3.7

// filterCommandToken applies the command-shape filter (#578): the raw token is
// emitted only when it plausibly is a command name. Everything else (values,
// paths, URLs, IPs, UUIDs, random identifiers) is replaced with redactedToken.
// Callers compute the fuzzy distance from the raw token before filtering, so
// redacted guesses still count.
func filterCommandToken(token string) string {
	switch {
	case !commandShapeRe.MatchString(token),
		len(token) > maxTokenLength,
		strings.Count(token, "-") > 1,
		strings.ContainsAny(token, "0123456789"),
		shannonEntropy(token) > maxTokenEntropy,
		net.ParseIP(token) != nil,
		looksLikeURL(token),
		uuidShapeRe.MatchString(token):
		return redactedToken
	}
	return token
}

func looksLikeURL(token string) bool {
	u, err := url.Parse(token)
	return err == nil && u.Scheme != "" && u.Host != ""
}

// shannonEntropy returns the entropy of the token in bits per character.
func shannonEntropy(s string) float64 {
	if s == "" {
		return 0
	}
	freq := map[rune]int{}
	n := 0
	for _, r := range s {
		freq[r]++
		n++
	}
	var h float64
	for _, c := range freq {
		p := float64(c) / float64(n)
		h -= p * math.Log2(p)
	}
	return h
}
