package strcase_test

import (
	"testing"

	"github.com/grafana/gcx/internal/strcase"
	"github.com/stretchr/testify/assert"
)

func TestToSnakeCase(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"MyDashboard", "my_dashboard"},
		{"my-dashboard", "my_dashboard"},
		{"my dashboard", "my_dashboard"},
		{"myDashboardName", "my_dashboard_name"},
		{"HTMLParser", "html_parser"},
		{"simple", "simple"},
		{"already_snake", "already_snake"},
		{"with123numbers", "with_123_numbers"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, strcase.ToSnakeCase(tt.input))
		})
	}
}

func TestToKebabCase(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"MyDashboard", "my-dashboard"},
		{"my_dashboard", "my-dashboard"},
		{"myDashboardName", "my-dashboard-name"},
		{"simple", "simple"},
		{"already-kebab", "already-kebab"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, strcase.ToKebabCase(tt.input))
		})
	}
}

func TestToPascalCase(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"my-dashboard", "MyDashboard"},
		{"my_dashboard", "MyDashboard"},
		{"my dashboard name", "MyDashboardName"},
		{"simple", "Simple"},
		{"already", "Already"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, strcase.ToPascalCase(tt.input))
		})
	}
}
