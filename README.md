# picoclaw-cli

![picoclaw-cli](/assets/images/cli.png)

A terminal-first remote CLI for PicoClaw.

## Usage

```bash
go run . \
  -url ws://your-pico-host:18790/pico/ws \
  -token your-pico-token
```

## Install

```bash
go install github.com/afjcjsbx/picoclaw-cli@latest
```

Then run:

```bash
picoclaw-cli \
  -url ws://your-pico-host:18790/pico/ws \
  -token your-pico-token
```

If `picoclaw-cli` is not on your `PATH`, run it from Go's default bin directory:

```bash
~/go/bin/picoclaw-cli \
  -url ws://your-pico-host:18790/pico/ws \
  -token your-pico-token
```

The `-url` flag must be a complete WebSocket URL, including `ws://` or `wss://`.
Use a host name or IP address only; SSH-style values like `user@host` are not
valid here.

The client opens a WebSocket session, sends `message.send`, and renders
`message.create` / `message.update` replies in the terminal.
It separates normal assistant output from structured tool activity and,
optionally, structured thought messages. Assistant replies are rendered as
terminal-friendly Markdown, including headings, lists, links, tables, and code
blocks.

## Project layout

- `main.go` lets `go install github.com/afjcjsbx/picoclaw-cli@latest` work from the module root
- `cmd/picoclaw-cli` contains the executable entrypoint
- `pkg/cli` contains the reusable CLI and TUI logic

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-url` | `$PICO_REMOTE_URL` | Remote Pico WebSocket URL |
| `-token` | `$PICO_REMOTE_TOKEN` | Pico auth token |
| `-session` | random UUID | Session id to reuse between runs |
| `-token-query` | `false` | Send token as `?token=...` instead of `Authorization: Bearer ...` |
| `-ping` | `25s` | Client ping interval |
| `-show-thoughts` | `false` | Show structured thought messages |
| `-tools` | `true` | Enable structured tool call / tool feedback panels |
| `-show-tools` | `true` | Alias for `-tools` |
| `-hide-tools` | `false` | Start with tool panels hidden |
| `-compact-tools` | `false` | Show tool activity as compact headers only, without payload details |

## Interactive commands

- Up/down arrows browse saved input history between sessions (last 100 entries)
- As you type a local `/command`, matching suggestions appear in the prompt
- `Tab` autocompletes local `/commands` and toggle values like `on`/`off`
- `/help` shows the available commands
- `/status` shows URL, session, and display toggles
- `/session` prints the current session id
- `/thoughts on|off` toggles structured thought visibility
- `/tools on|off` toggles tool visibility
- `/clear` clears the screen and redraws the header
- `/quit` exits

## Tailscale + SSH tunnel example

If the gateway on the Raspberry is only listening on `127.0.0.1`, tunnel it:

```bash
ssh user@your-tailnet-host.ts.net -L 18790:127.0.0.1:18790
picoclaw-cli -url ws://127.0.0.1:18790/pico/ws -token your-pico-token
```
