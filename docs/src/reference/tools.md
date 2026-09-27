# Tools

Every tool the model can call, the arguments it takes, and whether it pauses for
approval. Tools are registered per [mode](slash-commands.md); only the ones in the
active mode are offered to the model.

## Availability

| Tool | Agent | Ask | Plan | Subagent |
|------|:-----:|:---:|:----:|:--------:|
| [`read_file`](#read_file) | yes | yes | yes | yes |
| [`grep`](#grep) | yes | yes | yes | yes |
| [`find`](#find) | yes | yes | yes | yes |
| [`grant_read`](#grant_read) | yes | yes | yes | no |
| [`shell`](#shell) | yes | no | yes | yes |
| [`edit_file`](#edit_file) | yes | no | no | no |
| [`write_plan`](#write_plan) | yes | no | yes | no |
| [`tasks`](#tasks) | yes | no | no | no |
| [`web_search`](#web_search) | yes | yes | yes | no |
| [`web_fetch`](#web_fetch) | yes | yes | yes | no |
| [`delegate`](#delegate) | yes | no | no | no |

A custom tool added with `WithTool` or `WithToolFactory` is registered for the
mode you name, and is not added to a subagent. See
[Add a custom tool](../guides/add-a-custom-tool.md).

Each tool decides for itself whether a call needs approval, based on that call's
arguments. See [Permissions and approval](../concepts/permissions-and-approval.md).

## read_file

Read a file, in whole or in a line window.

| Argument | Type | Required | Description |
|----------|------|----------|-------------|
| `path` | string | yes | Path relative to the workspace root. |
| `offset` | int | no | 1-based line to start from. |
| `limit` | int | no | Maximum lines to return. |

Returns `content`, plus `start_line`, `end_line`, `total_lines`. When the window
stops short, it also returns `truncated` and `next_offset`; pass `next_offset` as
`offset` to continue.

Pauses for a protected path or a path outside the workspace, unless a read grant
covers it.

## grep

Search file contents for a substring.

| Argument | Type | Required | Description |
|----------|------|----------|-------------|
| `pattern` | string | yes | Substring to search for. Not a regex. |
| `path` | string | no | File or directory to search. Defaults to the workspace. |
| `read_paths` | string[] | no | Protected paths, or paths outside the workspace, to include. |

Files are matched by substring, not regular expression. Binary files are skipped
and counted in `binary_files_skipped`. Matching lines are clipped to about 300
runes centered on the match. Results stop at 50 matches or 32 KiB, whichever
comes first, and set `truncated`.

Denied paths come back in `denied` under an `access_denied` entry naming the rule,
not the file contents. `read_paths` pauses for approval and is the way to include
a blocked path.

## find

List files by path substring.

| Argument | Type | Required | Description |
|----------|------|----------|-------------|
| `pattern` | string | no | Substring matched against the workspace-relative path. Omit to list files. |
| `path` | string | no | Directory to search. Defaults to the workspace. |
| `read_paths` | string[] | no | Protected paths, or paths outside the workspace, to include. |

Returns up to 50 paths and sets `truncated` when it stops early. Same approval and
`denied` behavior as `grep`.

## grant_read

Ask once to read a file or directory for the rest of the chat.

| Argument | Type | Required | Description |
|----------|------|----------|-------------|
| `path` | string | yes | File or directory outside the workspace. |

Approving adds the path to the session's read grants. Later `read_file`, `grep`,
`find`, and sandboxed `shell` calls use it without another prompt. `/allowpath`
does the same thing from the composer.

Limits: it is read-only, it does not open a protected child such as an `.env`
inside a granted directory, and a subagent cannot call it. See
[Read grants](../concepts/permissions-and-approval.md#read-grants).

## shell

Run a command in the sandbox.

| Argument | Type | Required | Description |
|----------|------|----------|-------------|
| `command` | string | yes | Command to run, through `/bin/sh -c`. |
| `cwd` | string | no | Working directory relative to the workspace. |
| `profile` | string | no | `sandbox` (default), `extra_paths`, or `unsandboxed`. |
| `read_paths` | string[] | no | Extra files or directories to read. |
| `write_paths` | string[] | no | Extra files or directories to write. |
| `network_hosts` | string[] | no | Extra hosts the command may reach through the proxy. |
| `network` | string | no | `unrestricted` to allow all outbound network. |

Runs unattended when the call stays inside the default profile: the workspace as a
write root, protected paths denied, and no network. It pauses when the call asks
for anything more — `read_paths`, `write_paths`, `network_hosts`, `network:
unrestricted`, or `profile: unsandboxed`.

Returns `exit_code`, `stdout`, `stderr`, and `truncated`. Output is capped at
64 KiB per stream and the command times out after 30 seconds; on timeout the whole
process group is killed. When a command fails because the sandbox blocked a file
or the network, the result may include `blocked_paths`, `blocked_hosts`, and a
`message` suggesting a retry with the matching argument.

`extra_paths` is accepted only with `read_paths` or `write_paths`. The shell
sandbox is macOS-only: a sandboxed command on another platform returns
`access_denied` rather than running unsandboxed. See
[Sandboxing](../concepts/sandboxing.md).

## edit_file

Replace an exact snippet, or create a file.

| Argument | Type | Required | Description |
|----------|------|----------|-------------|
| `path` | string | yes | Path relative to the workspace root. |
| `old` | string | yes | Exact text to replace. Empty creates a new file. |
| `new` | string | yes | Replacement text. |

`old` must match exactly once. Zero matches returns an error; more than one returns
an error naming the count. An empty `old` creates a new file, and is refused when
the file already exists. Refused outright when the workspace is untrusted.

Pauses for a protected path or a write outside the workspace. Each successful
first write of a path is recorded so `/review` can show the change.

## write_plan

Create or update a plan under `<workspace>/.gopi/plans`. Two tools share this
name: Agent mode can only update an existing plan, and Plan mode can create one.

| Argument | Type | Required | Description |
|----------|------|----------|-------------|
| `plan_name` | string | yes | Short name for a new plan file. |
| `body` | string | yes | Markdown plan, without the todo frontmatter. |
| `path` | string | no | An existing plan under `.gopi/plans` to overwrite. |
| `todos` | Task[] | no | Implementation steps, as frontmatter. |

Omit `path` to create `<workspace>/.gopi/plans/<plan_name>-<uuid>.md`. Pass a
`path` to overwrite that file; it must exist, be under `.gopi/plans`, and end in
`.md`. Keeping a plan in the workspace does not ask for approval, and an untrusted
workspace is refused. Saving a plan adds `.gopi/plans` to the workspace-root
`.gitignore` when that file already exists.

## tasks

Patch the session checklist.

| Argument | Type | Required | Description |
|----------|------|----------|-------------|
| `clear` | bool | no | Drop every current task. |
| `remove` | string[] | no | Ids to drop. |
| `update` | TaskUpdate[] | no | Change status or content of existing ids. |
| `add` | Task[] | no | Append tasks. An existing id is updated instead. |

A call can combine these, applied in the order `clear`, `remove`, `update`, `add`.
A `Task` has `id`, `content`, and an optional `status` of `pending`,
`in_progress`, `completed`, or `canceled`; the default is `pending`. At most one
task is `in_progress`. An unknown id in `remove` or `update` is an error.

Agent mode only. The list is in-memory for the session and is not written to disk.

## web_search

Search the public web.

| Argument | Type | Required | Description |
|----------|------|----------|-------------|
| `query` | string | yes | Search query. |

Returns up to 5 results with a title, URL, and a snippet clipped to about 300
runes. It does not fetch the pages. Needs `search_api_key` in
`~/.gopi/secrets.toml`, and calls `https://api.search.brave.com/res/v1/web/search`
unless `[search] endpoint` is set. Never pauses; snippets are untrusted text.

## web_fetch

Read one public page.

| Argument | Type | Required | Description |
|----------|------|----------|-------------|
| `url` | string | yes | http or https URL of one public page. |

Runs in the host, not through `shell`. The URL must have no user info and a port
of 80, 443, or none, and the host must resolve to a public address — loopback,
private, link-local, CGNAT, multicast, and cloud metadata addresses are refused. A
redirect to a different host is refused. HTML is reduced to title and text,
capped at 8000 runes and 1 MiB. Never pauses; page text is untrusted.

## delegate

Hand a bounded investigation to a subagent.

| Argument | Type | Required | Description |
|----------|------|----------|-------------|
| `task` | string | yes | A self-contained question for the subagent. |

Returns the child's `answer`, a `tool_calls` count, and any `denied` summaries.
Never pauses: a call the child would need approval for fails with `access_denied`
instead. The child has `read_file`, `grep`, `find`, and `shell` only, and gets 50
turns, 2 minutes, and a session budget of 4 delegate calls. See
[Subagents](../concepts/subagents.md).

