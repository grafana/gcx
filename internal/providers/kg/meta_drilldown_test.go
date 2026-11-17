package kg_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/providers/kg"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	logConfigPath     = "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v2/config/log"
	traceConfigPath   = "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v2/config/trace"
	profileConfigPath = "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v2/config/profile"

	logConfigsBody     = `{"logDrilldownConfigs":[{"name":"cfg-a","match":[],"entityPropertyToLogLabelMapping":{"pod":"pod"}}]}`
	traceConfigsBody   = `{"traceDrilldownConfigs":[{"name":"cfg-a","match":[],"entityPropertyToTraceLabelMapping":{"service":"service.name"}}]}`
	profileConfigsBody = `{"profileDrilldownConfigs":[{"name":"cfg-a","match":[],"entityPropertyToProfileLabelMapping":{"service":"service_name"}}]}`
)

// configAnswer is one way the drill-down config endpoints can answer.
type configAnswer func(t *testing.T, w http.ResponseWriter, body string)

func answerBody(status int, body string) configAnswer {
	return func(_ *testing.T, w http.ResponseWriter, _ string) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

// answerConfigs answers 200 with the endpoint's own configs document.
func answerConfigs() configAnswer {
	return func(_ *testing.T, w http.ResponseWriter, body string) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}
}

// answerTruncated promises more bytes than it sends, then closes the
// connection: the body ends early.
func answerTruncated() configAnswer {
	return func(t *testing.T, w http.ResponseWriter, body string) {
		t.Helper()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", "4096")
		_, _ = w.Write([]byte(body[:10]))
		hj, ok := w.(http.Hijacker)
		require.True(t, ok)
		conn, _, err := hj.Hijack()
		require.NoError(t, err)
		_ = conn.Close()
	}
}

// answerHangUp closes the connection without sending a response.
func answerHangUp() configAnswer {
	return func(t *testing.T, w http.ResponseWriter, _ string) {
		t.Helper()
		hj, ok := w.(http.Hijacker)
		require.True(t, ok)
		conn, _, err := hj.Hijack()
		require.NoError(t, err)
		_ = conn.Close()
	}
}

func configServer(t *testing.T, answer configAnswer) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case logConfigPath:
			answer(t, w, logConfigsBody)
		case traceConfigPath:
			answer(t, w, traceConfigsBody)
		case profileConfigPath:
			answer(t, w, profileConfigsBody)
		default:
			// kg meta all also reads the schema and scopes.
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func TestFetchDrillDownConfigs(t *testing.T) {
	tests := []struct {
		name      string
		answer    configAnswer
		wantCount int
		wantErr   string
	}{
		{name: "configs document", answer: answerConfigs(), wantCount: 1},
		{name: "empty body is no configs, not an error", answer: answerBody(http.StatusOK, "")},
		{name: "whitespace-only body is no configs", answer: answerBody(http.StatusOK, "\n")},
		{name: "no content is no configs", answer: answerBody(http.StatusNoContent, "")},
		{name: "document with an empty list", answer: answerBody(http.StatusOK, `{"logDrilldownConfigs":[],"traceDrilldownConfigs":[],"profileDrilldownConfigs":[]}`)},
		{name: "error status stays an error", answer: answerBody(http.StatusInternalServerError, `{"message":"down"}`), wantErr: "down"},
		{name: "malformed JSON stays an error", answer: answerBody(http.StatusOK, `{"logDrilldownConfigs":[`), wantErr: "EOF"},
		{name: "body cut short stays an error", answer: answerTruncated(), wantErr: "read response"},
		{name: "connection closed before a response stays an error", answer: answerHangUp(), wantErr: "execute request"},
	}
	fetchers := []struct {
		name  string
		fetch func(context.Context, *kg.Client) (int, error)
	}{
		{"logs", func(ctx context.Context, c *kg.Client) (int, error) {
			r, err := c.FetchLogConfigs(ctx)
			return len(r.LogDrilldownConfigs), err
		}},
		{"traces", func(ctx context.Context, c *kg.Client) (int, error) {
			r, err := c.FetchTraceConfigs(ctx)
			return len(r.TraceDrilldownConfigs), err
		}},
		{"profiles", func(ctx context.Context, c *kg.Client) (int, error) {
			r, err := c.FetchProfileConfigs(ctx)
			return len(r.ProfileDrilldownConfigs), err
		}},
	}
	for _, tt := range tests {
		for _, f := range fetchers {
			t.Run(tt.name+"/"+f.name, func(t *testing.T) {
				client := newTestClient(t, configServer(t, tt.answer))
				n, err := f.fetch(t.Context(), client)
				if tt.wantErr != "" {
					require.Error(t, err)
					assert.Contains(t, err.Error(), tt.wantErr)
					return
				}
				require.NoError(t, err)
				assert.Equal(t, tt.wantCount, n)
			})
		}
	}
}

