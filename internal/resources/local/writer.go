package local

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/logs"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/grafana-app-sdk/logging"
)

type FileNamer func(resource *resources.Resource) (string, error)

// GroupResourcesByKind organizes resources by their full GVK, writing resources
// in a folder named after their plural resource name, version, and group.
// This matches the resource selector syntax used by `resources get`.
//
// Directory format: `{plural}.{version}.{group}/{name}.{extension}`
// Example: `dashboards.v1alpha1.dashboard.grafana.app/my-dash.yaml`
//
// The plurals map provides GVK → plural name mappings from the discovery registry.
// If a GVK is not found in the map, falls back to lowercase kind + "s".
func GroupResourcesByKind(extension string, plurals map[string]string) FileNamer {
	return func(resource *resources.Resource) (string, error) {
		if resource.Name() == "" {
			return "", errors.New("resource has no name")
		}

		gvk := resource.GroupVersionKind()
		plural := plurals[gvk.String()]
		if plural == "" {
			// Fallback: lowercase kind + "s" (matches K8s convention for most resources).
			plural = strings.ToLower(gvk.Kind) + "s"
		}

		dir := plural + "." + gvk.Version + "." + gvk.Group
		return filepath.Join(dir, resource.Name()+"."+extension), nil
	}
}

// PluralsFromFilters builds a GVK→plural mapping from resource filters.
// Used by GroupResourcesByKind to produce directory names that match selector syntax.
func PluralsFromFilters(filters resources.Filters) map[string]string {
	m := make(map[string]string, len(filters))
	for _, f := range filters {
		gvk := f.Descriptor.GroupVersionKind()
		m[gvk.String()] = f.Descriptor.Plural
	}
	return m
}

type FSWriter struct {
	// Path on the filesystem where resources should be written.
	Path string
	// Namer is a function mapping a resource to a path on the filesystem
	// (relative to Directory).
	// The naming strategy used here directly controls the file hierarchy created
	// by FSWriter.
	// Note: the path should contain an extension.
	Namer FileNamer
	// Encoder to use when encoding resources.
	Encoder format.Encoder
	// Whether to stop writing resources upon encountering an error.
	StopOnError bool
}

func (writer *FSWriter) Write(ctx context.Context, resources *resources.Resources) error {
	if resources.Len() == 0 {
		return nil
	}

	logger := logging.FromContext(ctx).With(slog.String("path", writer.Path))
	logger.Debug("Writing resources", slog.Int("resources", resources.Len()))

	// Create the directory if it doesn't exist
	if err := ensureDirectoryExists(writer.Path); err != nil {
		return err
	}

	for _, resource := range resources.AsList() {
		if err := writer.writeSingle(resource); err != nil {
			if writer.StopOnError {
				return err
			}

			logger.Warn("could not write resource: skipping", slog.String("kind", resource.Kind()), logs.Err(err))
		}
	}

	return nil
}

func (writer *FSWriter) writeSingle(resource *resources.Resource) error {
	filename, err := writer.Namer(resource)
	if err != nil {
		return fmt.Errorf("could not generate resource path: %w", err)
	}

	fullFileName := filepath.Join(writer.Path, filename)
	if err := ensureDirectoryExists(filepath.Dir(fullFileName)); err != nil {
		return fmt.Errorf("could ensure resource directory exists: %w", err)
	}

	file, err := os.OpenFile(fullFileName, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("could open resource file: %w", err)
	}
	defer file.Close()

	obj := resource.ToUnstructured()
	// MarshalJSON() methods for [unstructured.UnstructuredList] and
	// [unstructured.Unstructured] types are defined on pointer receivers,
	// so we need to make sure we dereference `resource` before formatting it.
	if err := writer.Encoder.Encode(file, &obj); err != nil {
		return fmt.Errorf("could write resource: %w", err)
	}

	return nil
}

func ensureDirectoryExists(directory string) error {
	info, err := os.Stat(directory)
	if os.IsNotExist(err) {
		return os.MkdirAll(directory, 0755)
	}

	if err != nil {
		return err
	}

	if !info.IsDir() {
		return &fs.PathError{
			Op:   "mkdir",
			Path: directory,
			Err:  os.ErrInvalid,
		}
	}

	return nil
}
