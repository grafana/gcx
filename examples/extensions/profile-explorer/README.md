# profile-explorer

An interactive flamegraph explorer for Pyroscope profiles, in the terminal.

This is the third worked example for the extension mechanism
([ADR-023](../../../docs/adrs/extensions/001-third-party-extensions-design.md)), and the
first one that is a long-running TUI rather than a one-shot command. It holds no
credentials and speaks no Pyroscope API: every fetch is a
`gcx datasources pyroscope …` call back through the gcx binary that dispatched
it.

The flamegraph model - levels of absolute offsets, zoom as a root frame,
sub-cell frames skipped while navigating - follows
[lptm](https://github.com/simonswine/lptm), and the frame colours are keyed by
package the same way `@grafana/flamegraph` does, so a function is the same
colour here as in the Grafana panel.

## Install

```bash
go build -o gcx-ext-profile-explorer .
gcx ext install .
gcx ext profile-explorer
```

Pick a datasource, pick a service, and it loads the CPU profile. Or skip the
pickers:

```bash
gcx ext profile-explorer -d grafanacloud-profiles \
  --expr '{service_name="frontend"}' \
  --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --since 3h
```

`--context` belongs to gcx, so it goes before `ext`:

```bash
gcx --context prod ext profile-explorer
```

## Key bindings

Press `?` in any view for this list.

Lists filter as you type, so letters and digits go into the filter rather than
acting as bindings there.

| Key | Action | Where |
|-----|--------|-------|
| `↑` / `↓` | Move | lists |
| letters, digits | Filter as you type | lists |
| `backspace` | Delete a filter character | lists |
| `enter` | Open a datasource, load a profile, or zoom in | anywhere |
| `j` / `k` | Callee / caller | flamegraph |
| `h` / `l` | Previous / next sibling frame | flamegraph |
| `z` / `o` | Zoom into the selected frame / zoom out one level | flamegraph |
| `0` | Reset zoom | flamegraph |
| `/` then `n` / `N` | Search frames, cycle matches | flamegraph |
| `T` | Toggle the top-functions table | flamegraph |
| `r` | Re-run the query | flamegraph |
| `ctrl+p` / `p` | Choose profile type | lists / flamegraph |
| `ctrl+e` / `e` | Edit the label selector | lists / flamegraph |
| `ctrl+t` / `t` | Cycle time range (15m, 1h, 3h, 6h, 24h) | lists / flamegraph |
| `esc` | Clear the filter, go back, or quit from the datasource list | anywhere |
| `ctrl+c` | Quit | anywhere |

Zoom is scoped: percentages, the top-functions table, and search all count
against the zoomed frame rather than the whole profile.

## Non-interactive use

With no terminal attached, or when the parent gcx is in agent mode
(`GCX_EXT_AGENT_MODE=true`), there is nothing to interact with, so it prints the
heaviest functions as JSON instead. `--no-tui` forces that path:

```bash
gcx ext profile-explorer -d grafanacloud-profiles \
  --expr '{service_name="frontend"}' --top 10 --no-tui
```

```json
{
  "datasource": "grafanacloud-profiles",
  "profileType": "process_cpu:cpu:nanoseconds:cpu:nanoseconds",
  "query": "{service_name=\"frontend\"}",
  "since": "1h",
  "unit": "nanoseconds",
  "total": 8890000000,
  "top": [
    { "name": "internal/runtime/syscall/linux.Syscall6", "self": 1300000000, "total": 1300000000, "selfPercent": 14.62, "totalPercent": 14.62 }
  ]
}
```

## What it does not do

- One label selector at a time. No comparison or diff view.
- No timeline, heatmap, or span exemplars - `gcx datasources pyroscope metrics`
  and `exemplars` expose the data, but this example stops at the flamegraph.
- `--max-nodes` is pinned to 8192. A terminal row holds a few hundred cells, so
  the server default of 50000 is latency for nodes that can never be rendered.
