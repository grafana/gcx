package stacks_test

import (
	"testing"

	"github.com/grafana/gcx/internal/cloud"
	"github.com/grafana/gcx/internal/providers/stacks"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/require"
)

func TestRegionTableGolden(t *testing.T) {
	for _, tt := range []struct {
		name string
		rows []cloud.Region
	}{
		{"populated", []cloud.Region{{Slug: "us", Name: "US Central", Description: "United States", Provider: "gcp"}, {Slug: "eu", Name: "Europe"}}},
		{"empty", []cloud.Region{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := stacks.ExportEncodeRegionTable(tt.rows)
			require.NoError(t, err)
			testutils.Golden(t, "regions_table_"+tt.name, got)
		})
	}
}
