package influxdb_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/query/influxdb"
	"github.com/grafana/gcx/internal/queryerror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func TestConvertGrafanaResponse(t *testing.T) {
	tests := []struct {
		name        string
		input       *influxdb.GrafanaQueryResponse
		wantColumns []string
		wantRows    [][]any
	}{
		{
			name: "empty results map",
			input: &influxdb.GrafanaQueryResponse{
				Results: map[string]influxdb.GrafanaResult{},
			},
			wantColumns: nil,
			wantRows:    nil,
		},
		{
			name: "result A with no frames",
			input: &influxdb.GrafanaQueryResponse{
				Results: map[string]influxdb.GrafanaResult{
					"A": {Frames: []influxdb.DataFrame{}},
				},
			},
			wantColumns: nil,
			wantRows:    nil,
		},
		{
			name: "result A with error still returns data",
			input: &influxdb.GrafanaQueryResponse{
				Results: map[string]influxdb.GrafanaResult{
					"A": {
						Error:       "partial error",
						ErrorSource: "downstream",
						Status:      400,
						Frames: []influxdb.DataFrame{
							{
								Schema: influxdb.DataFrameSchema{
									Fields: []influxdb.Field{
										{Name: "Time", Type: "time"},
										{Name: "Value", Type: "number"},
									},
								},
								Data: influxdb.DataFrameData{
									Values: [][]any{
										{float64(1000), float64(2000)},
										{float64(10.5), float64(20.3)},
									},
								},
							},
						},
					},
				},
			},
			wantColumns: []string{"Time", "Value"},
			wantRows: [][]any{
				{float64(1000), float64(10.5)},
				{float64(2000), float64(20.3)},
			},
		},
		{
			name: "single frame with 2 columns and 3 rows",
			input: &influxdb.GrafanaQueryResponse{
				Results: map[string]influxdb.GrafanaResult{
					"A": {
						Frames: []influxdb.DataFrame{
							{
								Schema: influxdb.DataFrameSchema{
									Fields: []influxdb.Field{
										{Name: "Time", Type: "time"},
										{Name: "Value", Type: "number"},
									},
								},
								Data: influxdb.DataFrameData{
									Values: [][]any{
										{float64(1000), float64(2000), float64(3000)},
										{float64(10.5), float64(20.3), float64(30.1)},
									},
								},
							},
						},
					},
				},
			},
			wantColumns: []string{"Time", "Value"},
			wantRows: [][]any{
				{float64(1000), float64(10.5)},
				{float64(2000), float64(20.3)},
				{float64(3000), float64(30.1)},
			},
		},
		{
			name: "single frame with 3 columns and 2 rows",
			input: &influxdb.GrafanaQueryResponse{
				Results: map[string]influxdb.GrafanaResult{
					"A": {
						Frames: []influxdb.DataFrame{
							{
								Schema: influxdb.DataFrameSchema{
									Fields: []influxdb.Field{
										{Name: "Time", Type: "time"},
										{Name: "cpu", Type: "number"},
										{Name: "host", Type: "string"},
									},
								},
								Data: influxdb.DataFrameData{
									Values: [][]any{
										{float64(1000), float64(2000)},
										{float64(55.2), float64(63.8)},
										{"server-a", "server-b"},
									},
								},
							},
						},
					},
				},
			},
			wantColumns: []string{"Time", "cpu", "host"},
			wantRows: [][]any{
				{float64(1000), float64(55.2), "server-a"},
				{float64(2000), float64(63.8), "server-b"},
			},
		},
		{
			name: "result A frame with empty values",
			input: &influxdb.GrafanaQueryResponse{
				Results: map[string]influxdb.GrafanaResult{
					"A": {
						Frames: []influxdb.DataFrame{
							{
								Schema: influxdb.DataFrameSchema{
									Fields: []influxdb.Field{
										{Name: "Time", Type: "time"},
										{Name: "Value", Type: "number"},
									},
								},
								Data: influxdb.DataFrameData{
									Values: [][]any{
										{},
										{},
									},
								},
							},
						},
					},
				},
			},
			wantColumns: []string{"Time", "Value"},
			wantRows:    nil,
		},
		{
			name: "multiple frames rows are combined using first frame schema",
			input: &influxdb.GrafanaQueryResponse{
				Results: map[string]influxdb.GrafanaResult{
					"A": {
						Frames: []influxdb.DataFrame{
							{
								Schema: influxdb.DataFrameSchema{
									Fields: []influxdb.Field{
										{Name: "Time", Type: "time"},
										{Name: "_value", Type: "number"},
									},
								},
								Data: influxdb.DataFrameData{
									Values: [][]any{
										{float64(1000)},
										{float64(42.0)},
									},
								},
							},
							{
								Schema: influxdb.DataFrameSchema{
									Fields: []influxdb.Field{
										{Name: "Time", Type: "time"},
										{Name: "_value", Type: "number"},
									},
								},
								Data: influxdb.DataFrameData{
									Values: [][]any{
										{float64(2000)},
										{float64(99.0)},
									},
								},
							},
						},
					},
				},
			},
			wantColumns: []string{"Time", "_value"},
			wantRows: [][]any{
				{float64(1000), float64(42.0)},
				{float64(2000), float64(99.0)},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := influxdb.ConvertGrafanaResponse(tt.input)
			require.NotNil(t, got)

			assert.Equal(t, tt.wantColumns, got.Columns)

			if tt.wantRows == nil {
				assert.Empty(t, got.Rows)
			} else {
				require.Len(t, got.Rows, len(tt.wantRows))
				for i, wantRow := range tt.wantRows {
					assert.Equal(t, wantRow, got.Rows[i], "row %d mismatch", i)
				}
			}
		})
	}
}

