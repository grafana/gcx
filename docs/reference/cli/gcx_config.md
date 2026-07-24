## gcx config

View or manipulate configuration settings

### Synopsis

View or manipulate configuration settings.

--config or $GCX_CONFIG selects one explicit file and bypasses layering.
Otherwise gcx merges every existing source from lowest to highest priority:

1. System configuration: $XDG_CONFIG_DIRS/gcx/config.yaml (for example, /etc/xdg/gcx/config.yaml).
2. User configuration: $HOME/.config/gcx/config.yaml, then the platform $XDG_CONFIG_HOME fallback.
3. Repository configuration: .gcx.yaml in the current directory.

Credential-bearing stack and Cloud entries are atomic across layers; contexts
merge only their references and datasource defaults.


### Options

```
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
  -h, --help             help for config
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx](gcx.md)	 - Control plane for Grafana Cloud operations
* [gcx config check](gcx_config_check.md)	 - Check the current configuration for issues
* [gcx config current-context](gcx_config_current-context.md)	 - Display the current context name
* [gcx config edit](gcx_config_edit.md)	 - Open a config file in $EDITOR
* [gcx config list-contexts](gcx_config_list-contexts.md)	 - List the contexts defined in the configuration
* [gcx config path](gcx_config_path.md)	 - Show loaded config file paths
* [gcx config set](gcx_config_set.md)	 - Set a single value in a configuration file
* [gcx config unset](gcx_config_unset.md)	 - Unset a single value in a configuration file
* [gcx config use-context](gcx_config_use-context.md)	 - Set the current context
* [gcx config view](gcx_config_view.md)	 - Display the current configuration

