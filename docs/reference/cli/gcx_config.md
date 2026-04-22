## gcx config

View or manipulate configuration settings

### Synopsis

View or manipulate configuration settings.

The configuration file to load is chosen as follows:

1. If the --config flag is set, then that file will be loaded. No other location will be considered.
2. If the $GCX_CONFIG environment variable is set, then that file will be loaded. No other location will be considered.
3. If the $XDG_CONFIG_HOME environment variable is set, then it will be used: $XDG_CONFIG_HOME/gcx/config.yaml
   Example: /home/user/.config/gcx/config.yaml
4. If the $HOME environment variable is set, then it will be used: $HOME/.config/gcx/config.yaml
   Example: /home/user/.config/gcx/config.yaml
5. If the $XDG_CONFIG_DIRS environment variable is set, then it will be used: $XDG_CONFIG_DIRS/gcx/config.yaml
   Example: /etc/xdg/gcx/config.yaml


### Options

```
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
  -h, --help             help for config
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --limit int          Maximum number of items to return from list operations (0 for all; defaults to 50 in agent mode)
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
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

