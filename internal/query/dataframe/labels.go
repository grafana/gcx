package dataframe

import (
	"sort"
	"strconv"
	"strings"
)

// FormatLabels renders labels in Prometheus selector syntax ({k="v",k2="v2"})
// with keys sorted for stable output. An empty or nil map renders as "{}".
func FormatLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return "{}"
	}

	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	b.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(strconv.Quote(labels[k]))
	}
	b.WriteByte('}')

	return b.String()
}
