package ml

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/config"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/rest"
)

type testLoader struct {
	cfg   config.NamespacedRESTConfig
	calls int
}

func (l *testLoader) LoadGrafanaConfig(context.Context) (config.NamespacedRESTConfig, error) {
	l.calls++
	return l.cfg, nil
}

func TestCommandUsesPublicGrafanaURLWithProxyTransport(t *testing.T) {
	for _, verb := range []string{"create", "update"} {
		t.Run(verb, func(t *testing.T) {
			var calls int
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				assert.Equal(t, http.MethodPost, r.Method)
				path := basePath + "jobs"
				if verb == "update" {
					path += "/one"
				}
				assert.Equal(t, path, r.URL.Path)
				var payload map[string]any
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
				assert.Equal(t, "https://stack.example", payload["grafanaUrl"])
				assert.NotContains(t, payload, "id")
				fmt.Fprint(w, `{"status":"success","data":{"id":"one","name":"forecast"}}`)
			}))
			defer srv.Close()
			loader := &testLoader{cfg: config.NamespacedRESTConfig{Config: rest.Config{Host: srv.URL}, GrafanaURL: "https://stack.example", Namespace: "default"}}
			cmd := resourceCommands(loader, "jobs", "forecast job", "forecast jobs", jobs())
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetIn(strings.NewReader(`{"apiVersion":"ml.ext.grafana.app/v1alpha1","kind":"Job","metadata":{"name":"source"},"spec":{"name":"forecast","metric":"requests","grafanaUrl":"https://old.example"}}`))
			args := []string{verb}
			if verb == "update" {
				args = append(args, "one")
			}
			args = append(args, "-f", "-", "-o", "json")
			cmd.SetArgs(args)
			require.NoError(t, cmd.Execute())
			require.Equal(t, 1, calls)
			require.Equal(t, 1, loader.calls)
			require.Contains(t, out.String(), `"name": "one"`)
		})
	}
}

func TestCommandsValidateBeforeConfigLoad(t *testing.T) {
	for _, args := range [][]string{{"get"}, {"list", "extra"}, {"list", "--limit", "-1"}, {"create"}, {"create", "--filename="}, {"update", "one"}, {"get", "../escape"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			loader := &testLoader{}
			cmd := resourceCommands(loader, "jobs", "forecast job", "forecast jobs", jobs())
			cmd.SetArgs(args)
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			require.Error(t, cmd.Execute())
			require.Zero(t, loader.calls)
		})
	}
}

func TestDryRunDoesNotLoadConfig(t *testing.T) {
	for _, args := range [][]string{{"create", "-f", "-", "--dry-run"}, {"update", "one", "-f", "-", "--dry-run"}, {"delete", "one", "--dry-run"}} {
		t.Run(args[0], func(t *testing.T) {
			loader := &testLoader{}
			cmd := resourceCommands(loader, "jobs", "forecast job", "forecast jobs", jobs())
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetIn(strings.NewReader(`{"id":"stale-body-id","name":"forecast"}`))
			cmd.SetArgs(append(args, "-o", "json"))
			require.NoError(t, cmd.Execute())
			require.Zero(t, loader.calls)
			var result cmdio.SingleMutation
			require.NoError(t, json.Unmarshal(out.Bytes(), &result))
			require.True(t, result.DryRun)
			want := "one"
			if args[0] == "create" {
				want = "forecast"
			}
			require.Equal(t, want, result.Target.Name)
		})
	}
}

func TestResourceSchemaAndCleanManifest(t *testing.T) {
	for _, r := range []adapter.Declaration{jobs(), holidays(), outliers()} {
		p := adapter.NewProvider("test", "test", nil, r)
		reg := p.TypedRegistrations()[0]
		t.Run(reg.GVK.Kind, func(t *testing.T) {
			require.NotNil(t, reg.Schema)
			require.NotEmpty(t, reg.Example)
			require.NotContains(t, string(reg.Example), "grafanaApiKey")
			require.NotContains(t, string(reg.Example), "grafanaUrl")
			require.NotContains(t, string(reg.Example), "datasourceId")
		})
	}
}

