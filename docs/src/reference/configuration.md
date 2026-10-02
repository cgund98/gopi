# Configuration

gopi keeps its files in `~/.gopi`, mode `0700`. `GOPI_HOME` overrides that
directory. The first launch creates `config.toml`. See
[File locations](file-locations.md) for the full directory.

```toml
model = "deepseek/deepseek-flash"
max_iterations = 50
effort = "none"           # none | low | medium | high

[models]
agent = "gpt-5.6-sol"     # empty uses model
ask = ""
plan = ""
build = ""                # empty uses the agent model; the b key on a plan uses this
explore = ""              # empty uses the active mode's model

[efforts]
agent = ""                # empty uses effort
ask = ""
plan = ""                 # the b key on a plan uses the plan effort
build = ""

[sandbox]
network = "deny"          # deny | allowlist
respect_gitignore = false # true adds the repo's .gitignore to the deny set

[sandbox.network]
allow = ["github.com", "proxy.golang.org", "*.npmjs.org"]
deny = []                 # a deny entry beats an allow entry

[instructions]
project_doc_max_bytes = 32768
fallback_files = []       # extra names beside AGENTS.md; empty by default
skill_dirs = []           # each entry is a directory of <name>/SKILL.md

[search]
endpoint = ""             # empty uses Brave Search
engine = "auto"           # auto | ripgrep | builtin
ripgrep_path = ""         # empty looks for rg on PATH

[subagents]
explore_max_calls = 6     # explore calls allowed per session
explore_iterations = 40   # model turns per explore subagent
explore_timeout_seconds = 120
```

## Keys

| Key | Default | Purpose |
|-----|---------|---------|
| `model` | `deepseek/deepseek-flash` | Default model name. See [Models](models.md). |
| `max_iterations` | `50` | Cap on model turns per task. |
| `effort` | `none` | Reasoning effort: `none`, `low`, `medium`, `high`. |
| `[models]` | empty | Per-mode model override, including `explore`. Empty uses `model`. |
| `[efforts]` | empty | Per-mode effort override. Empty uses `effort`. |
| `[sandbox] network` | `deny` | `deny` or `allowlist`. See [Sandboxing](../concepts/sandboxing.md#network). |
| `[sandbox] respect_gitignore` | `false` | Add the repo's `.gitignore` and `.git/info/exclude` to the deny set. See [Protected paths](../concepts/sandboxing.md#protected-paths). |
| `[sandbox.network]` | empty | `allow` and `deny` host lists. A `deny` entry beats an `allow` entry. A `*` matches one DNS label. |
| `[instructions] project_doc_max_bytes` | `32768` | Cap on each instruction section. |
| `[instructions] fallback_files` | empty | Extra instruction filenames beside `AGENTS.md`. |
| `[instructions] skill_dirs` | empty | Directories of `<name>/SKILL.md`. |
| `[search] endpoint` | empty | Search endpoint. Empty uses Brave Search. |
| `[search] engine` | `auto` | `grep` backend: `auto`, `ripgrep`, or `builtin`. See [Tools](tools.md#grep). |
| `[search] ripgrep_path` | empty | ripgrep binary. Empty looks for `rg` on `PATH`. |
| `[subagents] explore_max_calls` | `6` | `explore` calls allowed per session. |
| `[subagents] explore_iterations` | `40` | Model turns allowed per `explore` subagent. |
| `[subagents] explore_timeout_seconds` | `120` | Wall-clock cap on one `explore` subagent. |

`[search] ripgrep_path` that does not resolve is an error only when
`engine = "ripgrep"`, which asks for that binary by name. Under `auto`, a missing
or broken `rg` is not fatal: `grep` falls back to the built-in walker and reports
`backend: builtin` in its result. An unknown `engine` value fails at startup, the
way an unknown model name does.

An empty `[models] explore` means the explore subagent uses whatever model the
active mode is using, so by default it costs the same per token as the parent.

`web_search` calls `https://api.search.brave.com/res/v1/web/search` unless
`endpoint` is set. Put `search_api_key` in `~/.gopi/secrets.toml`. `web_fetch`
reads one public `http` or `https` URL on the host; it does not use the search
key or open a socket inside `shell`.

`max_iterations` counts model turns, not tool calls. Resolving a tool, including
an approval you have to answer, does not spend the budget, so a paused card does
not shorten the turn.

## Precedence

```
built-in defaults  <  ~/.gopi/config.toml
```

There is no project configuration file. `config.toml` is read from `~/.gopi`
only, so a repository cannot change the model, the sandbox, the network rules, or
the security floor. What a repository can ship is instructions and skills, which
load only in a trusted workspace and cannot widen any of the above. See
[Instructions](../concepts/instructions.md).

## Custom tool tables

A `WithToolFactory` tool can read its own top-level table. Built-in tables
(`model`, `models`, `efforts`, `sandbox`, `instructions`, `search`) are not
available this way, and a project `.gopi/config.toml` is not loaded, so a custom
table must be in `~/.gopi/config.toml`. See
[Add a custom tool](../guides/add-a-custom-tool.md).
