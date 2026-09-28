package agent

import (
	"slices"
	"strings"
	"unicode"
)

//nolint:gochecknoglobals // Immutable normalization vocabulary, shared by document and query tokenization.
var searchWordForms = map[string]string{
	"locally":        "local",
	"show":           "list",
	"display":        "list",
	"browse":         "list",
	"enumerate":      "list",
	"which":          "list",
	"what":           "list",
	"add":            "create",
	"make":           "create",
	"modify":         "update",
	"edit":           "update",
	"remove":         "delete",
	"deleting":       "delete",
	"uploading":      "upload",
	"dashboards":     "dashboard",
	"datasources":    "datasource",
	"resources":      "resource",
	"schemas":        "schema",
	"types":          "type",
	"kinds":          "kind",
	"stacks":         "stack",
	"collectors":     "collector",
	"pipelines":      "pipeline",
	"incidents":      "incident",
	"profiles":       "profile",
	"traces":         "trace",
	"checks":         "check",
	"probes":         "probe",
	"alerts":         "alert",
	"alerting":       "alert",
	"skills":         "skill",
	"guides":         "skill",
	"guide":          "skill",
	"tables":         "table",
	"contexts":       "context",
	"manifests":      "manifest",
	"files":          "file",
	"versions":       "version",
	"revisions":      "revision",
	"folders":        "folder",
	"metrics":        "metric",
	"definitions":    "definition",
	"pages":          "paging",
	"investigations": "investigation",
	"tests":          "test",
	"rules":          "rule",
	"fields":         "field",
	"postgresql":     "postgres",
}

//nolint:gochecknoglobals // Immutable phrase normalization, applied at token boundaries.
var searchCompoundForms = map[string]string{
	"data sources": "datasources", "data source": "datasource", "on call": "oncall",
	"roll back": "rollback", "back up": "backup", "log in": "login",
	"sign into": "login", "sign in": "login", "set up": "setup", "look up": "lookup",
	"am i using": "current", "i am using": "current",
}

func searchTokens(text string) []string {
	text = strings.ToLower(text)
	words := strings.FieldsFunc(text, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
	result := make([]string, 0, len(words))
	for i := 0; i < len(words); i++ {
		word := words[i]
		// Longest phrase first, before removing filler such as "on" and "in".
		for length := min(3, len(words)-i); length >= 2; length-- {
			if folded, ok := searchCompoundForms[strings.Join(words[i:i+length], " ")]; ok {
				word = folded
				i += length - 1
				break
			}
		}
		switch word {
		case "a", "an", "and", "are", "can", "do", "for", "from", "gcx", "how", "i", "in", "is", "it", "me", "my", "of", "on", "please", "the", "to", "want", "with", "all", "this", "that", "am", "by", "as", "at", "if", "before", "every", "much", "their", "them", "but", "not", "without", "don", "t", "right", "exist", "exists", "configured", "particular", "using", "grafana", "put", "into":
			continue
		}
		if folded, ok := searchWordForms[word]; ok {
			word = folded
		}
		if !slices.Contains(result, word) {
			result = append(result, word)
		}
	}
	return result
}

// searchAction identifies generic operations/modifiers that cannot establish a
// subject match on their own. It does not infer whether a command mutates state.
func searchAction(word string) bool {
	switch word {
	case "list", "get", "lookup", "create", "update", "delete", "query", "search", "find", "run", "execute", "fetch", "retrieve", "inspect", "check", "validate", "verify", "set", "start", "stop", "open", "close", "install", "setup", "send", "write", "deploy", "book", "buy", "rotate", "restart", "translate", "schedule", "order", "help", "something", "new", "file", "json", "yaml", "manifest", "available", "local", "current", "active", "name", "change", "select", "different", "another", "supported":
		return true
	}
	return false
}

func searchQuery(query string) ([]string, []string) {
	// Recognize exclusions before tokenization removes their negation. Keep
	// contractions intact, including typographic apostrophes from pasted text.
	query = strings.ReplaceAll(strings.ToLower(query), "’", "'")
	words := strings.FieldsFunc(query, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r) && r != '\'' && r != '-'
	})
	var excluded []string
	kept := make([]string, 0, len(words))
	for i := 0; i < len(words); i++ {
		if (words[i] == "not" || words[i] == "don't" || words[i] == "without") && i+1 < len(words) {
			next := searchTokens(words[i+1])
			if len(next) == 1 && searchAction(next[0]) {
				excluded = append(excluded, next[0])
				i++
				continue
			}
		}
		kept = append(kept, words[i])
	}
	return searchTokens(strings.Join(kept, " ")), excluded
}

// Read-shaped queries must not suggest a write merely because its help describes
// reading first. This conservative operation list covers command grammar; it is
// not a security classification or an execution permission check.
func searchReadRequest(terms []string) bool {
	if len(terms) == 0 {
		return false
	}
	switch terms[0] {
	case "list", "get", "search", "find", "inspect", "query", "fetch", "retrieve":
		return true
	}
	return false
}

func searchMutation(path string) bool {
	parts := strings.Fields(path)
	if len(parts) == 0 {
		return false
	}
	verb := strings.Split(parts[len(parts)-1], "-")[0]
	switch verb {
	case "create", "add", "update", "delete", "remove", "set", "unset", "reset", "restore", "apply", "upsert", "install", "uninstall", "start", "stop", "use", "push", "edit", "include", "exclude", "clear", "acknowledge", "resolve", "close", "escalate", "silence":
		return true
	}
	return false
}
