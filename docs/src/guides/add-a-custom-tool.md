# Add a custom tool

Add a `gogent.Tool` to a mode from your own Go program. The tool is compiled
into that program. The stock binary does not load plugins.

```go
err := gopi.Run(ctx,
    gopi.WithWorkspace(dir),
    gopi.WithTool(gopi.ModeAgent, myTool{}),
    gopi.WithTool(gopi.ModeAsk, myTool{}),
)
```

`WithTool` adds the tool only to that mode. It is not added to the delegate
child. A name that matches a built-in tool fails at startup.

## A minimal tool

```go
type clockTool struct{}

func (clockTool) Name() string { return "clock" }

func (clockTool) Description() string { return "Return the current time." }

func (clockTool) Parameters() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }

func (clockTool) RequiresApproval(context.Context, json.RawMessage) (gogent.ApprovalDecision, error) {
    return gogent.ApprovalDecision{}, nil
}

func (clockTool) Execute(context.Context, json.RawMessage) (json.RawMessage, error) {
    return json.Marshal(map[string]string{"time": time.Now().Format(time.RFC3339)})
}
```

Run it with the library:

```bash
go mod init example.com/myagent
go get github.com/cgund98/gopi@latest
go run .
```

See [Build with gopi as a library](build-with-gopi.md) for the `main` function.

## Credentials

A tool that needs credentials uses `WithToolFactory`. gopi calls the factory at
startup with a `ToolEnv`. `env.Secret(name)` reads that name from
`~/.gopi/secrets.toml`; a missing name stops startup. The value stays in the
factory and is never passed into a shell call. `env.SecretPath(name)` returns the
file behind a file secret, for a tool that must write a refreshed token back.

```go
gopi.WithToolFactory(gopi.ModeAgent, func(env gopi.ToolEnv) (gogent.Tool, error) {
    token, err := env.Secret("gcal_token")
    if err != nil {
        return nil, err
    }
    return calendar.New(token), nil
})
```

## Tool configuration

A `WithToolFactory` tool can read its own top-level table from `config.toml`. The
table name is whatever the tool picks, for example `[calendar]`:

```toml
[calendar]
default_calendar = "primary"
```

Decode it with `env.Config(name, &v)`, where `v` points at a struct with `toml`
tags:

```go
type config struct {
    DefaultCalendar string `toml:"default_calendar"`
}

gopi.WithToolFactory(gopi.ModeAgent, func(env gopi.ToolEnv) (gogent.Tool, error) {
    var cfg config
    if err := env.Config("calendar", &cfg); err != nil {
        return nil, err
    }
    return calendar.New(cfg.DefaultCalendar), nil
})
```

A missing table is an error. Built-in tables (`model`, `models`, `efforts`,
`sandbox`, `instructions`, `search`) are read by gopi and are not available this
way. A project `.gopi/config.toml` is not loaded, so a custom table must be in
`~/.gopi/config.toml`.

## Rendering

A custom tool can implement `gopi.ToolRenderer` to control how its calls look.
`Headline` replaces the tool name on the `>` line, `RenderApproval` draws the
approval body, and `RenderResult` draws a completed result. Each returns plain
text in a `gopi.ToolView` (fields, lines, markdown, or `Hide`), and gopi applies
its own frame, colors, and wrapping. An empty headline or a zero view keeps the
default. Error results always use gopi's error rendering.

```go
func (t *Tool) Headline(args json.RawMessage) string { return "calendar " + operation(args) }

func (t *Tool) RenderApproval(args json.RawMessage) gopi.ToolView {
    return gopi.ToolView{Fields: []gopi.ToolField{{Label: "Summary", Value: summary(args)}}}
}

func (t *Tool) RenderResult(args, result json.RawMessage) gopi.ToolView {
    return gopi.ToolView{Lines: eventLines(result)}
}
```
