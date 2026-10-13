package faro

import (
	"regexp"
	"strconv"
	"strings"
)

// stackFrame is one parsed frame of a stored exception stack trace.
type stackFrame struct {
	Function string `json:"function,omitempty"`
	Module   string `json:"module,omitempty"`
	File     string `json:"file"`
	// Path is File with bundler URL prefixes removed (webpack://<ns>/./src/a.ts
	// → src/a.ts), for locating the file in the repository. Omitted when it
	// equals File.
	Path  string `json:"path,omitempty"`
	Line  int    `json:"line"`
	Col   int    `json:"col"`
	InApp bool   `json:"in_app"`
}

// storedFrameRE matches one line of the flattened stack trace the Faro
// collector writes to faro_pinot_exceptions_v1.exceptionStacktrace:
// "fn (location:line:col)" for the first frame and "\n  at fn (location:line:col)"
// for the rest; fn is omitted when empty. Ported from
// app-o11y-kwl-endpoint pkg/fingerprint/signature.go (storedFrameRE).
var storedFrameRE = regexp.MustCompile(`^\s*(?:at\s+)?(.*?)\s*\(([^()]*)\)\s*$`)

var (
	leadingAtRE  = regexp.MustCompile(`^at\s+`)
	lineColRE    = regexp.MustCompile(`^(.*?):(\d+):(\d+)$`)
	lineOnlyRE   = regexp.MustCompile(`^(.*?):(\d+)$`)
	moduleFileRE = regexp.MustCompile(`^([^|]+)\|(.+)$`)
)

// parseStoredStacktrace parses stored stack-trace text into frames. Unlike the
// endpoint's ParseStoredStacktrace, it keeps line and column so the caller can
// open the right place in the file. JVM frames store "module|filename" as the
// location; those are split into Module and File. Lines that do not look like
// frames are skipped.
func parseStoredStacktrace(text string) []stackFrame {
	var frames []stackFrame
	for line := range strings.SplitSeq(text, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		m := storedFrameRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		f := stackFrame{
			Function: leadingAtRE.ReplaceAllString(strings.TrimSpace(m[1]), ""),
		}
		loc := strings.TrimSpace(m[2])
		if lc := lineColRE.FindStringSubmatch(loc); lc != nil {
			loc = lc[1]
			f.Line, _ = strconv.Atoi(lc[2])
			f.Col, _ = strconv.Atoi(lc[3])
		} else if l := lineOnlyRE.FindStringSubmatch(loc); l != nil {
			loc = l[1]
			f.Line, _ = strconv.Atoi(l[2])
		}
		if mf := moduleFileRE.FindStringSubmatch(loc); mf != nil {
			f.Module = mf[1]
			loc = mf[2]
		}
		f.File = loc
		if p := repoPath(loc); p != loc {
			f.Path = p
		}
		f.InApp = !isLibraryFrame(loc)
		frames = append(frames, f)
	}
	return frames
}

// webpackPrefixRE matches webpack source URLs: webpack:///, webpack://./,
// and webpack://<namespace>/, each optionally followed by "./".
var webpackPrefixRE = regexp.MustCompile(`^webpack://(?:[^/]*/)?(?:\./)?`)

// repoPath strips bundler URL prefixes so the path is repository-relative.
func repoPath(file string) string {
	return webpackPrefixRE.ReplaceAllString(file, "")
}

// isLibraryFrame reports whether a frame's filename indicates it originates
// from a third-party library rather than the application's own code. Ported
// verbatim from app-o11y-kwl-endpoint pkg/fingerprint/normalizer.go so gcx
// classifies frames the same way error grouping does.
func isLibraryFrame(filename string) bool {
	if filename == "" {
		return true
	}

	// Strip webpack prefixes to expose the underlying path
	cleaned := strings.TrimPrefix(filename, "webpack://./")
	cleaned = strings.TrimPrefix(cleaned, "webpack:///")
	cleaned = strings.TrimPrefix(cleaned, "webpack://")

	if strings.Contains(cleaned, "node_modules/") {
		return true
	}

	if strings.HasPrefix(cleaned, "chrome-extension://") ||
		strings.HasPrefix(cleaned, "moz-extension://") ||
		strings.HasPrefix(cleaned, "safari-extension://") {
		return true
	}

	if strings.HasPrefix(cleaned, "[native") || strings.HasPrefix(cleaned, "(native") {
		return true
	}

	// V8 reports frames with no JS source location (native built-ins like
	// Array.prototype.forEach, unlabeled eval, new Function()) as
	// "<anonymous>". These are never application code.
	if cleaned == "<anonymous>" {
		return true
	}

	return false
}

// framesSymbolicated is a heuristic for whether source maps were applied: at
// least one in-app frame has a real function name and a line above 1
// (minified bundles collapse to line 1).
func framesSymbolicated(frames []stackFrame) bool {
	for _, f := range frames {
		if f.InApp && f.Function != "" && f.Function != "?" && f.Line > 1 {
			return true
		}
	}
	return false
}

// longestStacktrace picks the trace with the most parsed frames, breaking
// ties by text length. Returns nil when no candidate parses.
func longestStacktrace(texts []string) []stackFrame {
	var best []stackFrame
	bestLen := -1
	for _, t := range texts {
		frames := parseStoredStacktrace(t)
		if len(frames) == 0 {
			continue
		}
		if len(frames) > len(best) || (len(frames) == len(best) && len(t) > bestLen) {
			best = frames
			bestLen = len(t)
		}
	}
	return best
}
