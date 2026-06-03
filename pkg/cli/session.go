package cli

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type safeConn struct {
	conn    *websocket.Conn
	writeMu sync.Mutex
}

func (c *safeConn) WriteJSON(v any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.conn.WriteJSON(v)
}

func (c *safeConn) WriteControl(messageType int, data []byte, deadline time.Time) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.conn.WriteControl(messageType, data, deadline)
}

func readLoop(ctx context.Context, conn *websocket.Conn, render *renderer, errCh chan<- error) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		var msg picoMessage
		if err := conn.ReadJSON(&msg); err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) || ctx.Err() != nil {
				return
			}
			errCh <- fmt.Errorf("connection closed: %w", err)
			return
		}

		render.handleMessage(msg)
	}
}

func pingLoop(ctx context.Context, conn *safeConn, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second))
		}
	}
}

func readInput(ctx context.Context, conn *safeConn, render *renderer, input lineReader) {
	for {
		render.finishActive()
		if !input.ManagesPrompt() {
			render.printPrompt()
		}

		line, err := input.ReadLine()
		input.AfterReadLine()
		if !input.ManagesPrompt() {
			render.closePrompt()
		}
		if err != nil {
			if err == io.EOF {
				return
			}
			render.printError(fmt.Sprintf("stdin read failed: %v", err))
			return
		}

		select {
		case <-ctx.Done():
			return
		default:
		}

		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		if handleLocalCommand(trimmed, render) {
			if trimmed == "/quit" || trimmed == "/exit" {
				return
			}
			continue
		}

		msg := picoMessage{
			Type:      typeMessageSend,
			ID:        uuid.NewString(),
			SessionID: render.sessionID,
			Timestamp: time.Now().UnixMilli(),
			Payload: map[string]any{
				payloadKeyContent: trimmed,
			},
		}
		if err := conn.WriteJSON(msg); err != nil {
			render.printError(fmt.Sprintf("send failed: %v", err))
			return
		}
	}
}

func handleLocalCommand(input string, render *renderer) bool {
	fields := strings.Fields(input)
	if len(fields) == 0 {
		return false
	}

	switch fields[0] {
	case "/quit", "/exit":
		return true
	case "/help":
		render.printHelp()
		return true
	case "/status":
		render.printSessionStatus()
		return true
	case "/session":
		render.printInfo("Session: " + render.sessionID)
		return true
	case "/clear":
		render.clear()
		render.printWelcome()
		return true
	case "/thoughts":
		if len(fields) == 1 {
			render.printInfo("Thoughts: " + onOff(render.showThoughts))
			return true
		}
		next, ok := parseToggle(fields[1])
		if !ok {
			render.printError("usage: /thoughts on|off")
			return true
		}
		render.setShowThoughts(next)
		return true
	case "/tools":
		if len(fields) == 1 {
			render.printInfo("Tools: " + onOff(render.showTools))
			return true
		}
		next, ok := parseToggle(fields[1])
		if !ok {
			render.printError("usage: /tools on|off")
			return true
		}
		render.setShowTools(next)
		return true
	default:
		return false
	}
}
