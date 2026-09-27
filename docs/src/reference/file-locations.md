# File locations

gopi keeps its files in `~/.gopi`, mode `0700`. `GOPI_HOME` overrides the
directory.

```text
~/.gopi/                  # 0700
  config.toml             # model, sandbox defaults, network allowlist
  system.md               # optional user system prompt
  AGENTS.md               # optional global instructions
  ignore                  # global ignore patterns
  secrets.toml            # 0600, secret name = string or { file = "..." }
  skills/                 # <name>/SKILL.md
  sessions/               # saved chats
  audit.log               # tool-call log
  trust.json              # workspace trust decisions
```

Project files live in the repository and load only after you trust the
workspace:

```text
<repo>/.gopi/skills/      # <name>/SKILL.md
<repo>/.gopi/plans/       # plans written by write_plan
<repo>/AGENTS.md          # project instructions
```

There is no project `config.toml`. Configuration is read from `~/.gopi/config.toml`
only, so a repository cannot change the model, the sandbox, or the network rules.

## Permissions

gopi refuses to start if `~/.gopi` is group- or world-readable. `secrets.toml`
and any file it references must be mode `0600`.

## Saved sessions

Each chat is one JSON file:

```text
~/.gopi/sessions/
  <id>.json               # transcript, mode, model, efforts, read grants, review
  <id>/                   # review baselines, one file per edited path
```

The newest 50 chats are kept. `--resume` and `/sessions` reopen them, and read
grants are restored with the chat. See
[Resume a session](../guides/resume-a-session.md).

## Project dotenv files

`.env` and `.env.local` in the current directory are read for provider keys. They
are not gopi configuration. See
[Environment variables](environment-variables.md).
