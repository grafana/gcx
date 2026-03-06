package local

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/grafana/grafana-app-sdk/logging"
	"github.com/grafana/grafanactl/internal/format"
	"github.com/grafana/grafanactl/internal/logs"
	"github.com/grafana/grafanactl/internal/resources"
	"golang.org/x/sync/errgroup"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type UnrecognisedFormatError struct {
	File   string
	Format string
}

func (e UnrecognisedFormatError) Error() string {
	if e.Format != "" {
		return "unrecognized format " + e.Format
	}

	return "unrecognized format for " + e.File
}

type ParseError struct {
	File string
	Err  error
}

func (err ParseError) Error() string {
	return fmt.Sprintf("parse error in '%s': %s", err.File, err.Err)
}

// FSReader is a reader that reads resources from the filesystem.
//
// The reader will read all resources from the filesystem and return them as
// an unstructured list.
type FSReader struct {
	// Decoders to use when decoding resources.
	Decoders map[format.Format]format.Codec
	// Whether to stop reading resources upon encountering an error.
	StopOnError bool
	// MaxConcurrentReads is the maximum number of concurrent file reads.
	// If not set, the default is 1.
	MaxConcurrentReads int
}

// Read reads all resources from the filesystem and returns them as an unstructured list.
func (reader *FSReader) Read(
	ctx context.Context, dst *resources.Resources, filters resources.Filters, paths []string,
) error {
	logger := logging.FromContext(ctx).With(slog.String("component", "fs_reader"))

	if len(paths) == 0 {
		logger.Debug("no paths or resources to read")
		return nil
	}

	if reader.MaxConcurrentReads < 1 {
		reader.MaxConcurrentReads = 1
	}

	// Error group & channel for coordinating the reading and processing of resources.
	gr, ctx := errgroup.WithContext(ctx)

	// Read directories.
	pathCh := make(chan string, reader.MaxConcurrentReads)
	gr.Go(func() error {
		defer close(pathCh)

		for _, path := range paths {
			info, err := os.Stat(path)
			if err != nil {
				if reader.StopOnError {
					return err
				}

				logger.Warn("Failed to stat path", slog.String("path", path), logs.Err(err))
				continue
			}

			// If the path exists and it's not a directory we don't need to traverse it,
			// and instead we can just send the path for reading.
			if !info.IsDir() {
				select {
				case <-ctx.Done():
					return nil
				case pathCh <- path:
				}

				continue
			}

			if err := filepath.WalkDir(path, func(path string, info os.DirEntry, err error) error {
				// Early return if context is cancelled
				if ctx.Err() != nil {
					return filepath.SkipAll
				}

				if err != nil {
					if reader.StopOnError {
						return err
					}
					logger.Warn("Failed to traverse directory", slog.String("path", path), logs.Err(err))
					return nil
				}

				// For directories, return nil to continue traversing
				if info.IsDir() {
					return nil
				}

				select {
				case <-ctx.Done():
					return filepath.SkipAll
				case pathCh <- path:
				}

				return nil
			}); err != nil {
				return err
			}
		}

		return nil
	})

	resCh := make(chan readResult, reader.MaxConcurrentReads)
	gr.Go(func() error {
		defer close(resCh)

		readg, ctx := errgroup.WithContext(ctx)
		readg.SetLimit(reader.MaxConcurrentReads)

		for path := range pathCh {
			readg.Go(func() error {
				var object resources.Resource

				// Read and decode the file
				if err := reader.ReadFile(ctx, &object, path); err != nil {
					if reader.StopOnError {
						return fmt.Errorf("failed to read file %s: %w", path, err)
					}

					logger.Warn("failed to read file", slog.String("path", path), logs.Err(err))
					return nil
				}

				if !filters.Matches(object) {
					logger.Debug("skipping object because it does not match any filters",
						"path", path,
						"gvk", object.GroupVersionKind(),
						"name", object.Name(),
					)
					return nil
				}

				res := readResult{
					Object: object,
					Path:   path,
				}

				select {
				case <-ctx.Done():
				case resCh <- res:
				}

				return nil
			})
		}

		return readg.Wait()
	})

	// Read all results in parallel.
	gr.Go(func() error {
		idx := make(map[objIdx]resources.Resource)

		for res := range resCh {
			obj := res.Object

			if _, ok := idx[objIdx{
				gvk:  obj.Raw.GetGroupVersionKind(),
				name: obj.Name(),
			}]; ok {
				logger.Info("skipping duplicate object",
					"gvk", obj.Raw.GetGroupVersionKind(),
					"name", obj.Name(),
					"path", res.Path,
				)

				continue
			}

			logger.Debug("adding object",
				"gvk", obj.Raw.GetGroupVersionKind(),
				"name", obj.Name(),
				"path", res.Path,
			)

			dst.Add(&obj)
		}

		return nil
	})

	return gr.Wait()
}

// ReadFile reads a resource from a file.
// It expects that the file only contains a single resource.
func (reader *FSReader) ReadFile(ctx context.Context, result *resources.Resource, filePath string) error {
	logger := logging.FromContext(ctx).With(slog.String("component", "fs_reader"), slog.String("file", filePath))

	decoder, err := reader.decoderForFormat(strings.TrimPrefix(path.Ext(filePath), "."))
	if err != nil {
		return err
	}

	logger.Debug("Parsing file", slog.String("file", filePath), slog.String("codec", string(decoder.Format())))

	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	return reader.readRaw(decoder, file, filePath, result)
}

// ReadBytes reads a resource from a byte slice.
func (reader *FSReader) ReadBytes(
	ctx context.Context, dst *resources.Resources, raw []byte, inputFormat string,
) error {
	logger := logging.FromContext(ctx).With(slog.String("component", "fs_reader"))
	logger.Debug("Parsing bytes", slog.String("format", inputFormat))

	decoder, err := reader.decoderForFormat(inputFormat)
	if err != nil {
		return err
	}

	res := &resources.Resource{}
	if err := reader.readRaw(decoder, bytes.NewBuffer(raw), "", res); err != nil {
		return err
	}
	dst.Add(res)

	return nil
}

func (reader *FSReader) readRaw(
	decoder format.Codec, src io.Reader, path string, dst *resources.Resource,
) error {
	object := &unstructured.Unstructured{}

	if err := decoder.Decode(src, object); err != nil {
		return ParseError{Err: err}
	}

	if err := dst.SetUnstructured(object); err != nil {
		return err
	}

	dst.SetSource(resources.SourceInfo{
		Path:   path,
		Format: decoder.Format(),
	})

	return nil
}

func (reader *FSReader) decoderForFormat(input string) (format.Codec, error) {
	switch input {
	case "json":
		return reader.Decoders[format.JSON], nil
	case "yaml", "yml":
		return reader.Decoders[format.YAML], nil
	default:
		return nil, UnrecognisedFormatError{Format: input}
	}
}

type objIdx struct {
	gvk  schema.GroupVersionKind
	name string
}

type readResult struct {
	Object resources.Resource
	Path   string
}
