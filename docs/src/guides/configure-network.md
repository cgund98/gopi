# Configure network access

The sandboxed shell has no network by default. A package install or `git fetch`
fails until you allowlist a host or approve a single elevated call. See
[Sandboxing](../concepts/sandboxing.md#network) for how enforcement works.

## Allow a host

Add hosts to `~/.gopi/config.toml` and set `network` to `allowlist`:

```toml
[sandbox]
network = "allowlist"     # deny | allowlist

[sandbox.network]
allow = ["github.com", "proxy.golang.org", "*.npmjs.org"]
deny = []                 # a deny entry beats an allow entry
```

A `*` matches one DNS label, so `*.npmjs.org` matches `registry.npmjs.org` but
not `a.b.npmjs.org`. URL paths are ignored; the entry is a hostname.

Restart gopi after editing the file.

## Deny a host

A `deny` entry beats an `allow` entry. Use it to carve a host back out of a
wildcard:

```toml
[sandbox.network]
allow = ["*.npmjs.org"]
deny = ["internal.npmjs.org"]
```

Private, loopback, link-local, and cloud metadata addresses stay blocked even
when a host is allowlisted.

## What fails

With `deny` (the default), an install fails and the command reports it:

```
$ go install example.com/tool@latest
go: example.com/tool@latest: module example.com/tool: Get
"https://proxy.example.com/tool/@v/list": dial tcp: operation not permitted
```

Add the host to the allowlist and restart, or let the model request the host for
one call.

## Elevate a single call

When a command needs a host that is not allowlisted, the model asks for it, and
gopi pauses for approval. The card names the delta, such as
`Network: github.com` or `Network: deny -> unrestricted`. Approve to run that
one call.

- **Keep the allowlist small.** Prefer adding one host over `unrestricted`.
- **Approval is per call.** Gopi asks again on the next call.
- **`unrestricted` is not a config setting.** It is only a per-call shell
  profile, and it always pauses for approval.

See [Permissions and approval](../concepts/permissions-and-approval.md).