func TestMetaDrillDown_NoConfigsNote(t *testing.T) {
	forceNoColor(t)
	pinHumanMode(t)

	commands := []struct {
		section string
		field   string
		build   func(kg.RESTConfigLoader) *cobra.Command
	}{
		{"logs", "logConfigs", kg.NewDescribeLogsCommand},
		{"traces", "traceConfigs", kg.NewDescribeTracesCommand},
		{"profiles", "profileConfigs", kg.NewDescribeProfilesCommand},
	}
	tests := []struct {
		name     string
		answer   configAnswer
		args     []string
		wantNote bool
		// wantText is a substring of human text output ("" = JSON output).
		wantText string
	}{
		{name: "configs: no note", answer: answerConfigs(), args: []string{"-o", "json"}},
		{name: "empty body: note, exit 0", answer: answerBody(http.StatusOK, ""), args: []string{"-o", "json"}, wantNote: true},
		{name: "empty list: note, exit 0", answer: answerBody(http.StatusOK, `{"logDrilldownConfigs":[],"traceDrilldownConfigs":[],"profileDrilldownConfigs":[]}`), args: []string{"-o", "json"}, wantNote: true},
		{name: "empty body, text output: note printed", answer: answerBody(http.StatusOK, ""), wantNote: true, wantText: "drill-down configs for this stack"},
	}
	for _, tt := range tests {
		for _, c := range commands {
			t.Run(tt.name+"/"+c.section, func(t *testing.T) {
				server := configServer(t, tt.answer)
				stdout, _, err := runKgCommand(t,
					func() *cobra.Command { return c.build(writeLoaderFor(server)) }, tt.args, "")
				require.NoError(t, err)
				if tt.wantText != "" {
					assert.Contains(t, stdout, "Note ("+c.section+"): ")
					assert.Contains(t, stdout, tt.wantText)
					assert.NotContains(t, stdout, "No metadata requested.")
					return
				}
				doc, ok := decodeSingleJSON(t, []byte(stdout)).(map[string]any)
				require.True(t, ok)
				notes, hasNotes := doc["notes"].(map[string]any)
				if !tt.wantNote {
					assert.False(t, hasNotes, "a section with configs carries no note")
					assert.Contains(t, doc, c.field)
					return
				}
				require.True(t, hasNotes, "an empty section must say so on stdout")
				assert.Contains(t, notes[c.section], "returned no")
				assert.NotContains(t, doc, c.field)
			})
		}
	}

	t.Run("meta all: empty sections are notes, not errors", func(t *testing.T) {
		server := configServer(t, answerBody(http.StatusOK, ""))
		stdout, _, err := runKgCommand(t,
			func() *cobra.Command { return kg.NewDescribeAllCommand(writeLoaderFor(server)) },
			[]string{"-o", "json"}, "")
		require.NoError(t, err)
		doc, ok := decodeSingleJSON(t, []byte(stdout)).(map[string]any)
		require.True(t, ok)
		notes, ok := doc["notes"].(map[string]any)
		require.True(t, ok)
		errs, _ := doc["errors"].(map[string]any)
		for _, section := range []string{"logs", "traces", "profiles"} {
			assert.Contains(t, notes, section)
			assert.NotContains(t, errs, section)
		}
	})

	t.Run("meta all: sections with configs carry no notes", func(t *testing.T) {
		server := configServer(t, answerConfigs())
		stdout, _, err := runKgCommand(t,
			func() *cobra.Command { return kg.NewDescribeAllCommand(writeLoaderFor(server)) },
			[]string{"-o", "json"}, "")
		require.NoError(t, err)
		doc, ok := decodeSingleJSON(t, []byte(stdout)).(map[string]any)
		require.True(t, ok)
		assert.NotContains(t, doc, "notes")
		for _, field := range []string{"logConfigs", "traceConfigs", "profileConfigs"} {
			assert.Contains(t, doc, field)
		}
	})
}

func TestMetaDrillDown_WindowFlagError(t *testing.T) {
	commands := []struct {
		use    string
		signal string
		build  func(kg.RESTConfigLoader) *cobra.Command
	}{
		{"logs", "log", kg.NewDescribeLogsCommand},
		{"traces", "trace", kg.NewDescribeTracesCommand},
		{"profiles", "profile", kg.NewDescribeProfilesCommand},
	}
	tests := []struct {
		name string
		args []string
		// wantReason is true when the error must say why the flag is refused.
		wantReason bool
	}{
		{name: "--from", args: []string{"--from", "2026-01-01T00:00:00Z"}, wantReason: true},
		{name: "--to=value", args: []string{"--to=2026-01-01T00:00:00Z"}, wantReason: true},
		{name: "--since", args: []string{"--since", "1h"}, wantReason: true},
		{name: "other unknown flag", args: []string{"--frm", "x"}},
		{name: "other unknown shorthand", args: []string{"-z"}},
	}
	for _, tt := range tests {
		for _, c := range commands {
			t.Run(tt.name+"/"+c.use, func(t *testing.T) {
				server := configServer(t, answerConfigs())
				// The parent stands in for the root command's handler, which
				// every other flag error must still reach.
				parent := &cobra.Command{Use: "meta"}
				parent.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
					return fmt.Errorf("parent handler: %w", err)
				})
				child := c.build(writeLoaderFor(server))
				parent.AddCommand(child)
				_, _, err := runKgCommand(t, func() *cobra.Command { return parent },
					append([]string{c.use}, tt.args...), "")
				require.Error(t, err)

				usage, isUsage := errors.AsType[*gcxerrors.UsageError](err)
				if !tt.wantReason {
					assert.Contains(t, err.Error(), "parent handler: ")
					assert.NotContains(t, err.Error(), "takes no --from")
					return
				}
				require.True(t, isUsage, "a window flag must be a usage error (exit 2)")
				assert.Contains(t, usage.Message, "unknown flag: "+tt.args[0][:strings.IndexAny(tt.args[0]+"=", "=")])
				assert.Contains(t, usage.Message, "meta "+c.use+" takes no --from, --to or --since")
				assert.Contains(t, usage.Message, "reads the "+c.signal+" drill-down configs as currently stored")
				assert.Equal(t, []string{"Run 'meta " + c.use + "' without --from, --to or --since"}, usage.Suggestions)
				assert.NotContains(t, err.Error(), "parent handler: ")
			})
		}
	}
}