func TestParentAlertCommands(t *testing.T) {
	for _, parent := range []string{"jobs", "outliers"} {
		for _, verb := range []string{"list", "get", "create", "update", "delete"} {
			t.Run(parent+"/"+verb, func(t *testing.T) {
				hits := 0
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					hits++
					path := basePath + parent + "/parent-id/alerts"
					if verb == "get" || verb == "update" || verb == "delete" {
						path += "/alert-id"
					}
					assert.Equal(t, path, r.URL.Path)
					method := http.MethodGet
					if verb == "create" || verb == "update" {
						method = http.MethodPost
					}
					if verb == "delete" {
						method = http.MethodDelete
					}
					assert.Equal(t, method, r.Method)
					if method == http.MethodPost {
						var body map[string]any
						assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
						assert.Equal(t, "Anomaly", body["title"])
						assert.Equal(t, "5m", body["for"])
						assert.NotContains(t, body, "id")
						assert.NotContains(t, body, "grafanaUrl")
					}
					if verb == "delete" {
						w.WriteHeader(http.StatusNoContent)
						return
					}
					if verb == "list" {
						fmt.Fprint(w, `{"status":"success","data":[]}`)
					} else {
						fmt.Fprint(w, `{"status":"success","data":{"id":"alert-id","title":"Anomaly","for":"5m","window":"1h"}}`)
					}
				}))
				defer srv.Close()
				loader := &testLoader{cfg: config.NamespacedRESTConfig{Config: rest.Config{Host: srv.URL}}}
				group := &cobra.Command{Use: parent}
				addAlertCommands(group, loader, parent)
				leaf := verb + "-alert"
				if verb == "list" {
					leaf = "list-alerts"
				}
				args := []string{leaf, "parent-id"}
				if verb == "get" || verb == "update" || verb == "delete" {
					args = append(args, "alert-id")
				}
				if verb == "create" || verb == "update" {
					args = append(args, "-f", "-")
				}
				if verb == "delete" {
					args = append(args, "--force")
				}
				group.SetArgs(append(args, "-o", "json"))
				var out bytes.Buffer
				group.SetOut(&out)
				group.SetErr(&out)
				group.SetIn(strings.NewReader(`{"id":"source-id","title":"Anomaly","for":"5m","window":"1h"}`))
				require.NoError(t, group.Execute())
				require.Equal(t, 1, hits)
				require.Equal(t, 1, loader.calls)
				if verb == "list" {
					require.JSONEq(t, `[]`, out.String())
				}
			})
		}
	}
}

func TestReadCommandsMatchAdapterJSON(t *testing.T) {
	t.Run("jobs", func(t *testing.T) { checkReadParity(t, jobs(), "jobs") })
	t.Run("holidays", func(t *testing.T) { checkReadParity(t, holidays(), "holidays") })
	t.Run("outliers", func(t *testing.T) { checkReadParity(t, outliers(), "outlier-detectors") })
}

