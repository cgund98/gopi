# CLI

```
gopi [flags] [workspace]
```

## Arguments

| Argument | Default | Description |
|----------|---------|-------------|
| `workspace` | current directory | Directory to work in. A leading `~` expands to your home directory. |

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--resume` | `false` | Open the newest saved session for the workspace instead of an empty chat. |

```bash
gopi                        # work in the current directory
gopi ~/code/app             # work in another directory
gopi --resume               # reopen the newest session for this directory
gopi ~/code/app --resume    # reopen the newest session for that directory
```

Flags come before the workspace. A single `-` also works (`-resume`), matching
Go's flag syntax. Passing more than one workspace is an error.

A workspace of `~` or `~/path` expands to your home directory, so `gopi ~/code/app`
works as it does in a shell.

With a workspace argument, `--resume` opens the newest session for that
directory.
