package alert_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/alert"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

type provisioningFlagLoader struct{ host string }

const (
	contactPointsPath = "/api/v1/provisioning/contact-points"
	muteTimingsPath   = "/api/v1/provisioning/mute-timings"
	templatesPath     = "/api/v1/provisioning/templates"
	policiesPath      = "/api/v1/provisioning/policies"
)

func (l provisioningFlagLoader) LoadGrafanaConfig(context.Context) (config.NamespacedRESTConfig, error) {
	return config.NamespacedRESTConfig{Config: rest.Config{Host: l.host}}, nil
}

func TestProvisioningCommandsAllowUIUpdates(t *testing.T) {
	commands := []struct {
		name   string
		newCmd func(alert.GrafanaConfigLoader) *cobra.Command
		args   []string
		body   string
		method string
		path   string
	}{
		{"contact points create", alert.NewContactPointsCreateCommandForTest, nil, `{"name":"test","type":"email","settings":{}}`, http.MethodPost, contactPointsPath},
		{"contact points update", alert.NewContactPointsUpdateCommandForTest, []string{"cp"}, `{"name":"test","type":"email","settings":{}}`, http.MethodPut, contactPointsPath + "/cp"},
		{"mute timings create", alert.NewMuteTimingsCreateCommandForTest, nil, `{"name":"test","time_intervals":[]}`, http.MethodPost, muteTimingsPath},
		{"mute timings update", alert.NewMuteTimingsUpdateCommandForTest, []string{"test"}, `{"name":"test","time_intervals":[]}`, http.MethodPut, muteTimingsPath + "/test"},
		{"templates upsert", alert.NewTemplatesUpsertCommandForTest, nil, `{"name":"test","template":"hello"}`, http.MethodPut, templatesPath + "/test"},
		{"notification policies set", alert.NewNotificationPoliciesSetCommandForTest, nil, `{"receiver":"test"}`, http.MethodPut, policiesPath},
	}
	flags := []struct {
		name       string
		arg        string
		wantHeader bool
		wantError  bool
	}{
		{"default", "", true, false},
		{"bare", "--allow-ui-updates", true, false},
		{"explicit true", "--allow-ui-updates=true", true, false},
		{"explicit false", "--allow-ui-updates=false", false, false},
		{"invalid", "--allow-ui-updates=invalid", false, true},
		{"empty", "--allow-ui-updates=", false, true},
	}

	for _, command := range commands {
		for _, flag := range flags {
			t.Run(command.name+"/"+flag.name, func(t *testing.T) {
				requests := 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requests++
					assert.Equal(t, command.method, r.Method)
					assert.Equal(t, command.path, r.URL.Path)
					if flag.wantHeader {
						assert.Equal(t, []string{"true"}, r.Header.Values("X-Disable-Provenance"))
					} else {
						assert.Empty(t, r.Header.Values("X-Disable-Provenance"))
					}
					w.WriteHeader(http.StatusAccepted)
				}))
				defer server.Close()

				cmd := command.newCmd(provisioningFlagLoader{host: server.URL})
				cmd.SilenceUsage = true
				cmd.SilenceErrors = true
				cmd.SetIn(strings.NewReader(command.body))
				cmd.SetOut(&bytes.Buffer{})
				cmd.SetErr(&bytes.Buffer{})
				args := append([]string{}, command.args...)
				args = append(args, "-f", "-")
				if command.name == "notification policies set" {
					args = append(args, "--force")
				}
				if flag.arg != "" {
					args = append(args, flag.arg)
				}
				cmd.SetArgs(args)
				err := cmd.Execute()
				if flag.wantError {
					require.Error(t, err)
					assert.Zero(t, requests)
					return
				}
				require.NoError(t, err)
				assert.Equal(t, 1, requests)
			})
		}
	}
}

func TestProvisioningEchoUsesRequestedProvenance(t *testing.T) {
	for _, allowUIUpdates := range []bool{true, false} {
		t.Run(map[bool]string{true: "editable", false: "provisioned"}[allowUIUpdates], func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusAccepted)
			}))
			defer server.Close()
			client, err := alert.NewClient(config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}})
			require.NoError(t, err)
			want := ""
			if !allowUIUpdates {
				want = "api"
			}
			for _, input := range []string{"", "api", "file"} {
				point, err := client.UpdateContactPoint(context.Background(), "cp", alert.ContactPoint{Name: "test", Provenance: input}, allowUIUpdates)
				require.NoError(t, err)
				assert.Equal(t, want, point.Provenance)
				template, err := client.UpsertTemplate(context.Background(), alert.NotificationTemplate{Name: "test", Provenance: input}, allowUIUpdates)
				require.NoError(t, err)
				assert.Equal(t, want, template.Provenance)
			}
		})
	}
}

func TestCreateContactPointReportsRequestedProvenance(t *testing.T) {
	tests := []struct {
		name           string
		allowUIUpdates []bool
		input          string
		response       string
		want           string
	}{
		{"default with absent input", nil, "", `{"uid":"cp","name":"test","type":"email","settings":{}}`, ""},
		{"false with absent input", []bool{false}, "", `{"uid":"cp","name":"test","type":"email","settings":{}}`, "api"},
		{"false with stale input", []bool{false}, "file", `{"uid":"cp","name":"test","type":"email","settings":{},"provenance":"file"}`, "api"},
		{"true with stale input", []bool{true}, "api", `{"uid":"cp","name":"test","type":"email","settings":{},"provenance":"api"}`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, contactPointsPath, r.URL.Path)
				if len(tt.allowUIUpdates) != 0 && !tt.allowUIUpdates[0] {
					assert.Empty(t, r.Header.Values("X-Disable-Provenance"))
				} else {
					assert.Equal(t, []string{"true"}, r.Header.Values("X-Disable-Provenance"))
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tt.response))
			}))
			defer server.Close()
			client, err := alert.NewClient(config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}})
			require.NoError(t, err)
			point, err := client.CreateContactPoint(context.Background(), alert.ContactPoint{Name: "test", Provenance: tt.input}, tt.allowUIUpdates...)
			require.NoError(t, err)
			assert.Equal(t, tt.want, point.Provenance)
			assert.Equal(t, "cp", point.UID)
		})
	}
}

func TestUpdateProvisionedContactPointRejectsUnlock(t *testing.T) {
	for _, tt := range []struct {
		name           string
		allowUIUpdates []bool
	}{
		{"default", nil},
		{"explicit true", []bool{true}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				assert.Equal(t, http.MethodPut, r.Method)
				assert.Equal(t, contactPointsPath+"/cp", r.URL.Path)
				assert.Equal(t, []string{"true"}, r.Header.Values("X-Disable-Provenance"))
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"message":"Resource with provenance status 'api' cannot be changed to 'none'"}`))
			}))
			defer server.Close()
			client, err := alert.NewClient(config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}})
			require.NoError(t, err)
			point, err := client.UpdateContactPoint(context.Background(), "cp", alert.ContactPoint{Name: "test", Provenance: "api"}, tt.allowUIUpdates...)
			require.ErrorContains(t, err, "cannot be changed to 'none'")
			assert.Nil(t, point)
			assert.Equal(t, 1, requests)
		})
	}
}
