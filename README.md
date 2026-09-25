# gopi

Extensible personal coding agent.

```bash
export OPENAI_API_KEY=...
go run ./cmd/gopi
```

Optional: `-workspace <dir>`. Model and iteration settings live in `~/.gopi/config.toml`. A `~/.gopi/system.md` file replaces the built-in system prompt.

See [ARCHITECTURE.md](ARCHITECTURE.md) for the full design.
