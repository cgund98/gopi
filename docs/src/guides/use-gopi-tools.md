# Use gopi-tools and egopi

[gopi-tools](https://github.com/cgund98/gopi-tools) is a Go module of
third-party tools for gopi. `egopi` is gopi with every tool in that module
compiled in.

## Install egopi

Install from the module proxy with Go 1.26 or later:

```bash
go install github.com/cgund98/gopi-tools/cmd/egopi@latest
```

Or download the file matching your OS and architecture,
`egopi_<version>_<os>_<arch>`, from the
[gopi-tools releases](https://github.com/cgund98/gopi-tools/releases) page, then
put it on your `PATH`. The prebuilt Linux and macOS binaries need no Go
toolchain.

## Run it

Run `egopi` in the directory you want it to work in:

```bash
cd /path/to/project
egopi
```

`egopi` acts like `gopi`: the same modes, sandbox, approval, and review. It adds
the tools from the gopi-tools module on top. See [CLI](../reference/cli.md) for
the command line, and the
[gopi-tools README](https://github.com/cgund98/gopi-tools) for the tools it adds.

## Build your own toolset

A program that imports gopi and one gopi-tools package compiles in only that
package's tools. Start from the library and register the packages you want:

```go
package main

import (
    "context"

    "github.com/cgund98/gopi"
)

func main() {
    ctx := context.Background()
    opts := []gopi.Option{gopi.WithWorkspace("/path/to/project")}

    // Register each gopi-tools package you want. A package exports a
    // constructor you pass to gopi.WithTool or gopi.WithToolFactory, or a
    // gopi.Options helper you append here. Its README shows the call and the
    // config and secrets it reads.
    opts = append(opts, gopi.WithToolFactory(gopi.ModeAgent, newTool))

    if err := gopi.Run(ctx, opts...); err != nil {
        panic(err)
    }
}
```

Only the gopi-tools packages you import are compiled into the binary, so you can
ship a build with exactly the tools you use. See
[Add a custom tool](add-a-custom-tool.md) for a factory, credentials, and
rendering, and [Build with gopi as a library](build-with-gopi.md) for the
options.

## Next steps

- [Add a custom tool](add-a-custom-tool.md)
- [Build with gopi as a library](build-with-gopi.md)
- [gopi-tools reference](../reference/gopi-tools.md)
