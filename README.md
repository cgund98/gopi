# gopi

Extensible personal coding agent.

```bash
export OPENAI_API_KEY=...
go run ./cmd/gopi
```

Optional: `-workspace <dir>`. The default workspace is the current directory. See [ARCHITECTURE.md](ARCHITECTURE.md) for the full design.

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

## Configuration

Gopi keeps its files in `~/.gopi`, mode `0700`. `GOPI_HOME` overrides that directory. The first launch creates `config.toml`.

```toml
model = "gpt-4o-mini"
max_iterations = 10

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

`web_search` calls `https://api.search.brave.com/res/v1/web/search` unless `endpoint` is set. Put `search_api_key` in `~/.gopi/secrets.toml`. The shell sandbox stays on its own network setting.

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

`~/.gopi/secrets.toml` is mode `0600`. Keys are names and values are strings. `openai_api_key` in that file overrides `OPENAI_API_KEY`. Other values are redacted from tool results.

Trust decisions for workspaces are stored in `~/.gopi/trust.json`. An untrusted workspace can be read, and it does not load project `AGENTS.md` or project skills.
