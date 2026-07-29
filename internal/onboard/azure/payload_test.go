//nolint:testpackage // white-box tests exercise unexported payload builders
package azure

import "testing"

func TestAzureMonitorPayload_FlatSchema(t *testing.T) {
	acct := Account{TenantID: "t-1", SubID: "sub-1", CloudName: "AzureCloud"}
	cred := SPCredential{AppID: "app-1", Password: "secret", Tenant: "t-1"}

	req := azureMonitorSpec{}.payload("gcx-azure-monitor", acct, cred)

	if req.Type != KindAzureMonitor {
		t.Fatalf("type = %q", req.Type)
	}
	if req.JSONData["azureAuthType"] != "clientsecret" {
		t.Fatalf("azureAuthType = %v", req.JSONData["azureAuthType"])
	}
	if req.JSONData["cloudName"] != "azuremonitor" {
		t.Fatalf("cloudName = %v", req.JSONData["cloudName"])
	}
	if req.JSONData["clientId"] != "app-1" || req.JSONData["tenantId"] != "t-1" || req.JSONData["subscriptionId"] != "sub-1" {
		t.Fatalf("unexpected jsonData: %v", req.JSONData)
	}
	if req.SecureJSONData["clientSecret"] != "secret" {
		t.Fatalf("clientSecret = %q", req.SecureJSONData["clientSecret"])
	}
	// Flat schema must NOT nest azureCredentials.
	if _, ok := req.JSONData["azureCredentials"]; ok {
		t.Fatal("azure monitor payload must use the flat schema")
	}
}

func TestADXPayload_NestedAzureCredentials(t *testing.T) {
	acct := Account{TenantID: "t-1", SubID: "sub-1", CloudName: "AzureUSGovernment"}
	cred := SPCredential{AppID: "app-1", Password: "secret", Tenant: "t-1"}

	req := adxSpec{}.payload("gcx-adx-foo", acct, cred, "https://foo.kusto.windows.net")

	if req.Type != KindADX {
		t.Fatalf("type = %q", req.Type)
	}
	if req.JSONData["clusterUrl"] != "https://foo.kusto.windows.net" {
		t.Fatalf("clusterUrl = %v", req.JSONData["clusterUrl"])
	}
	creds, ok := req.JSONData["azureCredentials"].(map[string]any)
	if !ok {
		t.Fatalf("azureCredentials missing or wrong type: %v", req.JSONData["azureCredentials"])
	}
	if creds["authType"] != "clientsecret" {
		t.Fatalf("authType = %v", creds["authType"])
	}
	if creds["azureCloud"] != "AzureUSGovernment" {
		t.Fatalf("azureCloud = %v", creds["azureCloud"])
	}
	if req.SecureJSONData["azureClientSecret"] != "secret" {
		t.Fatalf("azureClientSecret = %q", req.SecureJSONData["azureClientSecret"])
	}
}

func TestCloudNameMapping(t *testing.T) {
	cases := []struct {
		env         string
		wantMonitor string
		wantARM     string
	}{
		{"AzureCloud", "azuremonitor", "AzureCloud"},
		{"AzureUSGovernment", "govazuremonitor", "AzureUSGovernment"},
		{"AzureChinaCloud", "chinaazuremonitor", "AzureChinaCloud"},
		{"", "azuremonitor", "AzureCloud"},
	}
	for _, c := range cases {
		if got := monitorCloud(c.env); got != c.wantMonitor {
			t.Errorf("monitorCloud(%q) = %q, want %q", c.env, got, c.wantMonitor)
		}
		if got := armCloud(c.env); got != c.wantARM {
			t.Errorf("armCloud(%q) = %q, want %q", c.env, got, c.wantARM)
		}
	}
}

func TestArtifactName(t *testing.T) {
	cases := []struct {
		stack, token, resource string
		want                   string
	}{
		{"mystack", "azure-monitor", "", "gcx-mystack-azure-monitor"},
		{"", "azure-monitor", "", "gcx-azure-monitor"},
		{"mystack", "adx", "My Cluster", "gcx-mystack-adx-my-cluster"},
	}
	for _, c := range cases {
		if got := artifactName(c.stack, c.token, c.resource); got != c.want {
			t.Errorf("artifactName(%q,%q,%q) = %q, want %q", c.stack, c.token, c.resource, got, c.want)
		}
	}
}
