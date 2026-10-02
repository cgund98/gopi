# gopi documentation

gopi is an extensible personal coding agent: a terminal UI over
[gogent](https://github.com/cgund98/gogent), with tools that read and edit a
workspace, run a sandboxed shell, and delegate work to subagents.

New here? Start with the [Quickstart](guides/quickstart.md).

## Guides

Task-focused pages. Each one gets you to a result.

- [Quickstart](guides/quickstart.md) — install, connect a model, run your first task.
- [Choose a model](guides/choose-a-model.md) — pick a provider and a thinking effort.
- [Configure network access](guides/configure-network.md) — allowlist, deny, and per-call elevation.
- [Manage secrets](guides/manage-secrets.md) — store credentials and keep them out of the model.
- [Use project instructions](guides/use-project-instructions.md) — `AGENTS.md` and the user prompt.
- [Write a skill](guides/write-a-skill.md) — add reusable instructions gopi can load on demand.
- [Add a custom tool](guides/add-a-custom-tool.md) — extend gopi in your own Go program.
- [Build with gopi as a library](guides/build-with-gopi.md) — embed the agent with `gopi.Run`.
- [Use gopi-tools and egopi](guides/use-gopi-tools.md) — install egopi and build a subset of its tools.
- [Resume a session](guides/resume-a-session.md) — reopen a saved chat and its grants.
- [Review changes](guides/review-changes.md) — keep or revert each edit gopi made, as a diff.

## Concepts

Why gopi behaves the way it does.

- [How gopi works](concepts/how-gopi-works.md) — the host process, the agent loop, and the layers.
- [Sandboxing](concepts/sandboxing.md) — the profile around a command, network modes, and fail-closed behavior.
- [Permissions and approval](concepts/permissions-and-approval.md) — per-call approval, read grants, and elevation.
- [Secrets](concepts/secrets.md) — the broker and redaction.
- [Instructions](concepts/instructions.md) — how the system prompt is assembled.
- [Skills](concepts/skills.md) — the catalog, the body, and untrusted content.
- [Subagents](concepts/subagents.md) — `explore`, `delegate`, and the child policy.
- [Security model](concepts/security-model.md) — principles, the threat model, and the escape tests.

## Reference

Lookups. Exact keys, flags, names, and paths.

- [CLI](reference/cli.md) — arguments and flags.
- [Configuration](reference/configuration.md) — every key in `config.toml`.
- [Slash commands](reference/slash-commands.md) — in-chat commands.
- [Tools](reference/tools.md) — every built-in tool, its arguments, and when it pauses.
- [gopi-tools](reference/gopi-tools.md) — the extension module and the egopi binary.
- [Models](reference/models.md) — supported model names, context windows, and prices.
- [Environment variables](reference/environment-variables.md) — `GOPI_HOME`, provider keys, and `.env` files.
- [File locations](reference/file-locations.md) — the `~/.gopi` layout and saved sessions.

## Troubleshooting

- [Troubleshooting](troubleshooting.md) — access denied, network denied, and startup failures.

## How these docs are organized

Guides teach, Concepts explain, and Reference states facts — one job per page.