func TestConvertGrafanaResponse_Labels(t *testing.T) {
	tests := []struct {
		name        string
		input       *influxdb.GrafanaQueryResponse
		wantColumns []string
		wantRows    [][]any
	}{
		{
			name: "multiple frames with labels adds label columns",
			input: &influxdb.GrafanaQueryResponse{
				Results: map[string]influxdb.GrafanaResult{
					"A": {
						Frames: []influxdb.DataFrame{
							{
								Schema: influxdb.DataFrameSchema{
									Fields: []influxdb.Field{
										{Name: "Time", Type: "time"},
										{Name: "_value", Type: "number", Labels: map[string]string{"host": "server-a"}},
									},
								},
								Data: influxdb.DataFrameData{
									Values: [][]any{
										{float64(1000), float64(2000)},
										{float64(10.5), float64(20.3)},
									},
								},
							},
							{
								Schema: influxdb.DataFrameSchema{
									Fields: []influxdb.Field{
										{Name: "Time", Type: "time"},
										{Name: "_value", Type: "number", Labels: map[string]string{"host": "server-b"}},
									},
								},
								Data: influxdb.DataFrameData{
									Values: [][]any{
										{float64(3000), float64(4000)},
										{float64(30.1), float64(40.2)},
									},
								},
							},
						},
					},
				},
			},
			wantColumns: []string{"Time", "_value", "host"},
			wantRows: [][]any{
				{float64(1000), float64(10.5), "server-a"},
				{float64(2000), float64(20.3), "server-a"},
				{float64(3000), float64(30.1), "server-b"},
				{float64(4000), float64(40.2), "server-b"},
			},
		},
		{
			name: "frames with different label key sets uses union",
			input: &influxdb.GrafanaQueryResponse{
				Results: map[string]influxdb.GrafanaResult{
					"A": {
						Frames: []influxdb.DataFrame{
							{
								Schema: influxdb.DataFrameSchema{
									Fields: []influxdb.Field{
										{Name: "Time", Type: "time"},
										{Name: "_value", Type: "number", Labels: map[string]string{"host": "server-a", "region": "us-east"}},
									},
								},
								Data: influxdb.DataFrameData{
									Values: [][]any{
										{float64(1000)},
										{float64(10.0)},
									},
								},
							},
							{
								Schema: influxdb.DataFrameSchema{
									Fields: []influxdb.Field{
										{Name: "Time", Type: "time"},
										{Name: "_value", Type: "number", Labels: map[string]string{"host": "server-b", "env": "prod"}},
									},
								},
								Data: influxdb.DataFrameData{
									Values: [][]any{
										{float64(2000)},
										{float64(20.0)},
									},
								},
							},
						},
					},
				},
			},
			wantColumns: []string{"Time", "_value", "env", "host", "region"},
			wantRows: [][]any{
				{float64(1000), float64(10.0), "", "server-a", "us-east"},
				{float64(2000), float64(20.0), "prod", "server-b", ""},
			},
		},
		{
			name: "empty frame with labels still contributes label columns",
			input: &influxdb.GrafanaQueryResponse{
				Results: map[string]influxdb.GrafanaResult{
					"A": {
						Frames: []influxdb.DataFrame{
							{
								Schema: influxdb.DataFrameSchema{
									Fields: []influxdb.Field{
										{Name: "Time", Type: "time"},
										{Name: "_value", Type: "number", Labels: map[string]string{"host": "server-a"}},
									},
								},
								Data: influxdb.DataFrameData{
									Values: [][]any{{}, {}},
								},
							},
							{
								Schema: influxdb.DataFrameSchema{
									Fields: []influxdb.Field{
										{Name: "Time", Type: "time"},
										{Name: "_value", Type: "number", Labels: map[string]string{"host": "server-b"}},
									},
								},
								Data: influxdb.DataFrameData{
									Values: [][]any{
										{float64(3000)},
										{float64(30.0)},
									},
								},
							},
						},
					},
				},
			},
			wantColumns: []string{"Time", "_value", "host"},
			wantRows: [][]any{
				{float64(3000), float64(30.0), "server-b"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := influxdb.ConvertGrafanaResponse(tt.input)
			require.NotNil(t, got)

			assert.Equal(t, tt.wantColumns, got.Columns)

			require.Len(t, got.Rows, len(tt.wantRows))
			for i, wantRow := range tt.wantRows {
				assert.Equal(t, wantRow, got.Rows[i], "row %d mismatch", i)
			}
		})
	}
}

func TestExtractFieldKeys_AggregatesAllFrames(t *testing.T) {
	// SHOW FIELD KEYS without --measurement returns one frame per measurement.
	resp := &influxdb.GrafanaQueryResponse{
		Results: map[string]influxdb.GrafanaResult{
			"A": {
				Frames: []influxdb.DataFrame{
					{
						Schema: influxdb.DataFrameSchema{Fields: []influxdb.Field{{Name: "fieldKey"}, {Name: "fieldType"}}},
						Data:   influxdb.DataFrameData{Values: [][]any{{"cpu_usage", "load"}, {"float", "float"}}},
					},
					{
						Schema: influxdb.DataFrameSchema{Fields: []influxdb.Field{{Name: "fieldKey"}, {Name: "fieldType"}}},
						Data:   influxdb.DataFrameData{Values: [][]any{{"free", "used"}, {"integer", "integer"}}},
					},
				},
			},
		},
	}

	got := influxdb.ExtractFieldKeys(resp)
	require.NotNil(t, got)
	require.Len(t, got.Fields, 4)
	assert.Equal(t, "cpu_usage", got.Fields[0].FieldKey)
	assert.Equal(t, "float", got.Fields[0].FieldType)
	assert.Equal(t, "load", got.Fields[1].FieldKey)
	assert.Equal(t, "float", got.Fields[1].FieldType)
	assert.Equal(t, "free", got.Fields[2].FieldKey)
	assert.Equal(t, "integer", got.Fields[2].FieldType)
	assert.Equal(t, "used", got.Fields[3].FieldKey)
	assert.Equal(t, "integer", got.Fields[3].FieldType)
}

func TestExtractFieldKeys_SkipsFramesWithFewerThanTwoColumns(t *testing.T) {
	resp := &influxdb.GrafanaQueryResponse{
		Results: map[string]influxdb.GrafanaResult{
			"A": {
				Frames: []influxdb.DataFrame{
					{
						// malformed frame — only one column
						Data: influxdb.DataFrameData{Values: [][]any{{"cpu_usage"}}},
					},
					{
						Data: influxdb.DataFrameData{Values: [][]any{{"free"}, {"integer"}}},
					},
				},
			},
		},
	}

	got := influxdb.ExtractFieldKeys(resp)
	require.NotNil(t, got)
	require.Len(t, got.Fields, 1)
	assert.Equal(t, "free", got.Fields[0].FieldKey)
	assert.Equal(t, "integer", got.Fields[0].FieldType)
}

func newTestClient(t *testing.T, handler http.HandlerFunc) *influxdb.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: server.URL},
		Namespace: "default",
	}
	client, err := influxdb.NewClient(cfg)
	require.NoError(t, err)
	return client
}

