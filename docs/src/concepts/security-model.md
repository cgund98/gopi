# Security model

gopi runs with your user account, next to your credentials and your repositories.
The threat it is built around is a model that follows untrusted content — a
malicious repository, a poisoned skill, a web page, a tool result — and tries to
use that access.

You are not the attacker. The model is.

## Principles

- **Fail closed.** If a sandbox cannot be applied as asked, the command does not
  run. A warning plus an unsandboxed fallback is a bypass, not a convenience.
- **Deny wins.** Instructions can narrow what gopi does. They cannot widen the
  floor. A repository cannot un-protect `.env`, and a `deny` host entry beats an
  `allow` entry.
- **Approval is per call.** Protected reads and elevated commands are approved
  once, for that call, then forgotten.
- **The model is untrusted.** File contents, web pages, tool output, skills, and
  `AGENTS.md` inside a repository are data. They do not grant capabilities.
- **Secrets stay in the host.** A value is read by the host and never reaches a
  tool or the model.
- **macOS first.** The shell sandbox is Seatbelt on macOS. Elsewhere a sandboxed
  command is refused rather than run without one.

## Layers

Each layer assumes the one above it failed.

| Layer | What it decides | Enforced by |
|-------|-----------------|-------------|
| Instructions | What the model is told to do | The system prompt; see [Instructions](instructions.md) |
| Tool policy | Whether this call may run, and whether a person must approve it | The tool itself |
| Sandbox | What a process can touch once running | The OS |
| Secrets | Which credentials exist, and that the model never sees a value | The host process |

The first layer is not a boundary. A repository's `AGENTS.md` can ask for
anything; the layers below it decide. Tool policy alone is not a boundary either —
a check inside `read_file` does not stop `shell` from reading the same path. The
sandbox is what still holds after the spawn, which is why one deny set feeds both
the tool check and the OS profile. See [Sandboxing](sandboxing.md).

The same four-layer split is what Codex and Claude Code use, and it is the reason
gopi separates "may this call run" from "what can this process touch".

## The protections

| Protection | Covers |
|------------|--------|
| Protected paths | Secret files, credentials, `.git` internals, ignore files |
| Scrubbed environment | No inherited `HOME`, tokens, or injection variables |
| Sandbox profile | Reads, writes, and network for one child process |
| Network proxy | Host-level allowlist, plus private and metadata address blocks |
| Redaction | Known secret values and common token shapes in results |
| Per-call approval | Reaching outside the workspace, or widening the sandbox |

One directory is deliberately outside the protected set: `<workspace>/.gopi/plans`
is readable and writable like any other workspace file, because plans are
agent-authored markdown. The floor still applies inside it, so a `.env` or a
`*.pem` under `.gopi/plans` stays protected. See
[Protected paths](sandboxing.md#protected-paths).

## Threats and controls

| Threat | Control |
|--------|---------|
| `read_file` refuses `.env`, so the model tries `cat .env` | The same path is a deny rule in the shell profile |
| A repo's `AGENTS.md` or skill says to exfiltrate and skip approval | Instructions cannot change tools or the sandbox; only `~/.gopi/config.toml` is read as config |
| A symlink in the repo points at `~/.ssh/id_rsa` | Paths are canonicalized in the host, and the OS denies the target |
| `..`, case tricks, or `/tmp` versus `/private/tmp` | Canonical path rules and case folding |
| A binary ignores `HTTP_PROXY` and dials out | The OS denies every address except the loopback proxy port |
| An allowlisted host redirects to a metadata address or a private IP | The proxy rechecks the resolved address at connect time |
| A DNS tunnel while network is `deny` | The child gets no DNS at all |
| `docker.sock`, the ssh-agent, or a user bus | No extra unix sockets are available to the child |
| `LD_PRELOAD` or `DYLD_INSERT_LIBRARIES` set by the model | The environment is scrubbed before `exec`, and the binary is write-protected |
| Writing `.git/hooks/pre-commit` for later execution | Hooks, config, and ignore files are write-protected |
| Prompt injection asks for `profile: unsandboxed` | That request is a fresh approval card |
| A subagent tries to get approval from the parent | The child registry has no approval path; the call fails closed |
| A secret echoed by `env` or `set -x` | The child environment holds no broker value, and the redactor runs on the result |
| The sandbox helper is missing | The command is refused |
| Output floods memory or the context window | Per-command output cap and timeout |
| A file is swapped for a symlink between the check and the exec | The OS rule stays in force after the spawn |
| Approval fatigue makes cards meaningless | The card shows the argv, the real path, and the permission delta |

## Keeping the boundary honest

The controls above are only worth as much as the rules that implement them, so the
sandbox has a regression suite that runs whenever the profile generator changes. On
macOS, `TestShellEscape` asserts that a sandboxed command cannot:

- read a fixture `.env` placed in the workspace;
- write `.git/hooks/pre-commit`;
- follow a symlink out of the workspace;
- reach a local listener on loopback.

Alongside it, the Seatbelt tests check that a deny still wins after a workspace
allow and after a session or per-call grant, and the proxy tests check that an
allowlisted host is dialed while a metadata address is refused.

Every child is also bounded: a timeout, an output cap, and process-group
cancellation tied to the TUI interrupt. See [Bounds](sandboxing.md#bounds).

## Out of scope

- A malicious or compromised model provider.
- A compromised gopi binary. gopi is the thing making these decisions, so it
  assumes it is intact.
- You approving `unsandboxed` on purpose, with the card in front of you. The audit
  log exists so those decisions are reconstructable after the fact.
- Windows. macOS is the target; Linux is a policy port, not a supported sandbox
  today.

## Related

- [Sandboxing](sandboxing.md) — the profile, the protected paths, and the network
  modes.
- [Permissions and approval](permissions-and-approval.md) — the per-call gate.
- [Secrets](secrets.md) — what never reaches the model.
- [Instructions](instructions.md) — why text cannot grant a capability.
