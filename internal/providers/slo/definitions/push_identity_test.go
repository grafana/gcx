package definitions_test

import (
	"testing"

	"github.com/grafana/gcx/internal/providers/slo/definitions"
	"github.com/stretchr/testify/require"
)

func TestUUIDLessPushIsIdempotent(t *testing.T) {
	st := &sloAPIState{}
	srv := newSLOServer(t, st)
	file := writeSLOManifest(t, t.TempDir(), "resource.yaml", "Triage fixture", "")
	for range 2 {
		_, _, err := runDefinitions(t, srv.URL, false, "", "push", file)
		require.NoError(t, err)
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	require.Equal(t, 1, st.createCalls, "same UUID-less manifest must not be created twice")
}

func TestUUIDLessPushRejectsAmbiguousName(t *testing.T) {
	st := &sloAPIState{slos: map[string]definitions.Slo{
		"first":  {UUID: "first", Name: "Duplicate"},
		"second": {UUID: "second", Name: "Duplicate"},
	}}
	srv := newSLOServer(t, st)
	file := writeSLOManifest(t, t.TempDir(), "resource.yaml", "Duplicate", "")
	_, _, err := runDefinitions(t, srv.URL, false, "", "push", file)
	require.ErrorContains(t, err, "multiple")
	st.mu.Lock()
	defer st.mu.Unlock()
	require.Zero(t, st.createCalls)
}