func TestQuery_ReturnsTypedAPIErrorForHTTPFailure(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"results":{"A":{"error":"error parsing query","errorSource":"downstream","status":400}}}`))
	})

	_, err := client.Query(context.Background(), "influxdb-uid", influxdb.QueryRequest{Query: "SELECT * FROM cpu"})
	require.Error(t, err)

	var apiErr *queryerror.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, "influxdb", apiErr.Datasource)
	assert.Equal(t, "query", apiErr.Operation)
	assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	assert.Equal(t, "error parsing query", apiErr.Message)
	assert.Equal(t, "downstream", apiErr.ErrorSource)
}

func TestQuery_ReturnsTypedAPIErrorForQueryLevelError(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"results":{"A":{"error":"retention policy not found","errorSource":"downstream","status":400,"frames":[]}}}`))
	})

	_, err := client.Query(context.Background(), "influxdb-uid", influxdb.QueryRequest{Query: "SELECT * FROM cpu"})
	require.Error(t, err)

	var apiErr *queryerror.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, "influxdb", apiErr.Datasource)
	assert.Equal(t, "query", apiErr.Operation)
	assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	assert.Equal(t, "retention policy not found", apiErr.Message)
	assert.Equal(t, "downstream", apiErr.ErrorSource)
}

func TestMeasurements_ReturnsTypedAPIErrorForQueryLevelError(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"results":{"A":{"error":"database not found","errorSource":"downstream","status":404,"frames":[]}}}`))
	})

	_, err := client.Measurements(context.Background(), "influxdb-uid", influxdb.ModeInfluxQL, "")
	require.Error(t, err)

	var apiErr *queryerror.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, "influxdb", apiErr.Datasource)
	assert.Equal(t, "measurements query", apiErr.Operation)
	assert.Equal(t, http.StatusNotFound, apiErr.StatusCode)
	assert.Equal(t, "database not found", apiErr.Message)
}

func TestFieldKeys_ReturnsTypedAPIErrorForQueryLevelError(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"results":{"A":{"error":"measurement not found","errorSource":"downstream","status":404,"frames":[]}}}`))
	})

	_, err := client.FieldKeys(context.Background(), "influxdb-uid", "cpu")
	require.Error(t, err)

	var apiErr *queryerror.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, "influxdb", apiErr.Datasource)
	assert.Equal(t, "field keys query", apiErr.Operation)
	assert.Equal(t, http.StatusNotFound, apiErr.StatusCode)
	assert.Equal(t, "measurement not found", apiErr.Message)
}

