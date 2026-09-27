# Write a skill

A skill packages reusable instructions. gopi lists the skill's name and
description, then loads the body only when it is needed. See
[Skills](../concepts/skills.md) for why.

## Create a skill

Make a directory with a `SKILL.md`:

```bash
mkdir -p ~/.gopi/skills/go-tests
```

```markdown
---
name: go-tests
description: Run and interpret Go tests in this module.
---

# Go tests

Run tests for the packages you touch, unsandboxed, with the shared module cache:

    go test ./pkg/...

Read the failing assertion before changing the code. Report the first failure,
not every failure.
```

`name` and `description` are required. The body is Markdown.

## Add a script

Optional `scripts/` next to `SKILL.md` are ordinary files. Running them goes
through `shell` and the sandbox, so a skill gets no extra tools and no extra
filesystem roots:

```text
~/.gopi/skills/go-tests/
  SKILL.md
  scripts/
    run.sh
```

## Where gopi looks

1. `~/.gopi/skills/<name>/SKILL.md`
2. `<name>/SKILL.md` inside each directory in `skill_dirs`
3. `<workspace>/.gopi/skills/<name>/SKILL.md`, trusted workspaces only

Point at extra skill directories in `~/.gopi/config.toml`:

```toml
[instructions]
skill_dirs = ["~/notes/skills"]
```

A later skill with the same name replaces an earlier one. Restart gopi after
adding or changing a skill.

## What the model sees

The system prompt lists only the name, description, and path. The model reads a
full body with `read_file` of that path, so a large skill library does not fill
the context window. Project skills from an untrusted workspace are not listed.
