package org

import (
	"strconv"
	"strings"

	cmdio "github.com/grafana/gcx/internal/output"
)

type accountListResult struct {
	Items    []ServiceAccount `json:"items" yaml:"items"`
	ListMeta *cmdio.ListMeta  `json:"list_meta,omitempty" yaml:"list_meta,omitempty"`
}
type roleListResult struct {
	Items    []Role          `json:"items" yaml:"items"`
	ListMeta *cmdio.ListMeta `json:"list_meta,omitempty" yaml:"list_meta,omitempty"`
}

func accountListTable() cmdio.TableAdapter[accountListResult, ServiceAccount] {
	return cmdio.TableAdapter[accountListResult, ServiceAccount]{
		Table: cmdio.Table[ServiceAccount]{Columns: []cmdio.Column[ServiceAccount]{
			{Header: "ID", Content: func(a ServiceAccount) string { return strconv.FormatInt(a.ID, 10) }},
			{Header: "NAME", Content: func(a ServiceAccount) string { return a.Name }},
			{Header: "ROLE", Content: func(a ServiceAccount) string { return a.Role }},
			{Header: "DISABLED", Content: func(a ServiceAccount) string { return strconv.FormatBool(a.IsDisabled) }},
			{Header: "TOKENS", Content: func(a ServiceAccount) string { return strconv.FormatInt(a.Tokens, 10) }},
		}},
		Rows: func(result accountListResult) ([]ServiceAccount, error) { return result.Items, nil },
	}
}
func roleListTable() cmdio.TableAdapter[roleListResult, Role] {
	return cmdio.TableAdapter[roleListResult, Role]{
		Table: cmdio.Table[Role]{Columns: []cmdio.Column[Role]{
			{Header: "UID", Content: func(r Role) string { return r.UID }},
			{Header: "NAME", Content: func(r Role) string { return r.Name }},
			{Header: "DESCRIPTION", Content: func(r Role) string { return r.Description }},
		}},
		Rows: func(result roleListResult) ([]Role, error) { return result.Items, nil },
	}
}
func orgMutationTable() cmdio.TableAdapter[cmdio.SingleMutation, cmdio.SingleMutation] {
	return cmdio.TableAdapter[cmdio.SingleMutation, cmdio.SingleMutation]{
		Table: cmdio.Table[cmdio.SingleMutation]{Columns: []cmdio.Column[cmdio.SingleMutation]{
			{Header: "ACTION", Content: func(m cmdio.SingleMutation) string { return m.Action }},
			{Header: "KIND", Content: func(m cmdio.SingleMutation) string { return m.Target.Kind }},
			{Header: "ID", Content: func(m cmdio.SingleMutation) string { return m.Target.ID }},
			{Header: "NAME", Content: func(m cmdio.SingleMutation) string { return m.Target.Name }},
			{Header: "UID", Content: func(m cmdio.SingleMutation) string { return m.Target.UID }},
			{Header: "DRY RUN", Content: func(m cmdio.SingleMutation) string { return strconv.FormatBool(m.DryRun) }},
		}},
		Rows: func(m cmdio.SingleMutation) ([]cmdio.SingleMutation, error) { return []cmdio.SingleMutation{m}, nil },
	}
}

func orgTokenReceiptTable() cmdio.TableAdapter[cmdio.ArtifactReceipt, cmdio.ArtifactReceipt] {
	return cmdio.TableAdapter[cmdio.ArtifactReceipt, cmdio.ArtifactReceipt]{
		Table: cmdio.Table[cmdio.ArtifactReceipt]{Columns: []cmdio.Column[cmdio.ArtifactReceipt]{
			{Header: "ACTION", Content: func(r cmdio.ArtifactReceipt) string { return r.Action }},
			{Header: "FILE", Content: func(r cmdio.ArtifactReceipt) string {
				paths := make([]string, 0, len(r.Files))
				for _, file := range r.Files {
					paths = append(paths, file.Path)
				}
				return strings.Join(paths, ", ")
			}},
			{Header: "SUCCEEDED", Content: func(r cmdio.ArtifactReceipt) string { return strconv.Itoa(r.Summary.Succeeded) }},
			{Header: "SKIPPED", Content: func(r cmdio.ArtifactReceipt) string { return strconv.Itoa(r.Summary.Skipped) }},
		}},
		Rows: func(r cmdio.ArtifactReceipt) ([]cmdio.ArtifactReceipt, error) { return []cmdio.ArtifactReceipt{r}, nil },
	}
}
