package definitions_test

import (
	"testing"

	"github.com/grafana/gcx/internal/gcxerrors"
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
	require.Zero(t, st.updateCalls)
}

func TestUUIDLessPushMatchesNaturalKey(t *testing.T) {
	for _, tc := range []struct {
		name             string
		existing         bool
		creates, updates int
	}{
		{name: "created earlier in batch", creates: 1, updates: 1},
		{name: "remote slug match", existing: true, updates: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := &sloAPIState{slos: map[string]definitions.Slo{}}
			if tc.existing {
				st.slos["remote"] = definitions.Slo{UUID: "remote", Name: "HTTP Availability!"}
			}
			srv := newSLOServer(t, st)
			dir := t.TempDir()
			first := writeSLOManifest(t, dir, "first.yaml", "http-availability", "")
			second := writeSLOManifest(t, dir, "second.yaml", "HTTP Availability!", "")
			_, _, err := runDefinitions(t, srv.URL, false, "", "push", first, second)
			require.NoError(t, err)
			st.mu.Lock()
			defer st.mu.Unlock()
			require.Equal(t, 1, st.listCalls)
			require.Equal(t, tc.creates, st.createCalls)
			require.Equal(t, tc.updates, st.updateCalls)
		})
	}
}

func TestUUIDLessPushAmbiguityOnLaterFile(t *testing.T) {
	st := &sloAPIState{slos: map[string]definitions.Slo{
		"first":  {UUID: "first", Name: "Duplicate!"},
		"second": {UUID: "second", Name: "duplicate"},
	}}
	srv := newSLOServer(t, st)
	dir := t.TempDir()
	first := writeSLOManifest(t, dir, "first.yaml", "Unique", "")
	second := writeSLOManifest(t, dir, "second.yaml", "Duplicate", "")
	third := writeSLOManifest(t, dir, "third.yaml", "Skipped", "")
	stdout, _, err := runDefinitions(t, srv.URL, false, "", "push", first, second, third, "-o", "json")
	var partial *gcxerrors.EmittedError
	require.ErrorAs(t, err, &partial)
	require.ErrorContains(t, err, `multiple SLO resources have natural key "duplicate"`)
	doc, ok := decodeSingleJSONValue(t, stdout).(map[string]any)
	require.True(t, ok)
	summary, ok := doc["summary"].(map[string]any)
	require.True(t, ok)
	require.EqualValues(t, 1, summary["succeeded"])
	require.EqualValues(t, 1, summary["failed"])
	require.EqualValues(t, 1, summary["skipped"])
	st.mu.Lock()
	defer st.mu.Unlock()
	require.Equal(t, 1, st.listCalls)
	require.Equal(t, 1, st.createCalls)
	require.Zero(t, st.updateCalls)
}
