# Review changes

`/review` opens the file edits gopi made in this chat as a diff, so you can keep
or revert each one. It is the checkpoint between the model proposing an edit and
that edit becoming your code.

## What it covers

`edit_file` only. The first time gopi writes a path in a chat, it saves the file's
pre-write contents as a **baseline** under `~/.gopi/sessions/<id>/`, and the
review diffs that baseline against what is on disk now.

It does not cover:

- **`shell` writes.** A `sed -i`, a code generator, or a formatter you ran through
  the sandbox is not tracked.
- **Plan files.** `write_plan` writes under `.gopi/plans` and is not reviewed.
- **Your own edits.** The diff is the baseline against the file on disk, so a
  change you make by hand is part of what you see. Editing a hunk by hand can drop
  it from the list, because a decision is keyed to the hunk's baseline lines.

The baseline is the *first* pre-write copy, so several edits to one file in a chat
are all diffed against the original. You see the net change, not each step.

## Open it

Type `/review` at the composer. The views are:

- **Left — the file tree.** Every changed path, grouped by directory. Paths with
  undecided hunks are listed; a file drops off once you have decided every hunk.
- **Right — the file.** The whole file, with each hunk's old lines drawn above its
  new lines, showing the line numbers on both sides. The focused hunk is
  highlighted, and the header carries an added/deleted line count.

If there is nothing to review, the pane says so.

## Keys

| Key | Action |
|-----|--------|
| `↑` / `↓` | Move between files, in the tree pane |
| `Tab` | Switch between the tree and the file viewer |
| `n` / `p` | Next or previous hunk |
| `a` / `x` | Approve or reject the focused hunk |
| `A` / `X` | Approve or reject every hunk in the file |
| `Esc` | Leave the review pane |
| `Ctrl+C` | Quit |

In the file viewer, `↑` and `↓` scroll, `Shift` with `↑` or `↓` scrolls ten lines,
and `PgUp` and `PgDn` page.

## Approve and reject

**Approve** keeps the file as it is. The hunk is marked decided and leaves the
pending list; nothing is written.

**Reject** reverts the file on disk immediately: the hunk's current lines are
replaced with the baseline lines. This is a real write to your file, not a staged
decision, so a reject is undoable only through your own version control.

A few cases are handled for you:

- **Rejecting a whole file** applies the hunks bottom-up, so earlier hunks keep
  their line positions.
- **Rejecting every hunk of a file gopi created** deletes the file, rather than
  leaving an empty one behind.
- **A file that changed underneath the review** is never partially reverted. If a
  stored hunk no longer matches the file, the reject is refused with
  `hunk no longer matches` and nothing is written.
- **A later edit to a path clears its approvals.** If gopi touches the file again
  after you approved hunks, those hunks come back for review against the new
  content.

The pane closes when the last file is decided, and reopens with `/review`.

## Where the state lives

Hunk decisions are stored in the session file, and baselines in
`~/.gopi/sessions/<id>/`. Both come back with `--resume`, so a review you started
before quitting is still there. See
[File locations](../reference/file-locations.md) and
[Resume a session](resume-a-session.md).

Decisions are keyed to a hunk, not to a line, and the key is anchored to the
baseline. Rejecting one hunk therefore does not rename the hunks that remain.

## Related

- [Permissions and approval](../concepts/permissions-and-approval.md) — the
  approval card an edit can raise before it happens.
- [Tools](../reference/tools.md#edit_file) — `edit_file` and its arguments.
- [Slash commands](../reference/slash-commands.md) — `/review` and the rest.
