# gopi

An extensible personal coding agent for the terminal.

[![Latest Release](https://img.shields.io/github/v/release/cgund98/gopi)](https://github.com/cgund98/gopi/releases)
[![Go Reference](https://pkg.go.dev/badge/github.com/cgund98/gopi.svg)](https://pkg.go.dev/github.com/cgund98/gopi)
[![Docs](https://img.shields.io/badge/docs-cgund98.github.io%2Fgopi-blue)](https://cgund98.github.io/gopi/)

![gopi summarizing this repository](assets/summarize-repo.gif)

gopi reads and edits a workspace, runs commands in a sandbox, and delegates work
to subagents. It keeps credentials in the host process and pauses for approval
before it reads a protected file or widens the sandbox. If the sandbox cannot be
applied, the command does not run.

## Install

gopi needs Go 1.25 or later.

```bash
go install github.com/cgund98/gopi/cmd/gopi@latest
```

Prebuilt Linux and macOS binaries are attached to each
[release](https://github.com/cgund98/gopi/releases).

## Quickstart

```bash
export DEEPSEEK_API_KEY=...
cd /path/to/project
gopi
```

The default workspace is the current directory. Pass a directory to point at
another one, and `--resume` to reopen the newest saved session. See the
[Quickstart](https://cgund98.github.io/gopi/guides/quickstart.html) for the full
walkthrough.

## Highlights

- **Sandboxed shell** — commands run under Seatbelt on macOS. If the sandbox
  cannot be applied, the command is refused.
- **Approval per call** — a protected path or a wider sandbox profile pauses for
  one approval. gopi asks again on the next call.
- **Secrets stay in the host** — a credential is read by the host process and
  never reaches the model, a tool argument, or a log line.
- **Review every edit** — [`/review`](https://cgund98.github.io/gopi/guides/review-changes.html)
  walks the file edits from the chat as a diff. Approve or reject each hunk, or a
  whole file, and a rejected hunk reverts to the original.
- **Extensible** — add a tool from your own Go program with `gopi.WithTool`, or
  embed the agent with `gopi.Run`.
- **Third-party tools** — the
  [gopi-tools](https://github.com/cgund98/gopi-tools) module packages tools you
  can compile into your own build, and `egopi` is gopi with all of them. See
  [Use gopi-tools](https://cgund98.github.io/gopi/guides/use-gopi-tools.html).

## Documentation

The full documentation is at **[cgund98.github.io/gopi](https://cgund98.github.io/gopi/)**.

- **Guides** — task-focused pages, starting with the
  [Quickstart](https://cgund98.github.io/gopi/guides/quickstart.html).
- **Concepts** — why gopi behaves the way it does, such as
  [Sandboxing](https://cgund98.github.io/gopi/concepts/sandboxing.html) and
  [Permissions and approval](https://cgund98.github.io/gopi/concepts/permissions-and-approval.html).
- **Reference** — the [CLI](https://cgund98.github.io/gopi/reference/cli.html),
  [configuration](https://cgund98.github.io/gopi/reference/configuration.html),
  [slash commands](https://cgund98.github.io/gopi/reference/slash-commands.html),
  and [tools](https://cgund98.github.io/gopi/reference/tools.html).

The pages are markdown under [`docs/src/`](docs/src/index.md). Run `make docs-serve`
to read them locally, or `make docs` to build the site.

## Development

Run gopi from a clone:

```bash
git clone https://github.com/cgund98/gopi
cd gopi
go run ./cmd/gopi
```

Run `make test` for the test suite and `make lint` for the linter. Pushes run
lint, tests, and a format check. Merging a release pull request tags the module,
publishes it to the Go module proxy, and attaches prebuilt Linux and macOS
binaries to the GitHub release.

## License

BSD Zero Clause License. See [LICENSE](LICENSE).
