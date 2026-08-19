package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const (
	// artifactPrefix is stamped on every Azure and Grafana object this
	// extension creates so `cleanup` can find them again.
	artifactPrefix = "gcx-ext-azure"

	kindAzureMonitor = "grafana-azure-monitor-datasource"
	kindADX          = "grafana-azure-data-explorer-datasource"

	tokenAzureMonitor = "azure-monitor"
	tokenADX          = "adx"

	roleAssignRetries = 6
	roleAssignBackoff = 5 * time.Second
	secretExpiryYears = 1
)

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// suggestion is one datasource the extension proposes to create.
type suggestion struct {
	Token string   `json:"token"`
	Kind  string   `json:"kind"`
	Name  string   `json:"name"`
	UID   string   `json:"uid"`
	Roles []string `json:"roles"`
	Scope string   `json:"scope"`
	// ClusterURI is set for ADX suggestions.
	ClusterURI string `json:"clusterUri,omitempty"`
	// Hint is a non-blocking advisory, e.g. the backing resource is not
	// publicly reachable so Private Data source Connect is likely needed.
	Hint string `json:"hint,omitempty"`
}

// buildPlan discovers what is worth onboarding in a subscription. Azure Monitor
// always applies; one ADX datasource is proposed per running cluster.
func buildPlan(ctx context.Context, az azCLI, acct account, label string, types []string) []suggestion {
	scope := "/subscriptions/" + acct.ID
	var out []suggestion

	if wants(types, tokenAzureMonitor) {
		out = append(out, suggestion{
			Token: tokenAzureMonitor,
			Kind:  kindAzureMonitor,
			Name:  artifactName(label, tokenAzureMonitor, acct.Name),
			UID:   artifactUID(acct.ID, tokenAzureMonitor, ""),
			Roles: []string{"Reader"},
			Scope: scope,
		})
	}

	if !wants(types, tokenADX) {
		return out
	}
	clusters, err := az.listKustoClusters(ctx, acct.ID)
	if err != nil {
		// ADX discovery needs the `kusto` az extension and Reader access.
		// Treat any failure as "no clusters" rather than failing the run.
		return out
	}
	for _, cl := range clusters {
		if !strings.EqualFold(cl.State, "Running") {
			continue
		}
		s := suggestion{
			Token:      tokenADX,
			Kind:       kindADX,
			Name:       artifactName(label, tokenADX, cl.Name),
			UID:        artifactUID(acct.ID, tokenADX, cl.Name),
			Roles:      []string{"Reader"},
			Scope:      scope,
			ClusterURI: cl.URI,
		}
		if strings.EqualFold(cl.PublicNetworkAccess, "Disabled") {
			s.Hint = "cluster has public network access disabled; Grafana Cloud will need Private Data source Connect to reach it"
		}
		out = append(out, s)
	}
	return out
}

func wants(types []string, token string) bool {
	if len(types) == 0 {
		return true
	}
	for _, t := range types {
		if strings.EqualFold(strings.TrimSpace(t), token) {
			return true
		}
	}
	return false
}

// artifactName is the human-facing name shown in Grafana and Entra ID.
func artifactName(label, token, qualifier string) string {
	parts := []string{artifactPrefix, slug(label), token}
	if qualifier != "" {
		parts = append(parts, slug(qualifier))
	}
	return strings.Join(parts, "-")
}

// artifactUID is stable for a (subscription, kind, cluster) triple so a re-run
// finds the datasource it created last time instead of duplicating it.
func artifactUID(subscription, token, qualifier string) string {
	sum := sha256.Sum256([]byte(subscription + "/" + token + "/" + qualifier))
	return fmt.Sprintf("%s-%s-%s", artifactPrefix, token, hex.EncodeToString(sum[:])[:8])
}

func slug(s string) string {
	s = nonAlnum.ReplaceAllString(strings.ToLower(s), "-")
	return strings.Trim(s, "-")
}
