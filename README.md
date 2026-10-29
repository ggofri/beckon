# beckon

**Know which AI agent is calling you.**

Run Claude Code, Codex or opencode in a dozen [kitty](https://sw.kovidgoyal.net/kitty/) tabs and you stop knowing which one is waiting for you. beckon colours the tab, lists every agent across all your kitty instances, and puts a count in the macOS menu bar:

| Bar | Meaning |
|---|---|
| 🔴 2 (red) | agents blocked on a permission prompt |
| 🟢 1 (green) | agents idle, waiting for your next prompt |
| 🟡 3 (yellow) | agents working |
| ● (grey) | no agents |

Click an entry to jump to that window.

## Install

```sh
go install github.com/ggofri/beckon@latest      # or: mise use -g "go:github.com/ggofri/beckon"
beckon install                                  # hooks for claude + codex, SwiftBar plugin on macOS
beckon doctor                                   # check everything is wired up
```

## Quick start

1. Enable kitty remote control in `kitty.conf`:
   ```
   allow_remote_control socket-only
   listen_on unix:/tmp/kitty-{kitty_pid}
   ```
2. `beckon install` (use `--dry-run` to preview; it merges into `~/.claude/settings.json` and `~/.codex/hooks.json` and keeps everything else).
3. Restart your agents. Codex may ask you to trust the new hooks.
4. macOS menu bar: `brew install --cask swiftbar`, then `beckon install swiftbar`.

## Commands

| Command | What it does |
|---|---|
| `beckon ls [state] [--json]` | one line per agent: `state  project  tab  agent  window-id  socket` (TAB separated, script/fzf friendly) |
| `beckon focus <socket> <window-id>` | focus that window (and raise kitty on macOS) |
| `beckon hook <working\|blocked\|idle\|release>` | the agent hook: colours the tab and publishes the state |
| `beckon swiftbar` | SwiftBar plugin output |
| `beckon install [claude\|codex\|swiftbar] [--dry-run]` | idempotent setup |
| `beckon doctor` | report what beckon detects; exit 1 if kitty is unreachable |

fzf picker: `beckon ls | fzf --delimiter='\t' --with-nth=1..4 | awk -F'\t' '{print $6, $5}' | xargs beckon focus`

## How it works

State lives in a kitty **window user variable** `agent` ∈ `working | blocked | idle | ""`. Anything can write it (`kitten @ set-user-vars --match id:N agent=idle`); `beckon hook` does it from agent hooks (and tints the tab). beckon reads it through kitty's remote control, across every socket it finds (`$KITTY_LISTEN_ON`, `listen_on` in `kitty.conf`, `/tmp/kitty-*`).

A window only counts while its foreground process is `claude`, `codex` or `opencode`, so a state left behind by a crashed agent disappears. Subagent hooks (`"agent_id"` in the hook JSON) can't flip the main agent to idle.

Event mapping: `PermissionRequest→blocked`, `PreToolUse`/`UserPromptSubmit→working`, `Stop→idle`, `SessionEnd` (codex: `SessionStart`) `→release`.

## Limits

- kitty only; `kitty.conf` `include` lines and Linux abstract sockets are not followed.
- opencode has no hooks, so it only shows if something sets `agent` for it.
- The menu bar needs SwiftBar (polls every 3 s).

## Roadmap

More terminals (wezterm, tmux), desktop notifications, Homebrew tap.

## License

MIT
