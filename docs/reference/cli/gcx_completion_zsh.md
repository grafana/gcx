## gcx completion zsh

Generate the autocompletion script for zsh

### Synopsis

Generate the autocompletion script for the zsh shell.

If shell completion is not already enabled in your environment you will need
to enable it.  You can execute the following once:

	echo "autoload -U compinit; compinit" >> ~/.zshrc

To load completions in your current shell session:

	source <(gcx completion zsh)

To load completions for every new session, execute once:

#### Linux:

	gcx completion zsh > "${fpath[1]}/_gcx"

#### macOS:

	gcx completion zsh > $(brew --prefix)/share/zsh/site-functions/_gcx

You will need to start a new shell for this setup to take effect.


```
gcx completion zsh [flags]
```

### Options

```
  -h, --help              help for zsh
      --no-descriptions   disable completion descriptions
```

### Options inherited from parent commands

```
      --agent            Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --context string   Name of the context to use (overrides current-context in config)
      --no-color         Disable color output
      --no-truncate      Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count    Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx completion](gcx_completion.md)	 - Generate the autocompletion script for the specified shell

