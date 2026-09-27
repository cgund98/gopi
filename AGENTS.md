# Project Instructions

Gopi is a personal coding agent: a terminal UI over the gogent library. Start with [README.md](README.md) and the documentation under [docs/src/](docs/src/index.md). Gogent's own guide is [gogent/AGENTS.md](../gogent/AGENTS.md).

## Layout

| Path | Role |
|------|------|
| `gopi.go` | Public API: `Run`, `WithWorkspace`, `WithResume`, `WithTool`, `WithToolFactory` |
| `cmd/gopi` | Built binary. Parses args, then calls `gopi.Run` |
| `internal/app` | Wires config, prompt, tools, and one gogent agent; owns modes and the model factory |
| `internal/config` | `~/.gopi/config.toml`, `system.md`, `GOPI_HOME`, and secret loading |
| `internal/prompt` | Prompt assembly: built-in prompt, `AGENTS.md` chain, skill catalog, mode prefixes |
| `internal/trust` | Workspace trust decisions in `~/.gopi/trust.json` |
| `internal/workspace` | Canonical workspace root and path confinement |
| `internal/tools` | Every built-in tool: file, search, shell, plan, tasks, grants, `delegate` |
| `internal/sandbox` | macOS Seatbelt launcher, scrubbed env, and the network proxy |
| `internal/policy` | Security floor, ignore rules, and network policy |
| `internal/secrets` | `~/.gopi/secrets.toml` loading and redaction |
| `internal/session` | Saved chats under `~/.gopi/sessions` and review baselines |
| `internal/models` | Provider catalog and model-name parsing |
| `internal/review` | Diff computation for `/review` |
| `internal/toolview` | Renderer interface custom tools implement |
| `internal/tui` | Bubble Tea chat, adapted from `gogent/examples/tui` |

The design notes live in the documentation under [docs/src/concepts/](docs/src/concepts/how-gopi-works.md).

## Documentation

Documentation is part of the change, not a follow-up. When a change alters behavior, defaults, names, flags, limits, or the security model, update the matching page in the same change. A stale page is a bug.

| What changed | Update |
|--------------|--------|
| CLI flags or arguments | `reference/cli.md` |
| A `config.toml` key or default | `reference/configuration.md` |
| A model, provider, or context window | `reference/models.md` |
| Environment variables | `reference/environment-variables.md` |
| A file or directory under `~/.gopi` | `reference/file-locations.md` |
| A slash command | `reference/slash-commands.md` |
| A tool's arguments, pausing, or limits | `reference/tools.md` |
| Sandbox, protected paths, network, or bounds | `concepts/sandboxing.md` |
| Approval, read grants, or elevation | `concepts/permissions-and-approval.md` |
| Prompt assembly, `AGENTS.md`, or skills | `concepts/instructions.md`, `concepts/skills.md` |
| Subagent behavior or caps | `concepts/subagents.md` |
| Secrets or redaction | `concepts/secrets.md` |
| A principle, layer, or threat control | `concepts/security-model.md` |
| Tool availability or the turn lifecycle | `concepts/how-gopi-works.md` |
| A task the user performs | The matching page under `guides/` |
| A user-visible feature or install step | `README.md` |

Write what the code does, not what it was meant to do. If the code and a page disagree, fix the page, or make the code match the intent and state which. Document limits and failure modes as plainly as features.

Keep the navigation and descriptions true. Adding, renaming, or removing a page means updating three places together: the nav in `docs/src/SUMMARY.md`, the description in `docs/src/index.md`, and the layout table above if a source file moved. A page's `#` heading should match its `SUMMARY.md` title, and a description should name what the page actually covers.

Build before finishing: `make docs` must succeed, since a broken link or bad include fails the Pages workflow.

## Working rules

- Keep provider wire format in gogent. Gopi passes the system prompt through `openai.NewChat(...).WithSystemPrompt`.
- Register tools on the same `*gogent.ToolRegistry` pointer passed to `NewAgent`.
- File tools stay inside the workspace root. `edit_file` writes only when the workspace is trusted.
- `RequiresApproval(ctx, args)` decides per call, from that call's arguments. Keep it free of side effects.
- Format with `gofmt`. Run `make test` for the packages you touch.
