# Environment variables

| Variable | Purpose |
|----------|---------|
| `GOPI_HOME` | Override the `~/.gopi` directory. |
| `DEEPSEEK_API_KEY` | DeepSeek API key. |
| `KIMI_API_KEY` | Kimi API key. |
| `ANTHROPIC_API_KEY` | Anthropic API key. |
| `OPENAI_API_KEY` | OpenAI API key. |

A key is required only when a resolved model uses that provider. A matching
entry in `~/.gopi/secrets.toml` overrides the environment variable:
`deepseek_api_key`, `kimi_api_key`, `anthropic_api_key`, and `openai_api_key`.
See [Manage secrets](../guides/manage-secrets.md).

`.env` and `.env.local` in the current directory are loaded before the
environment is read, so a project can carry its own keys. `.env.local` wins over
`.env`, and both lose to `~/.gopi/secrets.toml` and to a variable already set in
your shell.

`search_api_key` has no environment variable. It is read only from
`~/.gopi/secrets.toml`, and only `web_search` uses it.
