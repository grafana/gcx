package local_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/resources/local"
	"github.com/grafana/grafana-app-sdk/logging"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestReaderRetainsRegisteredDuplicateEnvelopes(t *testing.T) {
	for _, retain := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary kind", true: "batch identity kind"}[retain], func(t *testing.T) {
			gvk := schema.GroupVersionKind{Group: "reader-duplicates.example", Version: "v1", Kind: map[bool]string{false: "Ordinary", true: "Batch"}[retain]}
			adapter.Register(adapter.Registration{GVK: gvk, RetainLocalDuplicates: retain, Schema: func() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }})
			dir := t.TempDir()
			for _, id := range []string{"id-1", "id-2"} {
				raw, err := json.Marshal(map[string]any{"apiVersion": gvk.GroupVersion().String(), "kind": gvk.Kind, "metadata": map[string]any{"name": "same-name", "annotations": map[string]any{"test.example/id": id}}, "spec": map[string]any{"title": id}})
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(filepath.Join(dir, id+".json"), raw, 0600))
			}
			dest := resources.NewResources()
			var warnings bytes.Buffer
			reader := local.FSReader{Decoders: format.Codecs(), StopOnError: true, MaxConcurrentReads: 2, Warn: &warnings}
			require.NoError(t, reader.Read(t.Context(), dest, nil, []string{dir}))
			if !retain {
				require.Equal(t, 1, dest.Len())
				require.Contains(t, warnings.String(), "skipping duplicate object")
				require.Contains(t, warnings.String(), "id-1.json")
				require.Contains(t, warnings.String(), "id-2.json")
				require.Contains(t, warnings.String(), "remove stale files before pushing")
				return
			}
			require.Empty(t, warnings.String())
			require.Equal(t, 2, dest.Len())
			var ids []string
			require.NoError(t, dest.ForEach(func(r *resources.Resource) error {
				require.Equal(t, "same-name", r.Name())
				require.NotEmpty(t, r.SourcePath())
				ids = append(ids, r.Annotations()["test.example/id"])
				return nil
			}))
			require.ElementsMatch(t, []string{"id-1", "id-2"}, ids)
		})
	}
}

func TestReaderDuplicateIdentityIncludesNamespace(t *testing.T) {
	dir := t.TempDir()
	for _, namespace := range []string{"one", "two"} {
		resource := titledResource("same-name", "CPU")
		resource.Object.SetNamespace(namespace)
		writeManifest(t, dir, namespace+".json", resource)
	}
	var warnings bytes.Buffer
	reader := local.FSReader{Decoders: format.Codecs(), StopOnError: true, MaxConcurrentReads: 2, Warn: &warnings}
	dest := resources.NewResources()
	require.NoError(t, reader.Read(t.Context(), dest, nil, []string{dir}))
	require.Equal(t, 2, dest.Len())
	require.Empty(t, warnings.String())
}

func TestReaderDuplicateWarningAtWarnLevel(t *testing.T) {
	dir := t.TempDir()
	for _, path := range []string{"uid.json", "cpu.json"} {
		writeManifest(t, dir, path, titledResource("uid", "CPU"))
	}
	var warnings bytes.Buffer
	logger := logging.NewSLogLogger(slog.NewTextHandler(&warnings, &slog.HandlerOptions{Level: slog.LevelWarn}))
	reader := local.FSReader{Decoders: format.Codecs(), StopOnError: true}
	require.NoError(t, reader.Read(logging.Context(t.Context(), logger), resources.NewResources(), nil, []string{dir}))
	require.Contains(t, warnings.String(), "level=WARN")
	require.Contains(t, warnings.String(), "skipping duplicate object")
}