func checkReadParity[T displayResource](t *testing.T, r adapter.Resource[T], path string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		item := `{"id":"one","name":"forecast","grafanaUrl":"https://server.example","grafanaApiKey":"secret","createdBy":"server"}`
		if strings.HasSuffix(req.URL.Path, "/one") {
			fmt.Fprint(w, `{"status":"success","data":`+item+`}`)
		} else {
			fmt.Fprint(w, `{"status":"success","data":[`+item+`]}`)
		}
	}))
	defer srv.Close()
	for _, verb := range []string{"list", "get"} {
		t.Run(verb, func(t *testing.T) {
			loader := &testLoader{cfg: config.NamespacedRESTConfig{Config: rest.Config{Host: srv.URL}, Namespace: "default"}}
			cmd := resourceCommands(loader, path, "resource", path, r)
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			args := []string{verb}
			if verb == "get" {
				args = append(args, "one")
			}
			cmd.SetArgs(append(args, "-o", "json"))
			require.NoError(t, cmd.Execute())
			impl, err := r.NewClient(t.Context(), adapter.ClientDeps{HTTP: srv.Client(), BaseURL: srv.URL})
			require.NoError(t, err)
			crud := r.TypedCRUD(impl, "default")
			var want any
			if verb == "get" {
				want, err = crud.AsAdapter().Get(t.Context(), "one", metav1.GetOptions{})
			} else {
				var list *unstructured.UnstructuredList
				list, err = crud.AsAdapter().List(t.Context(), metav1.ListOptions{})
				if err == nil {
					want = list.Items
				}
			}
			require.NoError(t, err)
			data, err := json.Marshal(want)
			require.NoError(t, err)
			require.JSONEq(t, string(data), out.String())
			require.NotContains(t, out.String(), "grafanaUrl")
			require.NotContains(t, out.String(), "secret")
			require.NotContains(t, out.String(), "createdBy")
			tableCmd := resourceCommands(loader, path, "resource", path, r)
			out.Reset()
			tableCmd.SetOut(&out)
			tableCmd.SetErr(&out)
			tableCmd.SetArgs(append(args, "-o", "table"))
			require.NoError(t, tableCmd.Execute())
			require.Contains(t, out.String(), "one")
			require.Contains(t, out.String(), "forecast")
		})
	}
}

func TestAlertDryRunTargets(t *testing.T) {
	for _, verb := range []string{"create", "update", "delete"} {
		t.Run(verb, func(t *testing.T) {
			loader := &testLoader{}
			group := &cobra.Command{Use: "jobs"}
			addAlertCommands(group, loader, "jobs")
			args := []string{verb + "-alert", "parent"}
			if verb != "create" {
				args = append(args, "target-alert")
			}
			if verb != "delete" {
				args = append(args, "-f", "-")
			}
			args = append(args, "--dry-run", "-o", "json")
			var out bytes.Buffer
			group.SetOut(&out)
			group.SetErr(&out)
			group.SetIn(strings.NewReader(`{"id":"stale-body-id","title":"New alert"}`))
			group.SetArgs(args)
			require.NoError(t, group.Execute())
			require.Zero(t, loader.calls)
			var result cmdio.SingleMutation
			require.NoError(t, json.Unmarshal(out.Bytes(), &result))
			require.True(t, result.DryRun)
			want := "target-alert"
			if verb == "create" {
				want = "New alert"
			}
			require.Equal(t, want, result.Target.Name)
		})
	}
}

func TestMutationHumanDefault(t *testing.T) {
	mode := agent.IsAgentMode()
	agent.SetFlag(false)
	t.Cleanup(func() { agent.SetFlag(mode) })
	cmd := resourceCommands(&testLoader{}, "jobs", "forecast job", "forecast jobs", jobs())
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"delete", "one", "--dry-run"})
	require.NoError(t, cmd.Execute())
	require.Contains(t, out.String(), `Dry run: Job "one"`)
	require.NotContains(t, out.String(), `"schema_version"`)
}

func TestListLimitReportsTruncation(t *testing.T) {
	for _, alert := range []bool{false, true} {
		t.Run(strconv.FormatBool(alert), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprint(w, `{"status":"success","data":[{"id":"one","name":"first","title":"first"},{"id":"two","name":"second","title":"second"}]}`)
			}))
			defer srv.Close()
			loader := &testLoader{cfg: config.NamespacedRESTConfig{Config: rest.Config{Host: srv.URL}}}
			cmd := resourceCommands(loader, "jobs", "forecast job", "forecast jobs", jobs())
			args := []string{"list", "--limit", "1", "-o", "json"}
			if alert {
				cmd = &cobra.Command{Use: "jobs"}
				addAlertCommands(cmd, loader, "jobs")
				args = []string{"list-alerts", "parent", "--limit", "1", "-o", "json"}
			}
			var out, errOut bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&errOut)
			cmd.SetArgs(args)
			require.NoError(t, cmd.Execute())
			var items []any
			require.NoError(t, json.Unmarshal(out.Bytes(), &items))
			require.Len(t, items, 1)
			require.Contains(t, errOut.String(), "showing first 1 of 2")
			require.Contains(t, errOut.String(), "--limit 0")
		})
	}
}
