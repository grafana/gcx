// Package helm formats the parameterized helm install command printed by
// gcx instrumentation setup. gcx never executes helm; the package only
// produces a copy-pasteable command string.
package helm

import (
	"strings"

	instrumentation "github.com/grafana/gcx/internal/providers/instrumentation"
	"github.com/grafana/gcx/internal/shellquote"
)

// Format returns a runnable helm command string that installs
// grafana-cloud-onboarding with the given cluster name, fleet management
// connection parameters, and access-policy token.
//
// Flags appear in stable alphabetical order by key, one --set per line,
// backslash-continued, followed by --wait. All values are single-quote-escaped
// so the command is safe to paste into any POSIX shell.
func Format(cluster string, fm instrumentation.FleetManagement, accessPolicyToken string) string {
	type setFlag struct {
		key string
		val string
	}

	// Flags in stable alphabetical order by key.
	flags := []setFlag{
		{"cluster.name", cluster},
		{"grafanaCloud.fleetManagement.auth.password", accessPolicyToken},
		{"grafanaCloud.fleetManagement.auth.username", fm.Username},
		{"grafanaCloud.fleetManagement.url", fm.URL},
	}

	var sb strings.Builder
	sb.WriteString("helm upgrade --install grafana-cloud grafana/grafana-cloud-onboarding \\\n")
	sb.WriteString("  --namespace monitoring --create-namespace")

	for _, f := range flags {
		sb.WriteString(" \\\n  --set ")
		sb.WriteString(f.key)
		sb.WriteByte('=')
		sb.WriteString(shellquote.Escape(f.val))
	}

	sb.WriteString(" \\\n  --wait")
	return sb.String()
}
