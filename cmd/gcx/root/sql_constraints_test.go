package root_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/root"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSQLDescribeConstraintsCommands(t *testing.T) {
	for _, ds := range []struct{ kind, plugin string }{{"mysql", "mysql"}, {"postgres", "grafana-postgresql-datasource"}} {
		for _, mode := range []string{"populated", "empty", "missing", "denied"} {
			for _, format := range []string{"table", "wide", "json", "yaml", "agents"} {
				t.Run(ds.kind+"/"+mode+"/"+format, func(t *testing.T) {
					testutils.SandboxConfigEnv(t)
					var queries []string
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						w.Header().Set("Content-Type", "application/json")
						switch {
						case r.URL.Path == "/api/datasources/uid/test-ds":
							_, err := fmt.Fprintf(w, `{"uid":"test-ds","type":%q}`, ds.plugin)
							assert.NoError(t, err)
						case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/query"):
							var body struct {
								Queries []struct {
									RawSQL string `json:"rawSql"`
								} `json:"queries"`
							}
							if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&body)) || !assert.Len(t, body.Queries, 1) {
								return
							}
							sql := body.Queries[0].RawSQL
							queries = append(queries, sql)
							if strings.Contains(sql, "information_schema.tables") {
								assert.Contains(t, sql, "table_schema = 'sales'")
								assert.Contains(t, sql, "table_name = 'orders'")
								if mode == "missing" {
									_, _ = fmt.Fprint(w, `{"results":{"A":{"frames":[]}}}`)
								} else {
									_, _ = fmt.Fprint(w, `{"results":{"A":{"frames":[{"schema":{"fields":[{"name":"found","type":"number"}]},"data":{"values":[[1]]}}]}}}`)
								}
								return
							}
							if mode == "denied" {
								w.WriteHeader(http.StatusForbidden)
								_, _ = fmt.Fprint(w, `{"message":"forbidden"}`)
								return
							}
							if mode == "empty" {
								_, _ = fmt.Fprint(w, `{"results":{"A":{"frames":[]}}}`)
								return
							}
							fields := []map[string]string{}
							for _, name := range []string{"constraint_name", "constraint_type", "ordinal_position", "column_name", "referenced_namespace", "referenced_table", "referenced_column", "on_delete", "on_update"} {
								typ := "string"
								if name == "ordinal_position" {
									typ = "number"
								}
								fields = append(fields, map[string]string{"name": name, "type": typ})
							}
							values := [][]any{{"fk_customer", "fk_customer"}, {"FOREIGN KEY", "FOREIGN KEY"}, {2, 1}, {"region_id", "customer_id"}, {"crm", "crm"}, {"customers", "customers"}, {"region", "id"}, {"CASCADE", "CASCADE"}, {"NO ACTION", "NO ACTION"}}
							assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"results": map[string]any{"A": map[string]any{"frames": []any{map[string]any{"schema": map[string]any{"fields": fields}, "data": map[string]any{"values": values}}}}}}))
						default:
							http.NotFound(w, r)
						}
					}))
					defer server.Close()
					config := testutils.CreateTempFile(t, fmt.Sprintf("version: 1\nstacks:\n  test:\n    grafana:\n      server: %s\n      token: test-token\n      org-id: 1\ncontexts:\n  test:\n    stack: test\ncurrent-context: test\n", server.URL))
					cmd := root.Command("test")
					var stdout, stderr bytes.Buffer
					cmd.SetOut(&stdout)
					cmd.SetErr(&stderr)
					cmd.SetArgs([]string{"datasources", ds.kind, "describe-constraints", "sales.orders", "-d", "test-ds", "--config", config, "-o", format})
					err := cmd.Execute()
					if mode == "missing" {
						require.ErrorContains(t, err, "not found")
						require.Len(t, queries, 1)
						return
					}
					if mode == "denied" {
						require.ErrorContains(t, err, "query constraints failed")
						assert.Empty(t, stdout.String())
						return
					}
					require.NoError(t, err)
					require.Len(t, queries, 2, "every output format must fetch the same metadata")
					if format == "json" || format == "agents" {
						var result struct {
							Table struct {
								Namespace string `json:"namespace"`
								Name      string `json:"name"`
							} `json:"table"`
							Constraints []struct {
								Columns         []string `json:"columns"`
								ReferencedTable struct {
									Columns []string `json:"columns"`
								} `json:"referencedTable"`
							} `json:"constraints"`
						}
						require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
						assert.Equal(t, "sales", result.Table.Namespace)
						assert.Equal(t, "orders", result.Table.Name)
						if mode == "empty" {
							assert.NotNil(t, result.Constraints)
							assert.Empty(t, result.Constraints)
						} else {
							require.Len(t, result.Constraints, 1)
							assert.Equal(t, []string{"customer_id", "region_id"}, result.Constraints[0].Columns)
							assert.Equal(t, []string{"id", "region"}, result.Constraints[0].ReferencedTable.Columns)
						}
					} else if mode == "populated" {
						assert.Contains(t, stdout.String(), "fk_customer")
						assert.Contains(t, stdout.String(), "CASCADE")
					}
				})
			}
		}
	}
}

func TestSQLDescribeConstraintsValidation(t *testing.T) {
	for _, kind := range []string{"mysql", "postgres"} {
		for _, table := range []string{"orders", "sales.orders;DROP", "a.b.c"} {
			t.Run(kind+"/"+table, func(t *testing.T) {
				testutils.SandboxConfigEnv(t)
				cmd := root.Command("test")
				cmd.SetOut(&bytes.Buffer{})
				cmd.SetErr(&bytes.Buffer{})
				cmd.SetArgs([]string{"datasources", kind, "describe-constraints", table})
				var usage *gcxerrors.UsageError
				require.ErrorAs(t, cmd.Execute(), &usage)
			})
		}
	}
}
