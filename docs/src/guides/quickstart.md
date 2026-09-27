# Quickstart

Install gopi, connect a model, start it in a workspace, and complete a first task.

## Install

gopi needs Go 1.25 or later. Install the binary from the module proxy:

```bash
go install github.com/cgund98/gopi/cmd/gopi@latest
```

To build from source instead, clone the repository and build the binary:

```bash
git clone https://github.com/cgund98/gopi
cd gopi
go build ./cmd/gopi
```

## Connect a model

gopi defaults to `deepseek/deepseek-flash`, which needs a DeepSeek API key. Save
it in `~/.gopi/secrets.toml` so it stays out of your shell environment:

```bash
mkdir -p ~/.gopi && chmod 700 ~/.gopi
echo 'deepseek_api_key = "..."' > ~/.gopi/secrets.toml
chmod 600 ~/.gopi/secrets.toml
```

The directory must be mode `0700` and the file mode `0600`; gopi refuses to
start otherwise. A key in `secrets.toml` overrides the matching environment
variable.

To use a different provider, add its key the same way and set the model in
`~/.gopi/config.toml`. You need a key only for the provider a resolved model
uses. See [Manage secrets](manage-secrets.md) and
[Choose a model](choose-a-model.md).

## Start gopi

Start gopi in the directory you want it to work in:

```bash
cd /path/to/project
gopi
```

The default workspace is the current directory. Pass a directory to point at
another one. `--resume` opens the newest saved session for that workspace
instead of an empty chat. See [CLI](../reference/cli.md) and
[Resume a session](resume-a-session.md).

The first launch creates `~/.gopi/config.toml`.

## Trust the workspace

The first time you open a directory, gopi asks whether to trust it. Press `y` to
trust it.

- **Trusted** — project instructions and skills load. Writes stay inside the
  workspace.
- **Untrusted** — read tools run sandboxed, and project `AGENTS.md`, skills, and
  `.gopi/config.toml` do not load.

A path outside the workspace, or a protected path inside it, pauses for approval.
Approve to read that one file, or use `/allowpath` and `grant_read` to open a
directory for the rest of the chat. See
[Permissions and approval](../concepts/permissions-and-approval.md).

## Give it a task

Type a prompt at the composer and press Enter. Press Shift+Enter to add a
newline.

gopi reads and edits files, runs commands in a sandbox, and delegates work to
subagents. The shell sandbox is macOS-only; on other platforms a sandboxed
command is refused. See [Tools](../reference/tools.md).

## Approve what it asks

gopi pauses before a protected path or a shell call that needs more than the
default sandbox. The approval card shows the command, the directory, and what
changes. Approve to run that one call; gopi asks again on the next one. See
[Permissions and approval](../concepts/permissions-and-approval.md).

## Switch modes

- **Agent** (default) — read, edit, and run commands.
- **Ask** — read-only. Answers questions about the workspace and the web.
- **Plan** — explores and writes a plan to `.gopi/plans`. It does not edit
  project files. In the plan view, press `b` to build the plan. Accepting a plan
  does not apply it; switch to Agent mode to apply it.

Switch with `/agent`, `/ask`, `/plan`, or `/mode <name>`. See
[Slash commands](../reference/slash-commands.md).

## Scroll and select text

gopi captures the mouse, so the wheel scrolls the chat, the plan viewer, and the
review pane. To select text, hold Option while dragging in iTerm, or Shift in
most other terminals.

- `/mouse off` hands the mouse to the terminal for plain selection.
- `/mouse on` restores wheel scrolling.
- `/mouse` toggles it.
- Shift with the up or down arrow scrolls ten lines.

## Next steps

- [Choose a model](choose-a-model.md)
- [Configure network access](configure-network.md)
- [Use project instructions](use-project-instructions.md)
- [How gopi works](../concepts/how-gopi-works.md)

