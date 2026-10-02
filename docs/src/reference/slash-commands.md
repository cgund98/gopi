# Slash commands

Type these at the composer. `/help` prints the same list in the chat.

| Command | Effect |
|---------|--------|
| `/agent` | Switch to Agent mode. |
| `/ask` | Switch to Ask mode. |
| `/plan` | Switch to Plan mode. |
| `/mode <name>` | Switch mode by name. |
| `/model <name>` | Set the model for the active mode. |
| `/model` | List the supported models. |
| `/effort <level>` | Set reasoning effort: `none`, `low`, `medium`, `high`. |
| `/effort` | List the effort levels. |
| `/allowpath <path>` | Open a path outside the workspace for the rest of this chat. A leading `~` expands to your home directory. |
| `/compact` | Summarize earlier turns. |
| `/mouse [on\|off]` | Toggle mouse capture. |
| `/sessions` | Open the saved-chat list. |
| `/plans` | Open saved plans. |
| `/review` | Walk the file edits from this chat. See [Review changes](../guides/review-changes.md). |
| `/help` | Show this list. |

Input that starts with `/` but names no command on this list is rejected with
an error at the composer and is not sent to the model. Type `/help` for the
list.

`/allowpath` and the `grant_read` tool do the same thing. `/model` and `/effort`
are refused while a turn is running or an approval is pending; finish the turn
first.

`/mouse off` hands the mouse to the terminal for plain selection. `/mouse on`
restores wheel scrolling. `/mouse` alone toggles it. See the
[Quickstart](../guides/quickstart.md) for scroll and selection keys.
