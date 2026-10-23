package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

func fixture(args []string, out, diagnostics io.Writer) int {
	if len(args) < 2 || args[0] != "resources" {
		fmt.Fprintln(diagnostics, "unsupported fixture command")
		return 2
	}
	selector := ""
	for i := 2; i < len(args); i++ {
		switch args[i] {
		case "--output", "--limit":
			i++
		case "--no-schema":
		default:
			selector = args[i]
		}
	}
	var payload any
	switch args[1] {
	case "list-types":
		entries := []typeEntry{}
		for _, entry := range []typeEntry{{Kind: "Dashboard", Plural: "dashboards", Singular: "dashboard"}, {Kind: "Folder", Plural: "folders", Singular: "folder"}} {
			if selector != "" && !strings.HasPrefix(selector, entry.Plural) && selector != entry.Singular {
				continue
			}
			entry.Schema = map[string]any{"type": "object", "properties": map[string]any{"title": map[string]any{"type": "string"}}, "description": "Offline " + entry.Kind + " spec"}
			entries = append(entries, entry)
		}
		if len(entries) == 0 {
			fmt.Fprintln(diagnostics, "unknown resource selector:", selector)
			return 2
		}
		payload = typeResponse{"demo.grafana.app": {"v1": entries}}
	case "get":
		all := false
		for i := 0; i+1 < len(args); i++ {
			if args[i] == "--limit" && args[i+1] == "0" {
				all = true
			}
		}
		if !all {
			fmt.Fprintln(diagnostics, "fixture requires --limit 0")
			return 2
		}
		kind, name, title := "Dashboard", "demo-overview", "Demo overview"
		if strings.HasPrefix(selector, "folders") {
			kind, name, title = "Folder", "demo-team", "Demo team"
		}
		payload = map[string]any{"items": []any{
			map[string]any{"kind": kind, "apiVersion": "demo.grafana.app/v1", "metadata": map[string]any{"name": name}, "spec": map[string]any{"title": title}},
			map[string]any{"apiVersion": "demo.grafana.app/v1", "metadata": map[string]any{"name": name + "-without-kind"}, "spec": map[string]any{"title": "Example without kind"}},
		}}
	default:
		fmt.Fprintln(diagnostics, "unsupported fixture command")
		return 2
	}
	if err := json.NewEncoder(out).Encode(payload); err != nil {
		fmt.Fprintln(diagnostics, err)
		return 1
	}
	return 0
}
