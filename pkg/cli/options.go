package cli

import (
	"flag"
	"fmt"
	"os"
	"time"
)

type cliOptions struct {
	rawURL       string
	token        string
	sessionID    string
	compactTools bool
	mouseMode    bool
	tokenQuery   bool
	pingEvery    time.Duration
	hideTools    bool
	showThoughts bool
	showTools    bool
}

func parseOptions() cliOptions {
	var opts cliOptions

	flag.StringVar(&opts.rawURL, "url", envOr("PICO_REMOTE_URL", ""), "Remote Pico WebSocket URL")
	flag.StringVar(&opts.token, "token", envOr("PICO_REMOTE_TOKEN", ""), "Pico auth token")
	flag.StringVar(&opts.sessionID, "session", envOr("PICO_REMOTE_SESSION", ""), "Session id to reuse")
	flag.BoolVar(&opts.tokenQuery, "token-query", false, "Send the token as ?token=... instead of Authorization header")
	flag.DurationVar(&opts.pingEvery, "ping", 25*time.Second, "Client ping interval")
	flag.BoolVar(&opts.mouseMode, "mouse", false, "Capture the mouse for wheel scroll and drag-to-copy in the TUI (disables native terminal text selection)")
	flag.BoolVar(&opts.showThoughts, "show-thoughts", false, "Show structured thought messages when the server sends them")
	flag.BoolVar(&opts.showTools, "tools", true, "Enable structured tool call and tool feedback panels")
	flag.BoolVar(&opts.showTools, "show-tools", true, "Show structured tool call and tool feedback events")
	flag.BoolVar(&opts.hideTools, "hide-tools", false, "Hide structured tool call and tool feedback panels at startup")
	flag.BoolVar(&opts.compactTools, "compact-tools", false, "Show compact tool activity headers without payload details")
	flag.Usage = usage
	flag.Parse()

	return opts
}

func usage() {
	fmt.Fprintf(os.Stderr, "Usage: %s -url ws://host:18790/pico/ws [-token xxx] [-session id]\n\n", os.Args[0])
	fmt.Fprintln(os.Stderr, "Flags:")
	flag.PrintDefaults()
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Interactive commands:")
	fmt.Fprintln(os.Stderr, "  /help                Show commands")
	fmt.Fprintln(os.Stderr, "  /status              Show current session and display settings")
	fmt.Fprintln(os.Stderr, "  /session             Print the current session id")
	fmt.Fprintln(os.Stderr, "  /thoughts on|off     Toggle structured thought visibility")
	fmt.Fprintln(os.Stderr, "  /tools on|off        Toggle tool event visibility")
	fmt.Fprintln(os.Stderr, "  /clear               Clear the screen and redraw the header")
	fmt.Fprintln(os.Stderr, "  /quit                Exit")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "By default the TUI leaves mouse selection to your terminal so text stays selectable and copyable.")
	fmt.Fprintln(os.Stderr, "Pass -mouse to enable wheel scrolling and drag-to-copy inside the app instead.")
}
