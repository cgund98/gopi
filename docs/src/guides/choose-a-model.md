# Choose a model

gopi defaults to `deepseek/deepseek-flash`. Pick a model in three ways: set a
default in `~/.gopi/config.toml`, override it per mode, or switch it mid-chat
with `/model`. See [Models](../reference/models.md) for the supported names and
prices.

A name without a prefix uses OpenAI, a `kimi/` prefix uses Kimi, a `deepseek/`
prefix uses DeepSeek, and a name that starts with `claude` uses Anthropic (no
prefix). An unknown name fails at startup. A saved `/model` choice that is no
longer supported falls back to the config.

## Set the default

Edit `~/.gopi/config.toml` and set `model`:

```toml
model = "gpt-4o"
```

Then export a key for that provider:

```bash
export OPENAI_API_KEY=...
```

If the file does not exist yet, run gopi once; the first launch creates it. See
[Configuration](../reference/configuration.md) for the full file.

## Override per mode

The `[models]` table sets a different model for each mode. An empty value uses
`model`:

```toml
model = "gpt-4o"          # default for every mode

[models]
agent = "gpt-5.6-sol"     # strongest model for edits
ask   = "gpt-5.6-luna"    # cheap model for questions
plan  = ""                # empty uses model
```

`agent` runs edits and commands, `ask` answers questions, and `plan` writes
plans. The `b` key on a plan builds it with the `build` model. See
[How gopi works](../concepts/how-gopi-works.md) for what each mode can do.

## Switch mid-chat

Type these at the composer while gopi runs:

```
/model                     # list the supported models
/model gpt-4o              # set the model for the active mode
/model kimi/kimi-k2.6      # a prefixed name selects another provider
```

`/model` sets the model for the active mode for the rest of the chat. The model
name shows at the bottom of the screen.

## Reasoning effort

`effort` sets how much the model thinks before it answers:

- `none` turns thinking off. This is the default.
- `low`, `medium`, and `high` turn it up.

Set a default in `~/.gopi/config.toml`:

```toml
effort = "medium"

[efforts]
agent = "high"            # more thinking for edits
ask   = "none"            # fast answers for questions
plan  = ""                # empty uses effort
```

Or change it mid-chat:

```
/effort high
```

DeepSeek and Kimi models enable thinking for any value except `none`. OpenAI
maps the value to `reasoning_effort`. Anthropic maps it to `output_config.effort`
for any value except `none`; there `none` and an empty value leave the model
default in place. The level shows next to the model name at the bottom of the
screen.

## Watch the cost

Keep an eye on the bottom of the screen. gopi shows the turn's token counts and
the running estimated cost for the active model. A turn that uses a cached
prefix costs less. Switching to a cheaper model for reading-heavy work, and
reserving a stronger model for edits, keeps the cost down.

## Provider keys

Each provider needs an API key, from the environment or `~/.gopi/secrets.toml`.
See [Environment variables](../reference/environment-variables.md) and
[Manage secrets](manage-secrets.md).
