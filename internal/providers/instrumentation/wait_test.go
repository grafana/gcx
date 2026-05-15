package instrumentation_test

import (
	"testing"

	"github.com/grafana/gcx/internal/providers/instrumentation"
)

func TestClassifyK8sMonitoringStatus(t *testing.T) {
	tests := []struct {
		status instrumentation.InstrumentationStatus
		want   instrumentation.WaitOutcome
	}{
		{"K8S_MONITORING_STATUS_UNSPECIFIED", instrumentation.WaitPending},
		{"K8S_MONITORING_STATUS_NOT_INSTRUMENTED", instrumentation.WaitPending},
		{"K8S_MONITORING_STATUS_INSTRUMENTED", instrumentation.WaitSuccess},
		{"K8S_MONITORING_STATUS_PENDING_INSTRUMENTATION", instrumentation.WaitPending},
		{"K8S_MONITORING_STATUS_ERROR", instrumentation.WaitError},
		{"K8S_MONITORING_STATUS_EXCLUDED", instrumentation.WaitSuccess},
		{"K8S_MONITORING_STATUS_PENDING_UNINSTRUMENTATION", instrumentation.WaitPending},
		{"", instrumentation.WaitPending},
		{"UNKNOWN_VALUE", instrumentation.WaitPending},
	}
	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			got := instrumentation.ClassifyK8sMonitoringStatus(tt.status)
			if got != tt.want {
				t.Errorf("ClassifyK8sMonitoringStatus(%q) = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}

func TestClassifyInstrumentationStatus(t *testing.T) {
	tests := []struct {
		status instrumentation.InstrumentationStatus
		want   instrumentation.WaitOutcome
	}{
		{"INSTRUMENTATION_STATUS_UNSPECIFIED", instrumentation.WaitPending},
		{"INSTRUMENTATION_STATUS_NOT_INSTRUMENTED", instrumentation.WaitPending},
		{"INSTRUMENTATION_STATUS_INSTRUMENTED", instrumentation.WaitSuccess},
		{"INSTRUMENTATION_STATUS_PENDING_INSTRUMENTATION", instrumentation.WaitPending},
		{"INSTRUMENTATION_STATUS_INSTRUMENTATION_ERROR", instrumentation.WaitError},
		{"INSTRUMENTATION_STATUS_EXCLUDED", instrumentation.WaitSuccess},
		{"INSTRUMENTATION_STATUS_PENDING_UNINSTRUMENTATION", instrumentation.WaitPending},
		{"", instrumentation.WaitPending},
		{"UNKNOWN_VALUE", instrumentation.WaitPending},
	}
	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			got := instrumentation.ClassifyInstrumentationStatus(tt.status)
			if got != tt.want {
				t.Errorf("ClassifyInstrumentationStatus(%q) = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}
