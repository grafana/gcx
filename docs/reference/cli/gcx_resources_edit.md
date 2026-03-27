## gcx resources edit

Edit resources from Grafana

### Synopsis

Edit resources from Grafana using the default editor.

This command allows the edition of any resource that can be accessed by this CLI tool.

It will open the default editor as configured by the EDITOR environment variable, or fall back to 'vi' for Linux or 'notepad' for Windows.
The editor will be started in the shell set by the SHELL environment variable. If undefined, '/bin/bash' is used for Linux or 'cmd' for Windows.

The edition will be cancelled if no changes are written to the file or if the file after edition is empty.


```
gcx resources edit RESOURCE_SELECTOR [flags]
```

### Examples

```

	# Editing a dashboard
	gcx resources dashboard/foo

	# Editing a dashboard in JSON
	gcx resources -o json dashboard/foo

	# Using an alternative editor
	EDITOR=nvim gcx resources dashboard/foo

```

### Options

```
  -h, --help            help for edit
      --json string     Comma-separated list of fields to include in JSON output, or '?' to discover available fields
  -o, --output string   Output format. One of: json, yaml (default "json")
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

* [gcx resources](gcx_resources.md)	 - Manipulate Grafana resources

