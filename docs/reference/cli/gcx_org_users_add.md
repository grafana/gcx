## gcx org users add

Add a user to the current organization.

```
gcx org users add [flags]
```

### Options

```
  -h, --help           help for add
      --login string   Login or email of the user to add (required)
      --role string    Role for the user, e.g. Admin, Editor, Viewer (required)
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string      Path to the configuration file to use
      --context string     Name of the context to use
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx org users](gcx_org_users.md)	 - Manage users in the current organization.

