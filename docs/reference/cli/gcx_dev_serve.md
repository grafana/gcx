## gcx dev serve

Serve Grafana resources locally

### Synopsis

Serve Grafana resources locally.

The server started by this command makes it easy to explore and review resources
locally.

While resources are loaded from disk, the server will use the Grafana instance
described in the current context to access some data (example: to run queries
when previewing dashboards).

Note on NFS/SMB and watch mode: fsnotify requires support from underlying
OS to work. The current NFS and SMB protocols does not provide network level
support for file notifications.


```
gcx dev serve [RESOURCE_DIR]... [flags]
```

### Examples

```

	# Serve resources from a directory:
	gcx dev serve ./resources

	# Serve resources from a directory but don't watch for changes:
	gcx dev serve ./resources --no-watch

	# Serve resources from a script that outputs a YAML resource and watch for changes:
	# Note: the Grafana Foundation SDK can be used to generate dashboards (https://grafana.github.io/grafana-foundation-sdk/)
	gcx dev serve --script 'go run dashboard-generator/*.go' --watch ./dashboard-generator --script-format yaml

```

### Options

```
      --address string         Address to bind (default "0.0.0.0")
  -h, --help                   help for serve
      --max-concurrent int     Maximum number of concurrent operations (default 10)
      --no-watch               Do not watch for changes
      --port int               Port on which the server will listen (default 8080)
  -S, --script string          Script to execute to generate a resource
  -f, --script-format string   Format of the data returned by the script (default "json")
  -w, --watch stringArray      Paths to watch for changes
```

### Options inherited from parent commands

```
      --agent            Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
      --no-color         Disable color output
      --no-truncate      Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count    Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx dev](gcx_dev.md)	 - Manage Grafana resources as code

