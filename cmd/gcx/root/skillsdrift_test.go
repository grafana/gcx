package root_test

import (
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"strings"
	"testing"

	claudeplugin "github.com/grafana/gcx/claude-plugin"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type driftAllowance struct {
	file     string
	contains string
}

// knownSkillDrift lists gcx invocations in the bundled skills that are known
// to be wrong on main. All entries are already fixed on the PR #936 branch;
// delete the whole list when #936 merges. Each entry suppresses failures in
// one file whose message contains the given substring.
func knownSkillDrift() []driftAllowance {
	return []driftAllowance{
		{"diagnose-entity-graph/SKILL.md", "unknown command `gcx kg health`"},
		{"diagnose-entity-graph/SKILL.md", "unknown command `gcx kg cypher`"},
		// push takes -p/--path, not -f
		{"gcx-observability/SKILL.md", "unknown flag -f on `gcx resources push`"},
		{"manage-dashboards/references/resource-model.md", "unknown flag --all-versions on `gcx resources pull`"},
		// the acknowledge confirmation flag is --force, not --yes
		{"oncall-triage/SKILL.md", "unknown flag --yes on `gcx irm oncall alert-groups acknowledge`"},
	}
}

// TestSkillsGcxInvocationsMatchCommandTree extracts every gcx invocation from
// fenced code blocks in the bundled skills and validates the command path and
// flags against the real command tree, so skills fail CI when the CLI surface
// moves out from under them.
func TestSkillsGcxInvocationsMatchCommandTree(t *testing.T) {
	rootCmd := buildRootCmd()
	rootCmd.InitDefaultHelpCmd()
	rootCmd.InitDefaultCompletionCmd()

	total := 0
	skillsFS := claudeplugin.SkillsFS()
	err := fs.WalkDir(skillsFS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || path.Ext(p) != ".md" {
			return nil
		}
		content, err := fs.ReadFile(skillsFS, p)
		if err != nil {
			return err
		}
		for _, inv := range extractInvocations(string(content)) {
			total++
			verr := validateInvocation(rootCmd, inv.args)
			if verr == nil {
				continue
			}
			msg := fmt.Sprintf("`%s`: %v", inv, verr)
			if allowedDrift(p, msg) {
				continue
			}
			t.Errorf("claude-plugin/skills/%s:%d: %s", p, inv.line, msg)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking skills FS: %v", err)
	}

	// Guard against the extractor silently matching nothing after a refactor.
	if total < 100 {
		t.Fatalf("extracted only %d gcx invocations from bundled skills, expected hundreds; extractor is likely broken", total)
	}
	t.Logf("validated %d gcx invocations from bundled skills", total)
}

func TestValidateInvocation(t *testing.T) {
	rootCmd := buildRootCmd()
	rootCmd.InitDefaultHelpCmd()
	rootCmd.InitDefaultCompletionCmd()

	tests := []struct {
		name    string
		args    []string
		wantErr string // substring of the error, empty for valid
	}{
		{
			name: "simple valid command",
			args: []string{"slo", "definitions", "list"},
		},
		{
			name: "alias resolves",
			args: []string{"synth", "checks", "list"},
		},
		{
			// flags before the subcommand are parsed by the resolved leaf,
			// mirroring cobra: --config exists on the resources subtree only
			name: "leaf flag written before subcommand",
			args: []string{"--config", "x", "resources", "get", "dashboards"},
		},
		{
			name: "placeholder in command position skipped",
			args: []string{"<provider>", "--help"},
		},
		{
			name:    "unknown command",
			args:    []string{"kg", "health"},
			wantErr: "unknown command `gcx kg health`",
		},
		{
			name:    "unknown flag",
			args:    []string{"resources", "pull", "dashboards", "--all-versions"},
			wantErr: "unknown flag --all-versions on `gcx resources pull`",
		},
		{
			name:    "unknown shorthand",
			args:    []string{"resources", "push", "-f", "x.yaml"},
			wantErr: "unknown flag -f on `gcx resources push`",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateInvocation(rootCmd, tt.args)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("validateInvocation(%v) = %v, want nil", tt.args, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("validateInvocation(%v) = %v, want error containing %q", tt.args, err, tt.wantErr)
			}
		})
	}
}

func allowedDrift(file, msg string) bool {
	for _, k := range knownSkillDrift() {
		if k.file == file && strings.Contains(msg, k.contains) {
			return true
		}
	}
	return false
}

type invocation struct {
	line int
	args []string // tokens following "gcx"
}

func (inv invocation) String() string {
	return "gcx " + strings.Join(inv.args, " ")
}

var envAssignRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// isShellKeyword reports leading words stripped so invocations inside loops
// and conditionals (e.g. `do gcx ...`) are still recognised.
func isShellKeyword(tok string) bool {
	switch tok {
	case "do", "then", "else", "time", "exec":
		return true
	}
	return false
}

// extractInvocations returns every gcx invocation found in shell-flavoured
// (bash, sh, shell, or bare) fenced code blocks of a markdown document.
func extractInvocations(content string) []invocation {
	lines := strings.Split(content, "\n")
	var invs []invocation
	inFence, shellFence := false, false
	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, "```") {
			if inFence {
				inFence = false
				continue
			}
			inFence = true
			lang := strings.TrimSpace(strings.TrimPrefix(trimmed, "```"))
			shellFence = lang == "" || lang == "bash" || lang == "sh" || lang == "shell"
			continue
		}
		if !inFence || !shellFence {
			continue
		}
		start := i
		logical := lines[i]
		for strings.HasSuffix(strings.TrimRight(logical, " \t"), "\\") &&
			i+1 < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i+1]), "```") {
			logical = strings.TrimSuffix(strings.TrimRight(logical, " \t"), "\\") + " " + lines[i+1]
			i++
		}
		for _, cmd := range parseCommands(logical) {
			if args, ok := gcxArgs(cmd); ok {
				invs = append(invs, invocation{line: start + 1, args: args})
			}
		}
	}
	return invs
}

