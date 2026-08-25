package kg_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/providers/kg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_GetEntityQualityReport(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name: "returns report",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Contains(t, r.URL.Path, "/asserts/kg-quality/v1/entities/Service/my-service/quality-report")
				assert.Equal(t, "prod", r.URL.Query().Get("env"))
				assert.Equal(t, "shop", r.URL.Query().Get("namespace"))
				writeJSON(w, kg.QualityReport{
					EntityName:     "my-service",
					EntityType:     "Service",
					Env:            "prod",
					Namespace:      "shop",
					QualityPercent: 80,
					FailedCheckIDs: []string{"span-metrics"},
					ReportData: &kg.QualityReportData{
						Results: []kg.QualityCheckResult{
							{ID: "span-metrics", State: kg.QualityStateWarning, Title: "No request metrics found", Impact: "IMPORTANT"},
						},
					},
				})
			},
		},
		{
			name: "handles 404",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"message":"Quality report not found"}`))
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()
			client := newTestClient(t, server)
			report, err := client.GetEntityQualityReport(t.Context(), "Service", "my-service", "prod", "shop", "")
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "my-service", report.EntityName)
			assert.Equal(t, 80, report.QualityPercent)
			assert.Equal(t, []string{"span-metrics"}, report.FailedCheckIDs)
			require.NotNil(t, report.ReportData)
			require.Len(t, report.ReportData.Results, 1)
			assert.Equal(t, kg.QualityStateWarning, report.ReportData.Results[0].State)
		})
	}
}

func TestClient_ListQualityReports(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/asserts/kg-quality/v1/entities/quality-reports")
		q := r.URL.Query()
		assert.Equal(t, "Service", q.Get("type"))
		assert.Equal(t, "prod", q.Get("env"))
		assert.Equal(t, "DESC", q.Get("sortDirection"))
		assert.Equal(t, "2", q.Get("page"))
		assert.Equal(t, "50", q.Get("pageSize"))
		assert.Equal(t, []string{"span-metrics", "service-logs"}, q["failedCheckIds"])
		writeJSON(w, kg.QualityReportPage{
			Content: []kg.QualityReportListItem{
				{EntityName: "svc-a", EntityType: "Service", Env: "prod", QualityPercent: 60, FailedCheckIDs: []string{"span-metrics"}},
			},
			TotalElements: 1,
			TotalPages:    1,
			Number:        0,
			Size:          50,
			First:         true,
			Last:          true,
		})
	}))
	defer server.Close()

	client := newTestClient(t, server)
	page, err := client.ListQualityReports(t.Context(), kg.QualityReportQuery{
		Type:           "Service",
		Env:            "prod",
		FailedCheckIDs: []string{"span-metrics", "service-logs"},
		SortDirection:  "DESC",
		Page:           2,
		PageSize:       50,
	})
	require.NoError(t, err)
	require.Len(t, page.Content, 1)
	assert.Equal(t, "svc-a", page.Content[0].EntityName)
	assert.Equal(t, 60, page.Content[0].QualityPercent)
}

func TestQualityReportListTableCodec_Encode(t *testing.T) {
	codec := &kg.QualityReportListTableCodec{}

	var buf bytes.Buffer
	items := []kg.QualityReportListItem{
		{EntityName: "svc-a", EntityType: "Service", Env: "prod", QualityPercent: 60, FailedCheckIDs: []string{"span-metrics", "service-logs"}},
	}
	require.NoError(t, codec.Encode(&buf, items))
	out := buf.String()
	assert.Contains(t, out, "svc-a")
	assert.Contains(t, out, "60%")
	assert.Contains(t, out, "span-metrics")

	// Wrong type is a decode-safe error, not a panic.
	require.Error(t, codec.Encode(&bytes.Buffer{}, "not a slice"))
	require.Error(t, codec.Decode(&bytes.Buffer{}, nil))
}

func TestCheckQuality(t *testing.T) {
	perfect := []kg.QualityReportListItem{
		{EntityName: "a", EntityType: "Service", QualityPercent: 100},
		{EntityName: "b", EntityType: "Service", QualityPercent: 100},
	}
	mixed := []kg.QualityReportListItem{
		{EntityName: "a", EntityType: "Service", QualityPercent: 60, FailedCheckIDs: []string{"span-metrics", "service-logs"}},
		{EntityName: "b", EntityType: "Service", QualityPercent: 80, FailedCheckIDs: []string{"span-metrics"}},
		{EntityName: "c", EntityType: "Service", QualityPercent: 100},
	}

	tests := []struct {
		name        string
		handler     http.HandlerFunc
		wantStatus  kg.CheckStatus
		wantSummary bool
		check       func(t *testing.T, s *kg.QualityCheckSummary)
	}{
		{
			name: "warns when services are below perfect",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, kg.QualityReportPage{Content: mixed, TotalPages: 1, Last: true})
			},
			wantStatus:  kg.CheckWarn,
			wantSummary: true,
			check: func(t *testing.T, s *kg.QualityCheckSummary) {
				t.Helper()
				assert.Equal(t, 3, s.TotalServices)
				assert.Equal(t, 2, s.BelowPerfect)
				assert.Equal(t, 60, s.WorstQuality)
				assert.False(t, s.Sampled)
				require.NotEmpty(t, s.TopFailedChecks)
				// span-metrics (2) ranks ahead of service-logs (1).
				assert.Equal(t, "span-metrics", s.TopFailedChecks[0].ID)
				assert.Equal(t, 2, s.TopFailedChecks[0].Count)
			},
		},
		{
			name: "samples worst services when the population exceeds the window",
			handler: func(w http.ResponseWriter, r *http.Request) {
				// diagnose pulls the worst services first, one bounded page.
				q := r.URL.Query()
				assert.Equal(t, "ASC", q.Get("sortDirection"))
				assert.Equal(t, "50", q.Get("pageSize"))
				// Backend reports 120 total services but returns only the worst
				// window; none reach 100%, so the aggregate is a lower bound.
				writeJSON(w, kg.QualityReportPage{
					Content: []kg.QualityReportListItem{
						{EntityName: "a", EntityType: "Service", QualityPercent: 40, FailedCheckIDs: []string{"span-metrics"}},
						{EntityName: "b", EntityType: "Service", QualityPercent: 55, FailedCheckIDs: []string{"span-metrics"}},
					},
					TotalElements: 120,
					TotalPages:    60,
					Last:          false,
				})
			},
			wantStatus:  kg.CheckWarn,
			wantSummary: true,
			check: func(t *testing.T, s *kg.QualityCheckSummary) {
				t.Helper()
				// TotalServices reflects the true population, not the window.
				assert.Equal(t, 120, s.TotalServices)
				assert.Equal(t, 2, s.BelowPerfect)
				assert.Equal(t, 40, s.WorstQuality)
				assert.True(t, s.Sampled)
			},
		},
		{
			name: "passes when all services are perfect",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, kg.QualityReportPage{Content: perfect, TotalPages: 1, Last: true})
			},
			wantStatus:  kg.CheckPass,
			wantSummary: true,
			check: func(t *testing.T, s *kg.QualityCheckSummary) {
				t.Helper()
				assert.Equal(t, 0, s.BelowPerfect)
			},
		},
		{
			name: "skips when service not provisioned (404)",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"message":"not found"}`))
			},
			wantStatus: kg.CheckSkip,
		},
		{
			name: "skips when no reports for scope",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, kg.QualityReportPage{Content: nil, TotalPages: 0, Last: true, Empty: true})
			},
			wantStatus: kg.CheckSkip,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()
			client := newTestClient(t, server)
			scope := kg.NewTestScopeFlags("prod", "", "")
			result, summary := kg.CheckQuality(t.Context(), client, &scope)

			assert.Equal(t, "Instrumentation quality", result.Name)
			assert.Equal(t, tt.wantStatus, result.Status)
			if tt.wantSummary {
				require.NotNil(t, summary)
				if tt.check != nil {
					tt.check(t, summary)
				}
			} else {
				assert.Nil(t, summary)
			}
		})
	}
}

