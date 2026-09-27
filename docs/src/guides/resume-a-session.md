# Resume a session

gopi saves each chat under `~/.gopi/sessions`, keyed by workspace. Reopen the
newest one for a directory with `--resume`.

## Reopen the newest session

Start gopi in the workspace and pass `--resume`:

```bash
cd /path/to/project
gopi --resume
```

gopi opens the newest saved session for that directory instead of an empty
chat. Pass a workspace directory to open the newest session for that directory:

```bash
gopi ~/code/app --resume
```

Without `--resume`, gopi starts an empty chat.

## Pick an older session

Inside gopi, open the saved-chat list:

```
/sessions
```

The list is newest first. Use the up and down arrows to move, Enter to open, and
`x` twice to delete the highlighted chat. Esc closes the list. A saved session
keeps its mode, its per-mode model and effort choices, and its review state.

## What carries over

- **Read grants.** A path granted with `/allowpath` or `grant_read` stays
  readable for the rest of the chat, including after a resume.
- **The transcript.** gopi restores the messages so you can keep going.
- **The review list.** Pending file edits are still there under `/review`. See
  [Review changes](review-changes.md).

Write access and unsandboxed commands always ask again; only read grants
persist. See [Permissions and approval](../concepts/permissions-and-approval.md).

## Where sessions live

```text
~/.gopi/sessions/    # one file per chat, newest first in the list
```

gopi keeps the 50 most recent sessions. See
[File locations](../reference/file-locations.md).
