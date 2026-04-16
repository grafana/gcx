package watch

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/fsnotify/fsnotify"
	"github.com/grafana/gcx/internal/logs"
	"github.com/grafana/grafana-app-sdk/logging"
)

type Watcher struct {
	logger   logging.Logger
	notifier *fsnotify.Watcher
	callback func(string)
}

func NewWatcher(ctx context.Context, callback func(string)) (*Watcher, error) {
	notifier, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	return &Watcher{
		logger:   logging.FromContext(ctx).With("component", "watcher"),
		notifier: notifier,
		callback: callback,
	}, nil
}

func (w *Watcher) Add(watchPaths ...string) error {
	for _, watchPath := range watchPaths {
		isDir, err := isDirectory(watchPath)
		if err != nil {
			return err
		}
		if !isDir {
			return fmt.Errorf("%s id not a directory", watchPath)
		}

		err = filepath.WalkDir(watchPath, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() {
				return nil
			}

			w.logger.Debug("Adding path to watch list ", slog.String("path", path))
			return w.notifier.Add(path)
		})
		if err != nil {
			return err
		}
	}

	return nil
}

func (w *Watcher) Watch(ctx context.Context) {
	go func() {
		defer w.notifier.Close()
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-w.notifier.Events:
				if !ok {
					return
				}

				// Temporary file created by an editor
				if strings.HasSuffix(event.Name, "~") {
					continue
				}

				w.logger.Debug("event:", slog.Any("event", event))
				if event.Has(fsnotify.Create) || event.Has(fsnotify.Write) {
					w.logger.Debug("detected file change:", slog.String("file", event.Name), slog.String("op", event.Op.String()))
					w.callback(event.Name)
				}
			case err, ok := <-w.notifier.Errors:
				if !ok {
					return
				}
				w.logger.Warn("watch error", logs.Err(err))
			}
		}
	}()
}

func isDirectory(path string) (bool, error) {
	fileInfo, err := os.Stat(path)
	if err != nil {
		return false, err
	}

	return fileInfo.IsDir(), err
}
