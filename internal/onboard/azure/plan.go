package azure

import (
	"context"
	"regexp"
	"strings"

	"github.com/grafana/gcx/internal/onboard"
	"github.com/grafana/grafana-app-sdk/logging"
)

// PlanInput controls suggestion building.
type PlanInput struct {
	CLI           *CLI
	Stack         string // naming label (e.g. stack slug) used in artifact names
	Account       Account
	IncludeCosmos bool
	Log           logging.Logger
}

// BuildPlan discovers Azure resources and returns the datasources gcx suggests
// creating. Azure Monitor is always suggested; ADX is suggested per discovered
// cluster (one datasource per cluster); Cosmos DB is suggested per account only
// when opted in.
func BuildPlan(ctx context.Context, in PlanInput) []Suggestion {
	sub := "/subscriptions/" + in.Account.SubID
	out := []Suggestion{{
		Spec:   azureMonitorSpec{},
		Name:   artifactName(in.Stack, TokenAzureMonitor, ""),
		Label:  "Azure Monitor (" + in.Account.Name + ")",
		Scopes: []string{sub},
	}}

	clusters, err := in.CLI.ListKustoClusters(ctx)
	if err != nil && in.Log != nil {
		in.Log.Debug("ADX discovery skipped", "error", err.Error())
	}
	for _, cl := range clusters {
		s := Suggestion{
			Spec:   adxSpec{},
			Name:   artifactName(in.Stack, TokenADX, cl.Name),
			Label:  "Azure Data Explorer — " + cl.Name,
			Scopes: []string{sub},
			Extra:  map[string]string{"clusterUrl": cl.URI, "rg": cl.RG, "cluster": cl.Name},
		}
		if !cl.IsRunning() {
			s.Disabled = true
			state := cl.State
			if state == "" {
				state = "not running"
			}
			s.DisabledReason = "cluster is " + strings.ToLower(state) + "; start it to provision"
		}
		out = append(out, s)
	}

	if in.IncludeCosmos {
		accounts, err := in.CLI.ListCosmosAccounts(ctx)
		if err != nil && in.Log != nil {
			in.Log.Debug("Cosmos discovery skipped", "error", err.Error())
		}
		for _, a := range accounts {
			out = append(out, Suggestion{
				Spec:  cosmosSpec{},
				Name:  artifactName(in.Stack, TokenCosmos, a.Name),
				Label: "Azure Cosmos DB — " + a.Name + " (requires Enterprise plugin)",
				Extra: map[string]string{"endpoint": a.DocumentEndpoint, "rg": a.RG, "account": a.Name},
			})
		}
	}

	return out
}

var nameSanitizer = regexp.MustCompile(`[^a-zA-Z0-9-]+`)

// artifactName builds a stable, gcx-prefixed name for an app registration /
// datasource: gcx-<stack>-<token>[-<resource>], lower-cased and sanitized.
func artifactName(stack, token, resource string) string {
	parts := []string{onboard.NamePrefix}
	if stack != "" {
		parts = append(parts, stack)
	}
	parts = append(parts, token)
	if resource != "" {
		parts = append(parts, resource)
	}
	name := strings.ToLower(strings.Join(parts, "-"))
	name = nameSanitizer.ReplaceAllString(name, "-")
	return strings.Trim(name, "-")
}
