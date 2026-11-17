package athena

import "strings"

func (r StringList) CSVRecords() ([]string, [][]string, error) {
	rows := make([][]string, 0, len(r.Items))
	for _, item := range r.Items {
		rows = append(rows, []string{item})
	}
	return []string{strings.ToLower(r.Header)}, rows, nil
}
