package kg_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/providers/kg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseAlertLabelSet(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    map[string]string
		wantErr bool
	}{
		{
			name: "basic pairs",
			raw:  "alertname=ErrorRatioBreach,job=cart",
			want: map[string]string{"alertname": "ErrorRatioBreach", "job": "cart"},
		},
		{
			name: "value contains equals (split on first)",
			raw:  "expr=rate(x[5m])>0.1,job=cart",
			want: map[string]string{"expr": "rate(x[5m])>0.1", "job": "cart"},
		},
		{
			name: "whitespace trimmed and empty pairs skipped",
			raw:  " alertname = Foo , , job=bar ",
			want: map[string]string{"alertname": "Foo", "job": "bar"},
		},
		{
			name:    "missing equals",
			raw:     "alertname",
			wantErr: true,
		},
		{
			name:    "empty key",
			raw:     "=value",
			wantErr: true,
		},
		{
			name:    "no pairs",
			raw:     ",,",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := kg.ParseAlertLabelSet(tt.raw)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseAlertmanagerLabels(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []map[string]string
		wantErr bool
	}{
		{
			name:  "alertmanager envelope",
			input: `{"alerts":[{"labels":{"alertname":"Foo","job":"cart"}},{"labels":{"alertname":"Bar"}}]}`,
			want: []map[string]string{
				{"alertname": "Foo", "job": "cart"},
				{"alertname": "Bar"},
			},
		},
		{
			name:  "bare array",
			input: `[{"labels":{"alertname":"Foo"}},{"labels":{"alertname":"Bar"}}]`,
			want: []map[string]string{
				{"alertname": "Foo"},
				{"alertname": "Bar"},
			},
		},
		{
			name:    "envelope with no labels",
			input:   `{"alerts":[{"labels":{}}]}`,
			wantErr: true,
		},
		{
			name:    "empty object",
			input:   `{}`,
			wantErr: true,
		},
		{
			name:    "garbage",
			input:   `not json`,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := kg.ParseAlertmanagerLabels([]byte(tt.input))
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// correlateHandler serves POST /v1/alert-inspection with the given response,
// recording the decoded request so tests can assert the mapping.
func correlateHandler(t *testing.T, resp kg.AlertInspectionResponse, gotReq *kg.AlertInspectionRequest) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "alert-inspection") {
			if gotReq != nil {
				assert.NoError(t, json.NewDecoder(r.Body).Decode(gotReq))
			}
			writeJSON(w, resp)
			return
		}
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}
}

func correlateResponse(entities ...kg.GraphEntity) kg.AlertInspectionResponse {
	return kg.AlertInspectionResponse{
		Type: "graph",
		Data: kg.AlertInspectionData{Entities: entities},
	}
}

func TestCorrelate_AlertLabels_MapsRequest(t *testing.T) {
	var gotReq kg.AlertInspectionRequest
	resp := correlateResponse(kg.GraphEntity{
		Type:                 "Service",
		Name:                 "ad",
		Scope:                map[string]string{"env": "prod", "namespace": "otel-demo"},
		ConnectedEntityTypes: map[string]int{"Pod": 1, "Service": 4},
	})
	server := httptest.NewServer(correlateHandler(t, resp, &gotReq))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	cmd := kg.NewCorrelateCommand(writeLoaderFor(server))
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"--alert-labels", "alertname=ErrorRatioBreach,job=cart", "--since", "1h", "-o", "table"})
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	require.NoError(t, cmd.Execute())

	// Request mapping: one label set, time window populated.
	require.Len(t, gotReq.AlertLabels, 1)
	assert.Equal(t, "ErrorRatioBreach", gotReq.AlertLabels[0]["alertname"])
	assert.Equal(t, "cart", gotReq.AlertLabels[0]["job"])
	require.NotNil(t, gotReq.TimeCriteria)
	assert.NotZero(t, gotReq.TimeCriteria.Start)
	assert.NotZero(t, gotReq.TimeCriteria.End)
	assert.Empty(t, gotReq.Query, "v1 must not send a query")

	// Table output shows entity + connected impact counts.
	out := stdout.String()
	assert.Contains(t, out, "Service")
	assert.Contains(t, out, "ad")
	assert.Contains(t, out, "Pod=1")
	assert.Contains(t, out, "Service=4")
}

func TestCorrelate_FileStdin_Alertmanager(t *testing.T) {
	var gotReq kg.AlertInspectionRequest
	server := httptest.NewServer(correlateHandler(t, correlateResponse(kg.GraphEntity{Type: "Service", Name: "ad"}), &gotReq))
	defer server.Close()

	cmd := kg.NewCorrelateCommand(writeLoaderFor(server))
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"-f", "-", "--since", "1h"})
	cmd.SetIn(bytes.NewBufferString(`{"alerts":[{"labels":{"alertname":"Foo","job":"cart"}},{"labels":{"alertname":"Bar"}}]}`))
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	require.NoError(t, cmd.Execute())
	require.Len(t, gotReq.AlertLabels, 2)
	assert.Equal(t, "Foo", gotReq.AlertLabels[0]["alertname"])
	assert.Equal(t, "Bar", gotReq.AlertLabels[1]["alertname"])
}

func TestCorrelate_EmptyResult_ExitsZeroWithNotice(t *testing.T) {
	server := httptest.NewServer(correlateHandler(t, correlateResponse(), nil))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	cmd := kg.NewCorrelateCommand(writeLoaderFor(server))
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"--alert-labels", "alertname=Nope", "--since", "1h"})
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	require.NoError(t, cmd.Execute(), "empty correlation must exit 0")
	assert.Contains(t, stderr.String(), "no entities correlated")
}

func TestCorrelate_JSONOutput(t *testing.T) {
	resp := correlateResponse(kg.GraphEntity{Type: "Service", Name: "ad", Scope: map[string]string{"env": "prod"}})
	server := httptest.NewServer(correlateHandler(t, resp, nil))
	defer server.Close()

	var stdout bytes.Buffer
	cmd := kg.NewCorrelateCommand(writeLoaderFor(server))
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"--alert-labels", "alertname=Foo", "--since", "1h", "-o", "json"})
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})

	require.NoError(t, cmd.Execute())
	// Output is a bare entities array (parity with 'entities list'), not the
	// full graph envelope.
	assert.NotContains(t, stdout.String(), "timeCriteria", "envelope must not be emitted")
	var decoded []kg.GraphEntity
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &decoded))
	require.Len(t, decoded, 1)
	assert.Equal(t, "ad", decoded[0].Name)
}

func TestCorrelate_NoInput_Errors(t *testing.T) {
	server := httptest.NewServer(correlateHandler(t, correlateResponse(), nil))
	defer server.Close()

	cmd := kg.NewCorrelateCommand(writeLoaderFor(server))
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"--since", "1h"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no alerts provided")
}
