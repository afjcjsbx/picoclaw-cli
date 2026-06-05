package cli

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"golang.org/x/term"
)

func Main() int {
	opts := parseOptions()
	if opts.hideTools {
		opts.showTools = false
	}

	if strings.TrimSpace(opts.rawURL) == "" {
		usage()
		return 2
	}

	if strings.TrimSpace(opts.sessionID) == "" {
		opts.sessionID = "pico-" + uuid.NewString()
	}

	wsURL, err := buildWSURL(opts.rawURL, opts.sessionID, opts.token, opts.tokenQuery)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error> invalid url: %v\n", err)
		return 1
	}

	header := http.Header{}
	if opts.token != "" && !opts.tokenQuery {
		header.Set("Authorization", "Bearer "+opts.token)
	}

	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, header)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error> connect failed: %v\n", err)
		return 1
	}
	defer conn.Close()

	ws := &safeConn{conn: conn}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd())) {
		if err := runBubbleTeaCLI(ctx, wsURL, opts, conn, ws); err != nil {
			fmt.Fprintf(os.Stderr, "error> tui failed: %v\n", err)
			return 1
		}
		return 0
	}

	input, err := newLineReader()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error> input init failed: %v\n", err)
		return 1
	}
	defer input.Close()

	var overlay transientLine
	if managed, ok := input.(transientLine); ok {
		overlay = managed
	}

	render := newRenderer(wsURL, opts.sessionID, opts.showThoughts, opts.showTools, opts.compactTools, input.Output(), input.ManagesPrompt(), overlay)
	render.printWelcome()

	readErrCh := make(chan error, 1)
	go readLoop(ctx, conn, render, readErrCh)

	if opts.pingEvery > 0 {
		go pingLoop(ctx, ws, opts.pingEvery)
	}

	inputDone := make(chan struct{})
	go func() {
		defer close(inputDone)
		readInput(ctx, ws, render, input)
	}()

	select {
	case <-ctx.Done():
	case <-inputDone:
	case err := <-readErrCh:
		if err != nil {
			render.printError(err.Error())
		}
	}

	render.finishActive()
	return 0
}
