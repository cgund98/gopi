# Models

Supported model names, by provider.

| Model | Provider | Context | Input / 1M | Output / 1M |
|-------|----------|---------|-----------|-------------|
| `gpt-5.6-sol` | OpenAI | 272K | $4.00 | $20.00 |
| `gpt-5.6-terra` | OpenAI | 272K | $2.00 | $12.00 |
| `gpt-5.6-luna` | OpenAI | 272K | $0.20 | $1.20 |
| `gpt-4o` | OpenAI | 128K | $2.50 | $10.00 |
| `kimi/kimi-k2.6` | Kimi | 262K | $0.95 | $4.00 |
| `deepseek/deepseek-flash` | DeepSeek (default) | 262K | $0.50 | $2.00 |
| `deepseek/deepseek-v3` | DeepSeek | 262K | $0.90 | $4.00 |
| `claude-opus-5-5` | Anthropic | 1M | $4.00 | $20.00 |
| `claude-sonnet-5-5` | Anthropic | 1M | $2.00 | $10.00 |
| `claude-haiku-4-5` | Anthropic | 200K | $1.00 | $5.00 |

Prices are USD per one million tokens. A cached input token costs less; the
bottom of the screen shows the running estimate.

A name without a prefix uses OpenAI, a `kimi/` prefix uses Kimi, a `deepseek/`
prefix uses DeepSeek, and a name that starts with `claude` uses Anthropic (no
prefix).

An unknown name fails at startup. A saved `/model` choice that is no longer
supported falls back to the config.

Each provider needs an API key. Set it in the environment or in
`~/.gopi/secrets.toml`, where `kimi_api_key`, `deepseek_api_key`, `anthropic_api_key`,
and `openai_api_key` override `KIMI_API_KEY`, `DEEPSEEK_API_KEY`,
`ANTHROPIC_API_KEY`, and `OPENAI_API_KEY`. A key is required only when a resolved
model uses that provider.

See [Choose a model](../guides/choose-a-model.md) to set the default and control
reasoning effort.
