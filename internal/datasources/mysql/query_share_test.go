package mysql_test

import (
	"testing"

	"github.com/grafana/gcx/internal/datasources/mysql"
	"github.com/grafana/gcx/internal/providers"
	"github.com/stretchr/testify/assert"
)

func TestQueryCmd_ExploreLinkFlags(t *testing.T) {
	cmd := mysql.QueryCmd(&providers.ConfigLoader{})
	assert.NotNil(t, cmd.Flags().Lookup("share-link"))
	assert.NotNil(t, cmd.Flags().Lookup("open"))
}
