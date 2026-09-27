# Build with gopi as a library

`cmd/gopi` is the built binary. It starts the same program as `gopi.Run` with
the built-in tools. Another Go program can start the same agent and add tools.

## Minimal program

```go
package main

import (
    "context"

    "github.com/cgund98/gopi"
)

func main() {
    ctx := context.Background()
    if err := gopi.Run(ctx, gopi.WithWorkspace("/path/to/project")); err != nil {
        panic(err)
    }
}
```

```bash
go mod init example.com/myagent
go get github.com/cgund98/gopi@latest
go run .
```

`Run` blocks until the session ends.

## Add a tool

```go
err := gopi.Run(ctx,
    gopi.WithWorkspace(dir),
    gopi.WithTool(gopi.ModeAgent, myTool{}),
)
```

## Options

| Option | Effect |
|--------|--------|
| `WithWorkspace(dir)` | Set the workspace root. Defaults to the current directory. |
| `WithTool(mode, tool)` | Add a `gogent.Tool` to one mode. |
| `WithToolFactory(mode, factory)` | Add a tool built at startup, for one that needs credentials. |
| `WithResume()` | Open the newest saved session for the workspace. |

The modes are `ModeAgent`, `ModeAsk`, and `ModePlan`. `WithTool` adds the tool
to that mode only, and not to the delegate child. A name that matches a built-in
tool fails at startup.

See [Add a custom tool](add-a-custom-tool.md) for credentials, tool
configuration, and result rendering.
