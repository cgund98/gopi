# gopi

Extensible personal coding agent.

```bash
export OPENAI_API_KEY=...
go run ./cmd/gopi
```

Optional: `-workspace <dir>`. The default workspace is the current directory. `-resume` opens the newest saved session instead of an empty chat. With `-workspace`, it opens the newest session for that directory. See [ARCHITECTURE.md](ARCHITECTURE.md) for the full design.

Gopi captures the mouse so the wheel scrolls the chat, the plan viewer, and the review file pane. Shift with the up or down arrow scrolls ten lines at a time. To select text, hold Option while dragging in iTerm, or Shift in most other terminals. `/mouse off` hands the mouse back to the terminal for plain selection, and `/mouse on` restores wheel scrolling. `/mouse` alone toggles it.

Pushing to GitHub runs lint, tests, and a format check. Pushes to `main` open a release pull request through release-please. Merging that pull request tags the module and publishes it to the Go module proxy. Actions must be allowed to create and approve pull requests.

## Library

`cmd/gopi` is the built binary. It starts the same program as `gopi.Run` with the built-in tools. Another Go program can add a `gogent.Tool` to a mode. The tool is compiled into that program. The stock binary does not load plugins.

```go
err := gopi.Run(ctx,
    gopi.WithWorkspace(dir),
    gopi.WithTool(gopi.ModeAgent, myTool{}),
    gopi.WithTool(gopi.ModeAsk, myTool{}),
)
```

`WithTool` adds the tool only to that mode. It is not added to the delegate child. A name that matches a built-in tool fails at startup.

A tool that needs credentials uses `WithToolFactory`. Gopi calls the factory at startup with a `ToolEnv`, and `env.Secret(name)` reads that name from `~/.gopi/secrets.toml`. A missing name stops startup. A name a factory reads becomes host-only: it is never offered on a shell approval card. `env.SecretPath(name)` returns the file behind a file secret, for a tool that must write a refreshed token back.

```go
gopi.WithToolFactory(gopi.ModeAgent, func(env gopi.ToolEnv) (gogent.Tool, error) {
    token, err := env.Secret("gcal_token")
    if err != nil {
        return nil, err
    }
    return calendar.New(token), nil
})
```

A custom tool can also implement `gopi.ToolRenderer` to control how its calls look. `Headline` replaces the tool name on the `>` line, `RenderApproval` draws the approval body, and `RenderResult` draws a completed result. Each returns plain text in a `gopi.ToolView` (fields, lines, markdown, or `Hide`), and gopi applies its own frame, colors, and wrapping. An empty headline or a zero view keeps the default. Error results always use gopi's error rendering.

```go
func (t *Tool) Headline(args json.RawMessage) string { return "calendar " + operation(args) }

func (t *Tool) RenderApproval(args json.RawMessage) gopi.ToolView {
    return gopi.ToolView{Fields: []gopi.ToolField{{Label: "Summary", Value: summary(args)}}}
}

func (t *Tool) RenderResult(args, result json.RawMessage) gopi.ToolView {
    return gopi.ToolView{Lines: eventLines(result)}
}
```

## Configuration

Gopi keeps its files in `~/.gopi`, mode `0700`. `GOPI_HOME` overrides that directory. The first launch creates `config.toml`.

```toml
model = "gpt-5.6-terra"
max_iterations = 10

[models]
agent = "gpt-5.6-sol"     # empty uses model
ask = ""
plan = ""
build = ""                # empty uses the agent model; the b key on a plan uses this

[sandbox]
network = "deny"          # deny | allowlist

[sandbox.network]
allow = ["github.com", "proxy.golang.org", "*.npmjs.org"]
deny = []                 # a deny entry beats an allow entry

[instructions]
project_doc_max_bytes = 32768
fallback_files = []       # extra names beside AGENTS.md; empty by default
skill_dirs = []           # each entry is a directory of <name>/SKILL.md

[search]
endpoint = ""             # empty uses Brave Search
```

`network` defaults to `deny`. `allowlist` sends shell traffic through a local proxy and still blocks private and metadata addresses. `unrestricted` is not a config setting. A single shell call can ask for `network_hosts` or `network = "unrestricted"`, and that call waits for approval.

A name without a prefix uses OpenAI. A `kimi/` prefix uses Kimi. Supported names are `gpt-5.6-sol`, `gpt-5.6-terra`, `gpt-5.6-luna`, `gpt-4o`, `kimi/kimi-k2.6`, and `kimi/kimi-k2.6-nothink`. The `-nothink` name is the same Kimi model with thinking disabled, so replies start sooner at some cost to planning quality. The default is `gpt-5.6-terra`. An unknown name fails at startup, and a saved `/model` choice that is no longer supported falls back to the config. Gogent sends GPT-5 models with reasoning off when tools are attached, because Chat Completions rejects function tools otherwise. `kimi_api_key` in `secrets.toml` overrides `KIMI_API_KEY`. A key is required only when a resolved model uses that provider.

`web_search` calls `https://api.search.brave.com/res/v1/web/search` unless `endpoint` is set. Put `search_api_key` in `~/.gopi/secrets.toml`. The shell sandbox stays on its own network setting. `web_fetch` reads one public `http` or `https` URL on the host. It does not use the search key or open a socket inside `shell`.

A `*` in a host pattern matches one DNS label, as in `*.npmjs.org`.

### Prompts and skills

These files are appended after the built-in prompt, in order:

| File | When it loads |
|------|----------------|
| `~/.gopi/system.md` | Always, when the file exists |
| `~/.gopi/AGENTS.md` | Always, when the file exists |
| `AGENTS.md` from the git root down to the workspace | Trusted workspaces only. `AGENTS.override.md` replaces `AGENTS.md` in that directory |
| Skill catalog | Name, description, and path. The skill body stays on disk |

Each of those sections is capped at `project_doc_max_bytes`. The end of the section is kept.

Skills are discovered from:

1. `~/.gopi/skills/<name>/SKILL.md`
2. `<name>/SKILL.md` inside each directory in `skill_dirs`
3. `<workspace>/.gopi/skills/<name>/SKILL.md`, trusted workspaces only

A skill file needs YAML frontmatter with `name` and `description`. A later skill with the same name replaces an earlier one. Restart gopi after adding or changing one.

### Secrets

`~/.gopi/secrets.toml` is mode `0600`. Keys are names. A value is a string, or a table that points at a file:

```toml
jira_api_token = "..."
gcal_token = { file = "~/.secrets/gcal_token.json" }
```

Gopi reads a referenced file once at startup. The file must be mode `0600`. Its path becomes a protected path, so `read_file` asks for approval and the sandboxed shell cannot read it. `openai_api_key` in that file overrides `OPENAI_API_KEY`. Other values are redacted from tool results. For a JSON value, fields whose key names a token, secret, key, or password are redacted on their own too.

Trust decisions for workspaces are stored in `~/.gopi/trust.json`. An untrusted workspace can be read, and it does not load project `AGENTS.md` or project skills.