// gcxArgs strips env-var assignment prefixes and shell keywords and reports
// whether the remaining simple command is a gcx invocation.
func gcxArgs(tokens []string) ([]string, bool) {
	for len(tokens) > 0 && (envAssignRe.MatchString(tokens[0]) || isShellKeyword(tokens[0])) {
		tokens = tokens[1:]
	}
	if len(tokens) == 0 || tokens[0] != "gcx" {
		return nil, false
	}
	return tokens[1:], true
}

// placeholderStartRe matches doc placeholders like <uid> or <name|uuid> so
// they are kept as argument tokens instead of being read as redirects.
var placeholderStartRe = regexp.MustCompile(`^<[^<>\s]+>`)

// parseCommands splits a logical shell line into simple commands, splitting on
// pipes, &&, ||, ;, and subshell parens. It recurses into $(...) and backtick
// substitutions so nested gcx calls are extracted too, keeps quoted strings as
// single tokens, and stops collecting arguments at redirects.
func parseCommands(line string) [][]string {
	var (
		cmds   [][]string
		cur    []string
		word   strings.Builder
		inWord bool
	)
	flushWord := func() {
		if inWord {
			cur = append(cur, word.String())
			word.Reset()
			inWord = false
		}
	}
	flushCmd := func() {
		flushWord()
		if len(cur) > 0 {
			cmds = append(cmds, cur)
			cur = nil
		}
	}
	i := 0
	for i < len(line) {
		c := line[i]
		switch {
		case c == '\'':
			i = scanSingleQuoted(line, i+1, &word)
			inWord = true
		case c == '"':
			var nested [][]string
			nested, i = scanDoubleQuoted(line, i+1, &word)
			cmds = append(cmds, nested...)
			inWord = true
		case c == '$' && i+1 < len(line) && line[i+1] == '(':
			inner, next := captureParen(line, i+2)
			cmds = append(cmds, parseCommands(inner)...)
			word.WriteString("$(...)")
			inWord = true
			i = next
		case c == '`':
			end := strings.IndexByte(line[i+1:], '`')
			if end < 0 {
				i = len(line)
				break
			}
			cmds = append(cmds, parseCommands(line[i+1:i+1+end])...)
			word.WriteString("$(...)")
			inWord = true
			i += end + 2
		case c == ' ' || c == '\t':
			flushWord()
			i++
		case c == '|' || c == ';' || c == '&' || c == '(' || c == ')':
			flushCmd()
			i++
		case c == '<':
			if m := placeholderStartRe.FindString(line[i:]); m != "" {
				word.WriteString(m)
				inWord = true
				i += len(m)
				break
			}
			flushCmd()
			i++
		case c == '>':
			// drop a bare fd-number word so 2>/dev/null leaves no stray arg
			if inWord && isDigits(word.String()) {
				word.Reset()
				inWord = false
			}
			flushCmd()
			i++
		case c == '#' && !inWord:
			flushCmd()
			return cmds
		case c == '\\' && i+1 < len(line):
			word.WriteByte(line[i+1])
			inWord = true
			i += 2
		default:
			word.WriteByte(c)
			inWord = true
			i++
		}
	}
	flushCmd()
	return cmds
}

