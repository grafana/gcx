## gcx config edit

Open a config file in $EDITOR

### Synopsis

Open a config file in your editor. If multiple config files are loaded,
specify which one to edit: system, user, or local.

If only one config file exists, it is opened directly.

```
gcx config edit [type] [flags]
```

### Options

```
      --create   Create the config file if it doesn't exist
  -h, --help     help for edit
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

* [gcx config](gcx_config.md)	 - View or manipulate configuration settings