func TestQualityReportTableCodec_Encode(t *testing.T) {
	codec := &kg.QualityReportTableCodec{}

	var buf bytes.Buffer
	report := &kg.QualityReport{
		EntityName:     "my-service",
		EntityType:     "Service",
		Env:            "prod",
		QualityPercent: 80,
		FailedCheckIDs: []string{"span-metrics"},
		ReportData: &kg.QualityReportData{
			Results: []kg.QualityCheckResult{
				{
					ID:          "span-metrics",
					State:       kg.QualityStateWarning,
					Title:       "No request metrics found",
					Impact:      "IMPORTANT",
					Description: "Emit RED span metrics to power service KPIs.",
					DocURL:      "https://example.test/docs/span-metrics",
					Reference:   &kg.QualityCheckReference{Title: "OTel Instrumentation Score", URL: "https://example.test/spec"},
				},
				// SUCCESS checks must not appear in the detail footer.
				{ID: "deployment-env", State: kg.QualityStateSuccess, Title: "Deployment environment set", Description: "should not show"},
			},
		},
	}
	require.NoError(t, codec.Encode(&buf, report))
	out := buf.String()
	assert.Contains(t, out, "my-service")
	assert.Contains(t, out, "80%")
	assert.Contains(t, out, "span-metrics")
	assert.Contains(t, out, "Failed checks: span-metrics")

	// Detail footer surfaces the metadata the table columns can't hold.
	assert.Contains(t, out, "Details:")
	assert.Contains(t, out, "Emit RED span metrics to power service KPIs.")
	assert.Contains(t, out, "docs: https://example.test/docs/span-metrics")
	assert.Contains(t, out, "OTel Instrumentation Score (https://example.test/spec)")
	// SUCCESS checks are excluded from the footer.
	assert.NotContains(t, out, "should not show")

	require.Error(t, codec.Encode(&bytes.Buffer{}, "not a report"))
}