// scanSingleQuoted consumes a single-quoted section starting just after the
// opening quote, appending its content to word; it returns the index just
// past the closing quote.
func scanSingleQuoted(line string, i int, word *strings.Builder) int {
	end := strings.IndexByte(line[i:], '\'')
	if end < 0 {
		word.WriteString(line[i:])
		return len(line)
	}
	word.WriteString(line[i : i+end])
	return i + end + 1
}

// scanDoubleQuoted consumes a double-quoted section starting just after the
// opening quote, appending its content to word, honouring backslash escapes
// and recursing into $(...) substitutions. It returns commands found in the
// substitutions and the index just past the closing quote.
func scanDoubleQuoted(line string, i int, word *strings.Builder) ([][]string, int) {
	var cmds [][]string
	for i < len(line) && line[i] != '"' {
		if line[i] == '\\' && i+1 < len(line) {
			word.WriteByte(line[i+1])
			i += 2
			continue
		}
		if line[i] == '$' && i+1 < len(line) && line[i+1] == '(' {
			inner, next := captureParen(line, i+2)
			cmds = append(cmds, parseCommands(inner)...)
			word.WriteString("$(...)")
			i = next
			continue
		}
		word.WriteByte(line[i])
		i++
	}
	return cmds, i + 1
}

// captureParen returns the content up to the parenthesis matching an already
// consumed "$(" plus the index just past the closing paren, skipping over
// quoted sections.
func captureParen(line string, start int) (string, int) {
	depth := 1
	i := start
	for i < len(line) {
		switch line[i] {
		case '\'':
			end := strings.IndexByte(line[i+1:], '\'')
			if end < 0 {
				return line[start:], len(line)
			}
			i += end + 2
			continue
		case '"':
			j := i + 1
			for j < len(line) && line[j] != '"' {
				if line[j] == '\\' {
					j++
				}
				j++
			}
			i = j + 1
			continue
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return line[start:i], i + 1
			}
		}
		i++
	}
	return line[start:], len(line)
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func isPlaceholder(tok string) bool {
	return strings.ContainsAny(tok, "<>{}[]") || strings.HasPrefix(tok, "$")
}

func isFlagToken(tok string) bool {
	if len(tok) < 2 || tok[0] != '-' {
		return false
	}
	// negative numbers and placeholder-ish tokens are not flags
	return !isDigits(strings.TrimLeft(tok, "-")) && !isPlaceholder(tok)
}

