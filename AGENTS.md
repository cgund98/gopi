# Project Instructions

Gopi is a personal coding agent: a terminal UI over the gogent library. Start with [README.md](README.md) and [ARCHITECTURE.md](ARCHITECTURE.md). Gogent's own guide is [gogent/AGENTS.md](../gogent/AGENTS.md).

## Layout

| Path | Role |
|------|------|
| `cmd/gopi` | Process entrypoint |
| `internal/app` | Wires config, tools, and one gogent agent |
| `internal/config` | `~/.gopi/config.toml`, `system.md`, and `OPENAI_API_KEY` |
| `internal/prompt` | Built-in system prompt |
| `internal/trust` | Workspace trust decisions in `~/.gopi/trust.json` |
| `internal/workspace` | Canonical workspace root and path confinement |
| `internal/tools` | `read_file`, `grep`, `find`, and `edit_file` |
| `internal/tui` | Bubble Tea chat, adapted from `gogent/examples/tui` |

Sandbox, secrets, skills, and per-call approval are later milestones described in [ARCHITECTURE.md](ARCHITECTURE.md). Do not add them while finishing the session slice.

## Working rules

- Keep provider wire format in gogent. Gopi passes the system prompt through `openai.NewChat(...).WithSystemPrompt`.
- Register tools on the same `*gogent.ToolRegistry` pointer passed to `NewAgent`.
- File tools stay inside the workspace root. `edit_file` writes only when the workspace is trusted.
- `RequiresApproval()` stays a boolean until the gogent change in milestone 3.
- Format with `gofmt`. Run `make test` for the packages you touch.