func TestExtractTagKeys_AggregatesAllFrames(t *testing.T) {
	// SHOW TAG KEYS without --measurement returns one frame per measurement.
	resp := &influxdb.GrafanaQueryResponse{
		Results: map[string]influxdb.GrafanaResult{
			"A": {
				Frames: []influxdb.DataFrame{
					{
						Schema: influxdb.DataFrameSchema{Fields: []influxdb.Field{{Name: "tagKey"}}},
						Data:   influxdb.DataFrameData{Values: [][]any{{"host", "region"}}},
					},
					{
						Schema: influxdb.DataFrameSchema{Fields: []influxdb.Field{{Name: "tagKey"}}},
						Data:   influxdb.DataFrameData{Values: [][]any{{"host", "env"}}},
					},
				},
			},
		},
	}

	got := influxdb.ExtractTagKeys(resp)
	require.NotNil(t, got)
	// "host" appears in both frames but should be deduplicated.
	require.Len(t, got.TagKeys, 3)
	assert.Equal(t, "host", got.TagKeys[0])
	assert.Equal(t, "region", got.TagKeys[1])
	assert.Equal(t, "env", got.TagKeys[2])
}

func TestExtractTagKeys_DeduplicatesAcrossFrames(t *testing.T) {
	resp := &influxdb.GrafanaQueryResponse{
		Results: map[string]influxdb.GrafanaResult{
			"A": {
				Frames: []influxdb.DataFrame{
					{Data: influxdb.DataFrameData{Values: [][]any{{"host"}}}},
					{Data: influxdb.DataFrameData{Values: [][]any{{"host"}}}},
				},
			},
		},
	}

	got := influxdb.ExtractTagKeys(resp)
	require.NotNil(t, got)
	require.Len(t, got.TagKeys, 1)
	assert.Equal(t, "host", got.TagKeys[0])
}