// validateInvocation resolves the subcommand path of a single gcx invocation
// against the real command tree and checks that every flag used exists on the
// resolved command (including persistent flags inherited from parents).
//
// Like cobra, it resolves the command path first, skipping over flag tokens,
// and only then parses flags against the resolved leaf - so a flag may
// legally appear before the subcommand it belongs to (e.g. `gcx --config x
// resources get`, where --config lives on the resources subtree).
func validateInvocation(rootCmd *cobra.Command, args []string) error {
	// pass 1: resolve the command path, skipping flag tokens
	cmd := rootCmd
	var pathTokens []string
	descending := true
	for i := 0; i < len(args); i++ {
		tok := args[i]
		if tok == "--" {
			break
		}
		if isFlagToken(tok) {
			if flagConsumesValue(cmd, tok) {
				i++
			}
			continue
		}
		if !descending {
			continue
		}
		child := findChild(cmd, tok)
		if child == nil {
			if isPlaceholder(tok) {
				// placeholder in command position: the rest cannot be resolved
				return nil
			}
			if !cmd.Runnable() && cmd.HasSubCommands() {
				return fmt.Errorf("unknown command `gcx %s`", strings.Join(append(pathTokens, tok), " "))
			}
			descending = false // positional argument of a runnable command
			continue
		}
		cmd = child
		pathTokens = append(pathTokens, tok)
	}
	cmdPath := "gcx"
	if len(pathTokens) > 0 {
		cmdPath += " " + strings.Join(pathTokens, " ")
	}

	// pass 2: validate every flag against the resolved leaf
	for i := 0; i < len(args); i++ {
		tok := args[i]
		if tok == "--" {
			break
		}
		if !isFlagToken(tok) {
			continue
		}
		name, _, hasValue := strings.Cut(tok, "=")
		if long, isLong := strings.CutPrefix(name, "--"); isLong {
			if long == "help" || long == "version" {
				continue
			}
			f := lookupFlag(cmd, long)
			if f == nil {
				return fmt.Errorf("unknown flag %s on `%s`", name, cmdPath)
			}
			if !hasValue && f.NoOptDefVal == "" {
				i++ // the next token is this flag's value
			}
			continue
		}
		shorts := name[1:]
		for k := range len(shorts) {
			s := string(shorts[k])
			// the help flag is registered lazily by cobra at execute time
			if s == "h" {
				continue
			}
			f := lookupShorthand(cmd, s)
			if f == nil {
				return fmt.Errorf("unknown flag -%s on `%s`", s, cmdPath)
			}
			if f.NoOptDefVal != "" {
				continue // bool-like: further letters may be more shorthands
			}
			// non-bool: the rest of the token, an =value, or the next token is its value
			if k == len(shorts)-1 && !hasValue {
				i++
			}
			break
		}
	}
	return nil
}

// flagConsumesValue reports whether a flag token eats the next token during
// path resolution. Mirrors cobra's stripFlags: a space-separated flag not yet
// known at the current node is assumed to consume a value, because its
// definition may live on a leaf that is still being resolved.
func flagConsumesValue(cmd *cobra.Command, tok string) bool {
	name, _, hasValue := strings.Cut(tok, "=")
	if hasValue {
		return false
	}
	if long, isLong := strings.CutPrefix(name, "--"); isLong {
		if long == "help" || long == "version" {
			return false
		}
		if f := lookupFlag(cmd, long); f != nil {
			return f.NoOptDefVal == ""
		}
		return true
	}
	short := name[1:]
	// cobra only consumes a value for single-letter shorthands
	if len(short) != 1 || short == "h" {
		return false
	}
	if f := lookupShorthand(cmd, short); f != nil {
		return f.NoOptDefVal == ""
	}
	return true
}

func findChild(cmd *cobra.Command, name string) *cobra.Command {
	for _, c := range cmd.Commands() {
		if c.Name() == name || c.HasAlias(name) {
			return c
		}
	}
	return nil
}

func lookupFlag(cmd *cobra.Command, name string) *pflag.Flag {
	if f := cmd.Flags().Lookup(name); f != nil {
		return f
	}
	for c := cmd; c != nil; c = c.Parent() {
		if f := c.PersistentFlags().Lookup(name); f != nil {
			return f
		}
	}
	return nil
}

func lookupShorthand(cmd *cobra.Command, s string) *pflag.Flag {
	if f := cmd.Flags().ShorthandLookup(s); f != nil {
		return f
	}
	for c := cmd; c != nil; c = c.Parent() {
		if f := c.PersistentFlags().ShorthandLookup(s); f != nil {
			return f
		}
	}
	return nil
}
