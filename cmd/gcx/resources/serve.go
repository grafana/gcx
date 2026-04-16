package resources

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"runtime"
	"slices"

	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/logs"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/local"
	"github.com/grafana/gcx/internal/server"
	"github.com/grafana/gcx/internal/server/livereload"
	"github.com/grafana/gcx/internal/server/watch"
	"github.com/grafana/grafana-app-sdk/logging"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type serveOpts struct {
	Address       string
	Port          int
	WatchPaths    []string
	NoWatch       bool
	Script        string
	ScriptFormat  string
	MaxConcurrent int
}

func (opts *serveOpts) setup(flags *pflag.FlagSet) {
	flags.StringVar(&opts.Address, "address", "0.0.0.0", "Address to bind")
	flags.IntVar(&opts.Port, "port", 8080, "Port on which the server will listen")
	flags.StringArrayVarP(&opts.WatchPaths, "watch", "w", nil, "Paths to watch for changes")
	flags.BoolVar(&opts.NoWatch, "no-watch", opts.NoWatch, "Do not watch for changes")
	flags.StringVarP(&opts.Script, "script", "S", "", "Script to execute to generate a resource")
	flags.StringVarP(&opts.ScriptFormat, "script-format", "f", "json", "Format of the data returned by the script")
	flags.IntVar(&opts.MaxConcurrent, "max-concurrent", 10, "Maximum number of concurrent operations")
}

func (opts *serveOpts) watchTargets(args []string) []string {
	if opts.NoWatch {
		return nil
	}

	return slices.Concat(args, opts.WatchPaths)
}

func (opts *serveOpts) Validate() error {
	if opts.MaxConcurrent < 1 {
		return errors.New("max-concurrent must be greater than zero")
	}

	return nil
}

func ServeCmd(configOpts *cmdconfig.Options) *cobra.Command {
	opts := &serveOpts{}

	cmd := &cobra.Command{
		Use:   "serve [RESOURCE_DIR]...",
		Args:  cobra.ArbitraryArgs,
		Short: "Serve Grafana resources locally",
		Long: `Serve Grafana resources locally.

The server started by this command makes it easy to explore and review resources
locally.

While resources are loaded from disk, the server will use the Grafana instance
described in the current context to access some data (example: to run queries
when previewing dashboards).

Note on NFS/SMB and watch mode: fsnotify requires support from underlying
OS to work. The current NFS and SMB protocols does not provide network level
support for file notifications.
`,
		Example: `
	# Serve resources from a directory:
	gcx dev serve ./resources

	# Serve resources from a directory but don't watch for changes:
	gcx dev serve ./resources --no-watch

	# Serve resources from a script that outputs a YAML resource and watch for changes:
	# Note: the Grafana Foundation SDK can be used to generate dashboards (https://grafana.github.io/grafana-foundation-sdk/)
	gcx dev serve --script 'go run dashboard-generator/*.go' --watch ./dashboard-generator --script-format yaml
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			cfg, err := configOpts.LoadConfig(cmd.Context())
			if err != nil {
				return err
			}

			logger := logging.FromContext(cmd.Context())
			parsedResources := resources.NewResources()
			reader := local.FSReader{
				Decoders:           format.Codecs(),
				StopOnError:        false,
				MaxConcurrentReads: opts.MaxConcurrent,
			}

			if len(args) != 0 {
				if err := reader.Read(cmd.Context(), parsedResources, resources.Filters{}, args); err != nil {
					return err
				}
			}

			parseFromScript := func() error {
				output, err := executeWatchScript(cmd.Context(), opts.Script)
				if err != nil {
					return err
				}

				if err = reader.ReadBytes(cmd.Context(), parsedResources, output, opts.ScriptFormat); err != nil {
					logger.Warn("Could not parse script output", logs.Err(err))
					return err
				}

				return nil
			}

			if opts.Script != "" {
				if err := parseFromScript(); err != nil {
					return err
				}
			}

			//nolint:nestif
			if len(opts.watchTargets(args)) > 0 {
				livereload.Initialize()

				parsedResources.OnChange(func(resource *resources.Resource) {
					logger.Debug("Resource changed in memory", slog.String("resource", string(resource.Ref())))
					livereload.ReloadResource(resource)
				})

				// By default, react to changes by parsing changed files
				onInputChange := func(file string) {
					object := &resources.Resource{}
					if err = reader.ReadFile(cmd.Context(), object, file); err != nil {
						logger.Warn("Could not parse file", slog.String("file", file), logs.Err(err))
						return
					}

					parsedResources.Add(object)
				}

				// If a script is given, run the script on change
				if opts.Script != "" {
					onInputChange = func(_ string) {
						if err := parseFromScript(); err != nil {
							_, _ = cmd.ErrOrStderr().Write([]byte(err.Error()))
						}
					}
				}

				watcher, err := watch.NewWatcher(cmd.Context(), onInputChange)
				if err != nil {
					return err
				}

				if err := watcher.Add(opts.watchTargets(args)...); err != nil {
					return err
				}

				// Start listening for events.
				watcher.Watch(cmd.Context())
			}

			serverCfg := server.Config{
				ListenAddr: opts.Address,
				Port:       opts.Port,
				NoColor:    cmd.Flags().Lookup("no-color").Value.String() == "true",
			}
			resourceServer := server.New(serverCfg, cfg.GetCurrentContext(), parsedResources)

			logger.Debug(fmt.Sprintf("Listening on %s:%d", opts.Address, opts.Port))
			cmdio.Info(cmd.OutOrStdout(), "Server will be available on http://localhost:%d/", opts.Port)

			return resourceServer.Start(cmd.Context())
		},
	}

	opts.setup(cmd.Flags())

	return cmd
}

func executeWatchScript(ctx context.Context, command string) ([]byte, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	logger := logging.FromContext(ctx).With(slog.String("component", "script"), slog.String("command", command))
	logger.Debug("executing script")

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/c", command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", command)
	}

	// If the script exits with a non-zero code, stderr will be used to populate an error.
	// Otherwise, we ensure that the output will at least be logged.
	defer func() {
		if stderr.Len() > 0 {
			logger.Warn("script stderr", slog.String("output", stderr.String()))
		}
	}()

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		details := stderr.String()
		stderr.Reset()

		return nil, fail.DetailedError{
			Summary: "Script failed",
			Details: details,
			Parent:  err,
		}
	}

	return stdout.Bytes(), nil
}
