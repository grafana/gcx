package helm_test

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	instrumentation "github.com/grafana/gcx/internal/providers/instrumentation"
	"github.com/grafana/gcx/internal/providers/instrumentation/helm"
)

func testFleetManagement() instrumentation.FleetManagement {
	return instrumentation.FleetManagement{
		URL:      "https://fleet-management-prod-001.grafana.net",
		Username: "987654",
	}
}

const knownToken = "glc_someAccessPolicyToken=="
const knownCluster = "my-cluster"

// golden is the expected output matching the IHub UI for grafana-cloud-onboarding.
const golden = `helm upgrade --install grafana-cloud grafana/grafana-cloud-onboarding \
  --namespace monitoring --create-namespace \
  --set cluster.name='my-cluster' \
  --set grafanaCloud.fleetManagement.auth.password='glc_someAccessPolicyToken==' \
  --set grafanaCloud.fleetManagement.auth.username='987654' \
  --set grafanaCloud.fleetManagement.url='https://fleet-management-prod-001.grafana.net' \
  --wait`

func TestFormat_Golden(t *testing.T) {
	got := helm.Format(knownCluster, testFleetManagement(), knownToken)
	if got != golden {
		t.Errorf("Format output does not match golden fixture.\ngot:\n%s\n\nwant:\n%s", got, golden)
	}
}

func TestFormat_StableOutput(t *testing.T) {
	first := helm.Format(knownCluster, testFleetManagement(), knownToken)
	second := helm.Format(knownCluster, testFleetManagement(), knownToken)
	if first != second {
		t.Errorf("Format is not stable: two calls with identical inputs produced different output")
	}
}

func TestFormat_NoTrailingBackslash(t *testing.T) {
	out := helm.Format(knownCluster, testFleetManagement(), knownToken)
	if strings.HasSuffix(out, "\\") {
		t.Errorf("Format output must not end with a backslash continuation; got:\n%s", out)
	}
}

func TestFormat_ShellSafety(t *testing.T) {
	fm := testFleetManagement()
	tests := []struct {
		name    string
		cluster string
		token   string
	}{
		{name: "cluster with spaces", cluster: "my cluster name", token: knownToken},
		{name: "cluster with single quote", cluster: "it's-a-cluster", token: knownToken},
		{name: "token with single quote", cluster: "plain-cluster", token: "glc_it's_a_token"},
	}

	bash, err := exec.LookPath("bash")
	hasBash := err == nil

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := helm.Format(tc.cluster, fm, tc.token)
			if hasBash {
				cmd := exec.CommandContext(context.Background(), bash, "-n")
				cmd.Stdin = strings.NewReader(out)
				if err := cmd.Run(); err != nil {
					t.Errorf("bash -n rejected the formatted command: %v\ncommand:\n%s", err, out)
				}
			}
			if !strings.Contains(out, "--set cluster.name=") {
				t.Errorf("output missing --set cluster.name flag:\n%s", out)
			}
			if !strings.Contains(out, "grafana-cloud-onboarding") {
				t.Errorf("output must reference grafana-cloud-onboarding chart:\n%s", out)
			}
		})
	}
}

func TestFormat_EscapedSingleQuote(t *testing.T) {
	out := helm.Format("it's-a-cluster", testFleetManagement(), knownToken)
	if !strings.Contains(out, `'it'\''s-a-cluster'`) {
		t.Errorf("single-quote in cluster name not escaped with canonical POSIX form.\ngot:\n%s", out)
	}
}
