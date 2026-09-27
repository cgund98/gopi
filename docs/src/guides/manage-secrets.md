# Manage secrets

Store credentials so a tool can use them without the model ever seeing the
value. See [Secrets](../concepts/secrets.md) for the model.

## Write the file

`~/.gopi/secrets.toml` is mode `0600`. Keys are names. A value is a string or a
table that points at a file:

```toml
jira_api_token = "..."
gcal_token = { file = "~/.secrets/gcal_token.json" }
```

Create it and lock down the mode:

```bash
touch ~/.gopi/secrets.toml
chmod 600 ~/.gopi/secrets.toml
```

gopi reads a referenced file once at startup. The file must be mode `0600`:

```bash
chmod 600 ~/.secrets/gcal_token.json
```

Its path becomes a protected path, so `read_file` asks for approval and the
sandboxed shell cannot read it.

## Provider keys

A provider key can live in the environment or in `secrets.toml`. The matching
name in `secrets.toml` overrides the environment variable:

| `secrets.toml` key | Environment variable |
|--------------------|----------------------|
| `openai_api_key` | `OPENAI_API_KEY` |
| `kimi_api_key` | `KIMI_API_KEY` |
| `deepseek_api_key` | `DEEPSEEK_API_KEY` |

```toml
deepseek_api_key = "sk-..."
```

A key is required only when a resolved model uses that provider. See
[Choose a model](choose-a-model.md).

## Redaction

Other values are redacted from tool results. For a JSON value, fields whose key
names a token, secret, key, or password are redacted on their own too. So an
OAuth token file stays covered field by field, even when the file has fields you
did not list.

Values are read by the host process and never reach the model, a tool argument,
or a log line. Redaction is a backstop: the primary control is that the child
environment does not contain the value.

## Read a secret in a tool

To read a secret in a custom tool, use `ToolEnv.Secret` and
`ToolEnv.SecretPath`. See [Add a custom tool](add-a-custom-tool.md).
