# gopi-tools

[gopi-tools](https://github.com/cgund98/gopi-tools) is a Go module of
third-party tools for gopi. It ships `egopi`, a binary that is gopi with the
module's tools compiled in.

## egopi

`egopi` is `gopi` with every tool in the module added. Run it in a workspace the
same way you run `gopi`:

```bash
cd /path/to/project
egopi
```

The command line is gopi's; see [CLI](cli.md). To install it, or to build a
program with a subset of the tools, see
[Use gopi-tools and egopi](../guides/use-gopi-tools.md).

## What the module provides

The gopi-tools [README](https://github.com/cgund98/gopi-tools) lists the packages
and the tools each one adds. Each package has its own README with its operations,
arguments, and setup.

## Adding a tool in code

Each gopi-tools package exports a constructor, or a gopi `Options` helper, for
its tools. Call `gopi.Run` with the ones you want; only the packages you import
are compiled in. See [Use gopi-tools and egopi](../guides/use-gopi-tools.md) and
[Add a custom tool](../guides/add-a-custom-tool.md), and the package README for
the exact call.

## Config and secrets

A tool that needs settings reads its own table from `~/.gopi/config.toml` and its
values from `~/.gopi/secrets.toml`. Each package's README names its table and its
secret names. A missing table stops startup. See
[Configuration](configuration.md#custom-tool-tables).
