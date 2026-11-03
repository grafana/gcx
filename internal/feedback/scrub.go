package feedback

import (
	"net"
	"regexp"
	"strings"
)

// scrubRule replaces every match of re. When replace is set it decides the
// replacement for each match; otherwise repl is used with regexp expansion.
type scrubRule struct {
	re      *regexp.Regexp
	repl    string
	replace func(match string) string
}

// Rules run in order, and the order matters:
//   - Whole http(s) URLs go first, so their user info, hosts and paths
//     disappear in one piece.
//   - Token formats run before key=value secrets, so a value that is already
//     redacted is left alone.
//   - Emails run before bare stack hosts, so an address at a stack domain is
//     reported as an email.
//   - IPv6 runs before IPv4, so an IPv4-mapped address (::ffff:10.0.0.1) and
//     a zone suffix (fe80::1%en0) are replaced whole.
//   - Quoted secret values are replaced up to the closing quote, so a
//     passphrase with spaces goes in one piece.
//
//nolint:gochecknoglobals // compiled once, read-only
var scrubRules = []scrubRule{
	{re: regexp.MustCompile(`(?i)\bhttps?://[^\s"'<>` + "`" + `]+`), repl: "<redacted:url>"},
	{re: regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.-]*://)[^\s/@:]+:[^\s/@]*@`), repl: "${1}<redacted:userinfo>@"},
	{re: regexp.MustCompile(`(?i)(\bauthorization"?\s*[:=]\s*"?)(?:(?:bearer|basic|token)\s+)?[^\s"',;]+`), repl: "${1}<redacted:auth>"},
	{re: regexp.MustCompile(`(?i)\b(bearer|basic)(\s+)([A-Za-z0-9\-._~+/]{8,}=*)`), replace: redactAuthScheme},
	{re: regexp.MustCompile(`\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]*`), repl: "<redacted:token>"},
	{re: regexp.MustCompile(`\b(?:glc_|glsa[_-])[A-Za-z0-9+/=_-]{8,}`), repl: "<redacted:token>"},
	{re: regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{16,}|github_pat_[A-Za-z0-9_]{16,})`), repl: "<redacted:token>"},
	{re: regexp.MustCompile(`\bxox[abpr]-[A-Za-z0-9-]{8,}`), repl: "<redacted:token>"},
	{re: regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`), repl: "<redacted:token>"},
	{re: regexp.MustCompile(`(?i)(` + secretKey + `"?\s*[:=]\s*)"[^"]*"`), repl: `${1}"<redacted:secret>"`},
	{re: regexp.MustCompile(`(?i)(` + secretKey + `"?\s*[:=]\s*)'[^']*'`), repl: `${1}'<redacted:secret>'`},
	{re: regexp.MustCompile(`(?i)(` + secretKey + `"?\s*[:=]\s*"?)([^\s"',;&<]+)`), repl: "${1}<redacted:secret>"},
	{re: regexp.MustCompile(`(?i)(--` + secretKey + `\s+)([^\s"',;&<-][^\s"',;&<]*)`), repl: "${1}<redacted:secret>"},
	{re: regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)*\.[A-Za-z]{2,}`), repl: "<redacted:email>"},
	{re: regexp.MustCompile(`(?i)\b(?:[a-z0-9-]+\.)+grafana(?:-dev|-ops)?\.net\b`), repl: "<redacted:host>"},
	{re: regexp.MustCompile(`(?i)(?:[0-9a-f]{0,4}:){2,7}(?:(?:\d{1,3}\.){3}\d{1,3}|[0-9a-f]{0,4})(?:%[0-9a-z_.-]+)?`), replace: redactIPv6},
	{re: regexp.MustCompile(`\b(?:(?:25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)\.){3}(?:25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)\b`), repl: "<redacted:ip>"},
	{re: regexp.MustCompile(`[A-Za-z]:\\{1,2}Users\\{1,2}[^\\\s"':,;]+\\{1,2}`), repl: "~/"},
	{re: regexp.MustCompile(`[A-Za-z]:\\{1,2}Users\\{1,2}[^\\\s"':,;]+`), repl: "~"},
	{re: regexp.MustCompile(`(?:/Users|/home)/[^/\s"':,;]+`), repl: "~"},
}

// secretKey matches a key name that holds a secret, with any prefix or suffix
// (GRAFANA_TOKEN, cloud-token, access_token, db_password).
//
//nolint:gosec // G101: a pattern for recognising secret key names, not a credential
const secretKey = `[a-z0-9_.-]*(?:token|secret|passwd|password|api[_-]?key|access[_-]?key|private[_-]?key)[a-z0-9_-]*`

// redactAuthScheme redacts a Bearer or Basic credential. The credential must
// look like one, with a digit, base64 punctuation or an uppercase letter after
// the first, so prose such as "basic authentication" is left alone.
func redactAuthScheme(match string) string {
	fields := strings.Fields(match)
	if len(fields) != 2 || !looksLikeCredential(fields[1]) {
		return match
	}
	return fields[0] + " <redacted:auth>"
}

func looksLikeCredential(s string) bool {
	if strings.ContainsAny(s, "0123456789+/=_-.~") {
		return true
	}
	return len(s) > 1 && strings.ToLower(s[1:]) != s[1:]
}

// redactIPv6 redacts a candidate only when it parses as an IPv6 address, so
// clock times and MAC addresses survive. A single group before "::" is not
// enough, so scope operators such as "std::string" survive too.
func redactIPv6(match string) string {
	match, _, _ = strings.Cut(match, "%")
	if net.ParseIP(match) == nil {
		return match
	}
	groups := 0
	for g := range strings.SplitSeq(match, ":") {
		if g != "" {
			groups++
		}
	}
	if groups == 0 || (groups < 2 && !strings.HasPrefix(match, "::")) {
		return match
	}
	return "<redacted:ip>"
}

// Scrub replaces anything in s that could identify a person, an organisation,
// a stack, or a credential with a <redacted:KIND> placeholder, and home
// directory paths with ~. It is applied to every key and value before
// feedback is printed by --dry-run or sent.
func Scrub(s string) string {
	for _, rule := range scrubRules {
		if rule.replace != nil {
			s = rule.re.ReplaceAllStringFunc(s, rule.replace)
			continue
		}
		s = rule.re.ReplaceAllString(s, rule.repl)
	}
	return s
}