func TestExtractTagKeys_EmptyResults(t *testing.T) {
	resp := &influxdb.GrafanaQueryResponse{
		Results: map[string]influxdb.GrafanaResult{},
	}

	got := influxdb.ExtractTagKeys(resp)
	require.NotNil(t, got)
	assert.Empty(t, got.TagKeys)
}

func TestExtractTagValues_AggregatesAllFrames(t *testing.T) {
	resp := &influxdb.GrafanaQueryResponse{
		Results: map[string]influxdb.GrafanaResult{
			"A": {
				Frames: []influxdb.DataFrame{
					{
						Schema: influxdb.DataFrameSchema{Fields: []influxdb.Field{{Name: "key"}, {Name: "value"}}},
						Data:   influxdb.DataFrameData{Values: [][]any{{"host", "host"}, {"server-a", "server-b"}}},
					},
					{
						Schema: influxdb.DataFrameSchema{Fields: []influxdb.Field{{Name: "key"}, {Name: "value"}}},
						Data:   influxdb.DataFrameData{Values: [][]any{{"host"}, {"server-c"}}},
					},
				},
			},
		},
	}

	got := influxdb.ExtractTagValues(resp)
	require.NotNil(t, got)
	require.Len(t, got.Values, 3)
	assert.Equal(t, influxdb.TagValue{Key: "host", Value: "server-a"}, got.Values[0])
	assert.Equal(t, influxdb.TagValue{Key: "host", Value: "server-b"}, got.Values[1])
	assert.Equal(t, influxdb.TagValue{Key: "host", Value: "server-c"}, got.Values[2])
}

func TestExtractTagValues_SkipsFramesWithFewerThanTwoColumns(t *testing.T) {
	resp := &influxdb.GrafanaQueryResponse{
		Results: map[string]influxdb.GrafanaResult{
			"A": {
				Frames: []influxdb.DataFrame{
					{Data: influxdb.DataFrameData{Values: [][]any{{"host"}}}},
					{Data: influxdb.DataFrameData{Values: [][]any{{"host"}, {"server-a"}}}},
				},
			},
		},
	}

	got := influxdb.ExtractTagValues(resp)
	require.NotNil(t, got)
	require.Len(t, got.Values, 1)
	assert.Equal(t, influxdb.TagValue{Key: "host", Value: "server-a"}, got.Values[0])
}

func TestTagKeys_ReturnsTypedAPIErrorForQueryLevelError(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"results":{"A":{"error":"database not found","errorSource":"downstream","status":404,"frames":[]}}}`))
	})

	_, err := client.TagKeys(context.Background(), "influxdb-uid", "")
	require.Error(t, err)

	var apiErr *queryerror.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, "influxdb", apiErr.Datasource)
	assert.Equal(t, "tag keys query", apiErr.Operation)
	assert.Equal(t, http.StatusNotFound, apiErr.StatusCode)
	assert.Equal(t, "database not found", apiErr.Message)
}

func TestTagValues_ReturnsTypedAPIErrorForQueryLevelError(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"results":{"A":{"error":"database not found","errorSource":"downstream","status":404,"frames":[]}}}`))
	})

	_, err := client.TagValues(context.Background(), "influxdb-uid", "host", "")
	require.Error(t, err)

	var apiErr *queryerror.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, "influxdb", apiErr.Datasource)
	assert.Equal(t, "tag values query", apiErr.Operation)
	assert.Equal(t, http.StatusNotFound, apiErr.StatusCode)
	assert.Equal(t, "database not found", apiErr.Message)
}

func TestMeasurements_RejectsUnsupportedMode(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		// should never be reached
		w.WriteHeader(http.StatusOK)
	})

	_, err := client.Measurements(context.Background(), "influxdb-uid", influxdb.ModeSQL, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SQL")
	assert.Contains(t, err.Error(), "not supported")
}

func TestQuery_ReturnsTypedAPIErrorWithBadRequestFallbackWhenStatusMissing(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// status field omitted — JSON unmarshals to 0
		_, _ = w.Write([]byte(`{"results":{"A":{"error":"unknown field","errorSource":"downstream","frames":[]}}}`))
	})

	_, err := client.Query(context.Background(), "influxdb-uid", influxdb.QueryRequest{Query: "SELECT unknown FROM cpu"})
	require.Error(t, err)

	var apiErr *queryerror.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	assert.Equal(t, "unknown field", apiErr.Message)
}
