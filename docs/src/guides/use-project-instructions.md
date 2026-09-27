# Use project instructions

Tell gopi how your project works. Instructions are appended after the built-in
prompt, so the base behavior always applies. See
[Instructions](../concepts/instructions.md) for the full order.

## Add an AGENTS.md

Write an `AGENTS.md` at the repo root:

```markdown
# Project instructions

- Run `make test` before finishing.
- Keep provider wire format in the `openai` package.
- Use table-driven tests.
```

Commit it to the repo. Under a trusted workspace, gopi loads it on every run. To
scope instructions to a subdirectory, add another `AGENTS.md` there; the closest
file comes last and wins.

## Order

gopi assembles the prompt from these files, in order:

| File | When it loads |
|------|----------------|
| `~/.gopi/system.md` | Always, when the file exists |
| `~/.gopi/AGENTS.md` | Always, when the file exists |
| `AGENTS.md` from the git root down to the workspace | Trusted workspaces only |
| Skill catalog | Name, description, and path. The body stays on disk |

The project chain is concatenated root-first. `AGENTS.override.md` in a
directory replaces `AGENTS.md` in that directory only.

## Add a user-level prompt

For guidance that applies to every project, write `~/.gopi/system.md`:

```markdown
Prefer small commits and explain the change in one sentence.
```

## Tune the limits

```toml
[instructions]
project_doc_max_bytes = 32768   # cap per section
fallback_files = []             # extra names beside AGENTS.md
```

Each section is capped at `project_doc_max_bytes`, and the end of the section is
kept, so the closest file survives truncation.

`fallback_files` adds extra names beside `AGENTS.md`. It is empty by default, so
a `CLAUDE.md` is not loaded unless you opt in:

```toml
[instructions]
fallback_files = ["CLAUDE.md", "CONTRIBUTING.md"]
```

Restart gopi after editing `config.toml`.

Project files load only in a trusted workspace. They are instructions to the
model, not configuration, and they cannot change the sandbox floor. See
[Permissions and approval](../concepts/permissions-and-approval.md).
