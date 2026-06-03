package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/gomarkdown/markdown"
	"github.com/gomarkdown/markdown/ast"
	"github.com/gomarkdown/markdown/parser"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"golang.org/x/term"
)

const (
	typeMessageSend   = "message.send"
	typeMessageCreate = "message.create"
	typeMessageUpdate = "message.update"
	typeMessageDelete = "message.delete"
	typeMediaCreate   = "media.create"
	typeTypingStart   = "typing.start"
	typeTypingStop    = "typing.stop"
	typeError         = "error"
	typePong          = "pong"

	payloadKeyContent = "content"
	payloadKeyKind    = "kind"
	payloadKeyTools   = "tool_calls"
	payloadKeyModel   = "model_name"

	messageKindThought   = "thought"
	messageKindToolCalls = "tool_calls"

	assistantFlushDelay = 180 * time.Millisecond
)

var (
	typingSpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	toolFeedbackPattern = regexp.MustCompile("^\\s*(?:[^A-Za-z0-9`\\r\\n]+\\s*)?`([^`\\n\\r]*?)(?:\\.{1,2})?`[^\\n\\r]*(?:\\r?\\n([\\s\\S]*))?$")
	codeFencePattern    = regexp.MustCompile("(?m)```(?:json)?\\r?\\n([\\s\\S]*?)\\r?\\n```")
	ansiEscapePattern   = regexp.MustCompile("\x1b\\[[0-9;]*m")
	colorUser           = lipgloss.Color("#14746F")
	colorAssistant      = lipgloss.Color("#3056D3")
	colorTool           = lipgloss.Color("#B45309")
	colorThought        = lipgloss.Color("#7C3AED")
	colorStatus         = lipgloss.Color("#6B7280")
	colorDanger         = lipgloss.Color("#D14343")
	colorBorder         = lipgloss.Color("#4B5563")
	colorTextMuted      = lipgloss.Color("#94A3B8")
	stylePrompt         = lipgloss.NewStyle().Foreground(colorUser).Bold(true)
	styleHeader         = lipgloss.NewStyle().Foreground(colorAssistant).Bold(true)
	styleMuted          = lipgloss.NewStyle().Foreground(colorTextMuted)
	styleStatus         = lipgloss.NewStyle().Foreground(colorStatus)
	styleError          = lipgloss.NewStyle().Foreground(colorDanger).Bold(true)
	styleToolInline     = lipgloss.NewStyle().Foreground(colorTool).Bold(true)
	styleThoughtInline  = lipgloss.NewStyle().Foreground(colorThought).Bold(true)
	styleBody           = lipgloss.NewStyle()
	styleRule           = lipgloss.NewStyle().Foreground(colorBorder)
	styleMarkdownH1     = lipgloss.NewStyle().Foreground(colorAssistant).Bold(true).Underline(true)
	styleMarkdownH2     = lipgloss.NewStyle().Foreground(colorAssistant).Bold(true)
	styleMarkdownH3     = lipgloss.NewStyle().Foreground(colorAssistant)
	styleMarkdownCode   = lipgloss.NewStyle().Foreground(lipgloss.Color("#155E75")).Background(lipgloss.Color("#ECFEFF")).Bold(true)
	styleMarkdownLink   = lipgloss.NewStyle().Foreground(lipgloss.Color("#0F766E")).Underline(true)
	styleMarkdownQuote  = lipgloss.NewStyle().Foreground(lipgloss.Color("#64748B"))
	styleMarkdownRule   = lipgloss.NewStyle().Foreground(colorBorder)
	styleMarkdownBullet = lipgloss.NewStyle().Foreground(colorAssistant).Bold(true)
)

type picoMessage struct {
	Type      string         `json:"type"`
	ID        string         `json:"id,omitempty"`
	SessionID string         `json:"session_id,omitempty"`
	Timestamp int64          `json:"timestamp,omitempty"`
	Payload   map[string]any `json:"payload,omitempty"`
}

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

type displayKind string

const (
	displayNormal       displayKind = "normal"
	displayThought      displayKind = "thought"
	displayToolCalls    displayKind = "tool_calls"
	displayToolFeedback displayKind = "tool_feedback"
	displayPlaceholder  displayKind = "placeholder"
)

type messageView struct {
	kind      displayKind
	modelName string
	content   string
	toolCalls []VisibleToolCall
}

type renderer struct {
	mu sync.Mutex

	wsURL     string
	sessionID string

	showThoughts bool
	showTools    bool
	compactTools bool
	typing       bool

	activeMessageID string
	activeContent   string
	activeModel     string
	activeOpen      bool
	promptOpen      bool
	flushSeq        uint64
	spinnerVisible  bool
	spinnerSeq      uint64

	messages map[string]messageView
}

func newRenderer(wsURL, sessionID string, showThoughts, showTools, compactTools bool) *renderer {
	return &renderer{
		wsURL:         wsURL,
		sessionID:     sessionID,
		showThoughts:  showThoughts,
		showTools:     showTools,
		compactTools:  compactTools,
		messages:      make(map[string]messageView),
		activeOpen:    false,
		activeContent: "",
	}
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
}

func Main() int {
	var (
		rawURL       string
		token        string
		sessionID    string
		compactTools bool
		tokenQuery   bool
		pingEvery    time.Duration
		hideTools    bool
		showThoughts bool
		showTools    bool
	)

	flag.StringVar(&rawURL, "url", envOr("PICO_REMOTE_URL", ""), "Remote Pico WebSocket URL")
	flag.StringVar(&token, "token", envOr("PICO_REMOTE_TOKEN", ""), "Pico auth token")
	flag.StringVar(&sessionID, "session", envOr("PICO_REMOTE_SESSION", ""), "Session id to reuse")
	flag.BoolVar(&tokenQuery, "token-query", false, "Send the token as ?token=... instead of Authorization header")
	flag.DurationVar(&pingEvery, "ping", 25*time.Second, "Client ping interval")
	flag.BoolVar(&showThoughts, "show-thoughts", false, "Show structured thought messages when the server sends them")
	flag.BoolVar(&showTools, "tools", true, "Enable structured tool call and tool feedback panels")
	flag.BoolVar(&showTools, "show-tools", true, "Show structured tool call and tool feedback events")
	flag.BoolVar(&hideTools, "hide-tools", false, "Hide structured tool call and tool feedback panels at startup")
	flag.BoolVar(&compactTools, "compact-tools", false, "Show compact tool activity headers without payload details")
	flag.Usage = usage
	flag.Parse()

	if hideTools {
		showTools = false
	}

	if strings.TrimSpace(rawURL) == "" {
		usage()
		return 2
	}

	if strings.TrimSpace(sessionID) == "" {
		sessionID = "pico-" + uuid.NewString()
	}

	wsURL, err := buildWSURL(rawURL, sessionID, token, tokenQuery)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error> invalid url: %v\n", err)
		return 1
	}

	header := http.Header{}
	if token != "" && !tokenQuery {
		header.Set("Authorization", "Bearer "+token)
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

	render := newRenderer(wsURL, sessionID, showThoughts, showTools, compactTools)
	render.printWelcome()

	readErrCh := make(chan error, 1)
	go readLoop(ctx, conn, render, readErrCh)

	if pingEvery > 0 {
		go pingLoop(ctx, ws, pingEvery)
	}

	inputDone := make(chan struct{})
	go func() {
		defer close(inputDone)
		readInput(ctx, ws, render)
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

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func buildWSURL(rawURL, sessionID, token string, tokenQuery bool) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}

	query := parsed.Query()
	query.Set("session_id", sessionID)
	if tokenQuery && token != "" {
		query.Set("token", token)
	}
	parsed.RawQuery = query.Encode()

	return parsed.String(), nil
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

func readInput(ctx context.Context, conn *safeConn, render *renderer) {
	reader := bufio.NewReader(os.Stdin)

	for {
		render.finishActive()
		render.printPrompt()

		line, err := reader.ReadString('\n')
		render.closePrompt()
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

		input := strings.TrimSpace(line)
		if input == "" {
			continue
		}

		if handleLocalCommand(input, render) {
			if input == "/quit" || input == "/exit" {
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
				payloadKeyContent: input,
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

func parseToggle(value string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "on", "true", "1", "yes":
		return true, true
	case "off", "false", "0", "no":
		return false, true
	default:
		return false, false
	}
}

func onOff(v bool) string {
	if v {
		return "on"
	}
	return "off"
}

func ternary[T any](cond bool, whenTrue, whenFalse T) T {
	if cond {
		return whenTrue
	}
	return whenFalse
}

func (r *renderer) printPrompt() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.flushAssistantLocked()
	fmt.Print(stylePrompt.Render("› "))
	r.promptOpen = true
}

func (r *renderer) closePrompt() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.promptOpen = false
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

func (r *renderer) handleMessage(msg picoMessage) {
	payload := msg.Payload
	if payload == nil {
		payload = map[string]any{}
	}

	switch msg.Type {
	case typeTypingStart:
		r.setTyping(true)
		return
	case typeTypingStop:
		r.setTyping(false)
		return
	case typeMessageDelete:
		messageID, _ := payload["message_id"].(string)
		r.printStatusLine(fmt.Sprintf("message deleted: %s", strings.TrimSpace(messageID)))
		return
	case typeError:
		message, _ := payload["message"].(string)
		code, _ := payload["code"].(string)
		if code != "" {
			r.printError(fmt.Sprintf("%s: %s", code, message))
		} else {
			r.printError(message)
		}
		return
	case typePong:
		return
	case typeMessageCreate, typeMessageUpdate, typeMediaCreate:
	default:
		r.printStatusLine(fmt.Sprintf("received %s", msg.Type))
		return
	}

	r.setTyping(false)

	messageID, _ := payload["message_id"].(string)
	prev := r.messages[messageID]
	next := buildMessageView(payload, prev)
	if messageID != "" {
		r.messages[messageID] = next
	}

	switch next.kind {
	case displayPlaceholder:
		if text := strings.TrimSpace(next.content); text != "" {
			r.printStatusLine(text)
		}
	case displayThought:
		if r.shouldShowThoughts() {
			r.printTranscriptEntry("◌", "Thought", next.modelName, next.content, colorThought)
		}
	case displayToolCalls, displayToolFeedback:
		if r.shouldShowTools() {
			r.printToolActivity(next)
		}
	case displayNormal:
		if msg.Type == typeMessageUpdate {
			r.handleAssistantUpdate(messageID, next.modelName, next.content)
		} else {
			r.handleAssistantCreate(messageID, next.modelName, next.content)
		}
	}
}

func buildMessageView(payload map[string]any, prev messageView) messageView {
	content, _ := payload[payloadKeyContent].(string)
	modelName := strings.TrimSpace(stringValue(payload[payloadKeyModel]))
	if modelName == "" {
		modelName = prev.modelName
	}

	if payload["placeholder"] == true {
		return messageView{
			kind:      displayPlaceholder,
			modelName: modelName,
			content:   content,
		}
	}

	toolCalls := parseVisibleToolCalls(payload[payloadKeyTools])
	kind := normalizedKind(payload)
	switch {
	case kind == messageKindThought:
		return messageView{
			kind:      displayThought,
			modelName: modelName,
			content:   content,
		}
	case kind == messageKindToolCalls || len(toolCalls) > 0:
		return messageView{
			kind:      displayToolCalls,
			modelName: modelName,
			content:   content,
			toolCalls: toolCalls,
		}
	}

	if legacyCalls, ok := parseLegacyToolFeedback(content); ok {
		return messageView{
			kind:      displayToolFeedback,
			modelName: modelName,
			content:   content,
			toolCalls: legacyCalls,
		}
	}

	return messageView{
		kind:      displayNormal,
		modelName: modelName,
		content:   content,
	}
}

func parseVisibleToolCalls(raw any) []VisibleToolCall {
	if raw == nil {
		return nil
	}

	data, err := json.Marshal(raw)
	if err != nil {
		return nil
	}

	var toolCalls []VisibleToolCall
	if err := json.Unmarshal(data, &toolCalls); err != nil || len(toolCalls) == 0 {
		return nil
	}
	return toolCalls
}

func parseLegacyToolFeedback(content string) ([]VisibleToolCall, bool) {
	trimmed := strings.TrimSpace(content)
	matches := toolFeedbackPattern.FindStringSubmatch(trimmed)
	if len(matches) == 0 {
		return nil, false
	}

	toolName := strings.TrimSpace(matches[1])
	body := ""
	if len(matches) > 2 {
		body = strings.TrimSpace(matches[2])
	}
	argsText, explanation := splitToolFeedbackBody(body)

	call := VisibleToolCall{
		Type: "function",
	}
	if toolName != "" || argsText != "" {
		call.Function = &VisibleToolCallFunction{
			Name:      toolName,
			Arguments: argsText,
		}
	}
	if explanation != "" {
		call.ExtraContent = &VisibleToolCallExtraContent{
			ToolFeedbackExplanation: explanation,
		}
	}

	if call.Function == nil && call.ExtraContent == nil {
		return nil, false
	}
	return []VisibleToolCall{call}, true
}

func splitToolFeedbackBody(body string) (string, string) {
	if strings.TrimSpace(body) == "" {
		return "", ""
	}

	codeFence := codeFencePattern.FindStringSubmatch(body)
	argsText := ""
	if len(codeFence) > 1 {
		argsText = strings.TrimSpace(codeFence[1])
	}

	explanation := strings.TrimSpace(codeFencePattern.ReplaceAllString(body, ""))
	return argsText, explanation
}

func normalizedKind(payload map[string]any) string {
	return strings.ToLower(strings.TrimSpace(stringValue(payload[payloadKeyKind])))
}

func stringValue(raw any) string {
	value, _ := raw.(string)
	return value
}

func renderToolBody(view messageView) string {
	parts := make([]string, 0, len(view.toolCalls)+1)
	for idx, toolCall := range view.toolCalls {
		lines := make([]string, 0, 4)
		if len(view.toolCalls) > 1 {
			lines = append(lines, fmt.Sprintf("call %d", idx+1))
		}

		name := ""
		if toolCall.Function != nil {
			name = strings.TrimSpace(toolCall.Function.Name)
		}
		if name == "" {
			name = "unknown"
		}
		lines = append(lines, styleToolInline.Render("tool")+": "+name)

		if toolCall.ExtraContent != nil {
			explanation := strings.TrimSpace(toolCall.ExtraContent.ToolFeedbackExplanation)
			if explanation != "" {
				lines = append(lines, explanation)
			}
		}

		if toolCall.Function != nil {
			args := strings.TrimSpace(toolCall.Function.Arguments)
			if args != "" {
				lines = append(lines, "args:")
				lines = append(lines, indentBlock(prettyJSON(args), "  "))
			}
		}

		parts = append(parts, strings.Join(lines, "\n"))
	}

	if len(parts) == 0 {
		content := strings.TrimSpace(view.content)
		if content == "" {
			return "No visible tool details."
		}
		return content
	}

	return strings.Join(parts, "\n\n")
}

func renderToolEntries(view messageView, compact bool) []string {
	if len(view.toolCalls) == 0 {
		body := strings.TrimSpace(view.content)
		if body == "" {
			return nil
		}
		if compact {
			body = ""
		}
		return []string{renderTranscriptEntry("⏺", "Tool activity", view.modelName, body, colorTool)}
	}

	entries := make([]string, 0, len(view.toolCalls))
	for idx, toolCall := range view.toolCalls {
		name := "tool"
		if toolCall.Function != nil && strings.TrimSpace(toolCall.Function.Name) != "" {
			name = strings.TrimSpace(toolCall.Function.Name)
		}

		meta := strings.TrimSpace(view.modelName)
		if len(view.toolCalls) > 1 {
			callMeta := fmt.Sprintf("call %d/%d", idx+1, len(view.toolCalls))
			if meta == "" {
				meta = callMeta
			} else {
				meta += " · " + callMeta
			}
		}

		bodyParts := make([]string, 0, 3)
		if !compact {
			if toolCall.ExtraContent != nil {
				explanation := strings.TrimSpace(toolCall.ExtraContent.ToolFeedbackExplanation)
				if explanation != "" {
					bodyParts = append(bodyParts, explanation)
				}
			}

			if toolCall.Function != nil {
				args := strings.TrimSpace(toolCall.Function.Arguments)
				if args != "" {
					bodyParts = append(bodyParts, styleMuted.Render("args"))
					bodyParts = append(bodyParts, indentRenderedBlock(renderCodeFence(prettyJSON(args), ""), "  "))
				}
			}
		}

		body := strings.Join(bodyParts, "\n")
		entries = append(entries, renderTranscriptEntry("⏺", name, meta, body, colorTool))
	}

	return entries
}

func renderTranscriptEntry(icon, title, meta, body string, accent lipgloss.Color) string {
	header := lipgloss.NewStyle().Foreground(accent).Bold(true).Render(icon + " " + title)
	if strings.TrimSpace(meta) != "" {
		header += "  " + styleMuted.Render(meta)
	}
	if strings.TrimSpace(stripControlWhitespace(body)) == "" {
		return header
	}

	return header + "\n" + indentRenderedBlock(body, "  ")
}

func prettyJSON(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}

	var out bytes.Buffer
	if err := json.Indent(&out, []byte(trimmed), "", "  "); err == nil {
		return out.String()
	}
	return trimmed
}

func indentBlock(text, prefix string) string {
	lines := strings.Split(text, "\n")
	for i := range lines {
		lines[i] = prefix + lines[i]
	}
	return strings.Join(lines, "\n")
}

func (r *renderer) printWelcome() {
	body := strings.Join([]string{
		r.wrapKeyValue("Connected to", r.wsURL),
		r.wrapKeyValue("Session", r.sessionID),
		r.wrapKeyValue("Thoughts", onOff(r.showThoughts)+"   Tools: "+onOff(r.showTools)),
		r.wrapKeyValue("Tool view", ternary(r.compactTools, "compact", "full")),
		r.wrapKeyValue("Assistant replies", "markdown-rendered"),
		r.wrapKeyValue("Commands", "/help /status /thoughts on|off /tools on|off /clear /quit"),
	}, "\n")
	r.printPanel("Remote Pico CLI", body, colorAssistant)
}

func (r *renderer) printHelp() {
	body := strings.Join([]string{
		"/help                Show commands",
		"/status              Show current session and display settings",
		"/session             Print the current session id",
		"/thoughts on|off     Toggle structured thought visibility",
		"/tools on|off        Toggle tool event visibility",
		"/clear               Clear the screen and redraw the header",
		"/quit                Exit",
		"",
		"Assistant replies render basic Markdown: headings, lists, links, tables, and code blocks.",
	}, "\n")
	r.printPanel("Help", body, colorAssistant)
}

func (r *renderer) printSessionStatus() {
	r.mu.Lock()
	body := strings.Join([]string{
		r.wrapKeyValue("URL", r.wsURL),
		r.wrapKeyValue("Session", r.sessionID),
		r.wrapKeyValue("Thoughts", onOff(r.showThoughts)),
		r.wrapKeyValue("Tools", onOff(r.showTools)),
		r.wrapKeyValue("Tool view", ternary(r.compactTools, "compact", "full")),
		r.wrapKeyValue("Assistant", "markdown-rendered"),
		r.wrapKeyValue("Typing", onOff(r.typing)),
	}, "\n")
	r.mu.Unlock()
	r.printPanel("Status", body, colorAssistant)
}

func (r *renderer) setShowThoughts(enabled bool) {
	r.mu.Lock()
	r.showThoughts = enabled
	r.mu.Unlock()
	r.printInfo("Thoughts: " + onOff(enabled))
}

func (r *renderer) setShowTools(enabled bool) {
	r.mu.Lock()
	r.showTools = enabled
	r.mu.Unlock()
	r.printInfo("Tools: " + onOff(enabled))
}

func (r *renderer) shouldShowThoughts() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.showThoughts
}

func (r *renderer) shouldShowTools() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.showTools
}

func (r *renderer) setTyping(typing bool) {
	r.mu.Lock()
	if r.typing == typing {
		r.mu.Unlock()
		return
	}
	r.typing = typing
	if typing {
		r.spinnerSeq++
		seq := r.spinnerSeq
		r.mu.Unlock()
		go r.runTypingSpinner(seq)
		return
	}
	r.clearSpinnerLocked()
	r.mu.Unlock()
}

func (r *renderer) clear() {
	r.finishActive()
	fmt.Print("\033[2J\033[H")
}

func (r *renderer) printInfo(text string) {
	r.printLine(styleStatus.Render("· "), text)
}

func (r *renderer) printStatusLine(text string) {
	r.printLine(styleStatus.Render("… "), text)
}

func (r *renderer) printError(text string) {
	r.printLine(styleError.Render("✕ "), text)
}

func (r *renderer) printLine(prefix, text string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.ensureOutputLineLocked()
	fmt.Printf("%s%s\n", prefix, text)
}

func (r *renderer) printPanel(title, body string, accent lipgloss.Color) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.ensureOutputLineLocked()
	fmt.Println(r.renderPanel(title, body, accent))
	fmt.Println()
}

func (r *renderer) renderPanel(title, body string, accent lipgloss.Color) string {
	header := lipgloss.NewStyle().Foreground(accent).Bold(true).Render(title)
	rule := styleRule.Render(strings.Repeat("─", minInt(r.panelWidth(), 36)))
	return strings.Join([]string{
		header,
		indentRenderedBlock(styleBody.Render(body), "  "),
		rule,
	}, "\n")
}

func (r *renderer) printTranscriptEntry(icon, title, meta, body string, accent lipgloss.Color) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.ensureOutputLineLocked()
	fmt.Println(r.renderTranscriptEntryString(icon, title, meta, body, accent))
	fmt.Println()
}

func (r *renderer) renderTranscriptEntryString(icon, title, meta, body string, accent lipgloss.Color) string {
	return renderTranscriptEntry(icon, title, meta, body, accent)
}

func (r *renderer) printToolActivity(view messageView) {
	entries := renderToolEntries(view, r.compactTools)
	if len(entries) == 0 {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.ensureOutputLineLocked()
	for idx, entry := range entries {
		if idx > 0 {
			fmt.Println()
		}
		fmt.Println(entry)
	}
	fmt.Println()
}

func (r *renderer) panelWidth() int {
	width := 96
	if detected, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && detected > 0 {
		width = detected - 4
	}
	if width < 52 {
		return 52
	}
	if width > 144 {
		return 144
	}
	return width
}

func (r *renderer) panelContentWidth() int {
	width := r.panelWidth() - 4
	if width < 24 {
		return 24
	}
	return width
}

func (r *renderer) wrapKeyValue(label, value string) string {
	prefix := strings.TrimSpace(label) + ": "
	return wrapWithPrefixes(prefix+strings.TrimSpace(value), prefix, strings.Repeat(" ", len(prefix)), r.panelContentWidth())
}

func (r *renderer) finishActive() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clearSpinnerLocked()
	r.flushAssistantLocked()
}

func (r *renderer) flushAssistantLocked() {
	if !r.activeOpen {
		return
	}

	r.ensureOutputLineLocked()
	fmt.Println(r.renderTranscriptEntryString("●", "Assistant", r.activeModel, renderAssistantMarkdown(r.activeContent), colorAssistant))
	fmt.Println()
	r.activeOpen = false
	r.activeMessageID = ""
	r.activeContent = ""
	r.activeModel = ""
}

func (r *renderer) ensureOutputLineLocked() {
	if !r.promptOpen {
		if r.spinnerVisible {
			r.clearSpinnerLocked()
		}
		return
	}
	fmt.Println()
	r.promptOpen = false
	if r.spinnerVisible {
		r.clearSpinnerLocked()
	}
}

func (r *renderer) clearSpinnerLocked() {
	if !r.spinnerVisible {
		return
	}
	fmt.Print("\r\033[K")
	r.spinnerVisible = false
}

func (r *renderer) runTypingSpinner(seq uint64) {
	ticker := time.NewTicker(90 * time.Millisecond)
	defer ticker.Stop()

	frame := 0
	for {
		r.mu.Lock()
		if !r.typing || r.spinnerSeq != seq {
			r.clearSpinnerLocked()
			r.mu.Unlock()
			return
		}

		if r.promptOpen {
			fmt.Println()
			r.promptOpen = false
		}

		label := styleStatus.Render(typingSpinnerFrames[frame] + " Thinking")
		fmt.Print("\r\033[K" + label)
		r.spinnerVisible = true
		r.mu.Unlock()

		frame = (frame + 1) % len(typingSpinnerFrames)
		<-ticker.C
	}
}

func (r *renderer) handleAssistantCreate(messageID, modelName, content string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.rememberActiveAssistantLocked(messageID, modelName, content)
}

func (r *renderer) handleAssistantUpdate(messageID, modelName, content string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.rememberActiveAssistantLocked(messageID, modelName, content)
}

func (r *renderer) rememberActiveAssistantLocked(messageID, modelName, content string) {
	if r.activeOpen && r.activeMessageID != "" && messageID != "" && r.activeMessageID != messageID {
		r.flushAssistantLocked()
	}

	r.activeMessageID = messageID
	r.activeContent = content
	r.activeModel = modelName
	r.activeOpen = true
	r.scheduleAssistantFlushLocked()
}

func (r *renderer) scheduleAssistantFlushLocked() {
	r.flushSeq++
	seq := r.flushSeq
	time.AfterFunc(assistantFlushDelay, func() {
		r.flushAssistantAfter(seq)
	})
}

func (r *renderer) flushAssistantAfter(seq uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.activeOpen || r.flushSeq != seq {
		return
	}
	r.flushAssistantLocked()
}

func renderAssistantMarkdown(content string) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return styleMuted.Render("(empty response)")
	}

	extensions := (parser.CommonExtensions | parser.NoEmptyLineBeforeBlock) &^ parser.DefinitionLists
	p := parser.NewWithExtensions(extensions)
	doc := markdown.Parse([]byte(trimmed), p)
	if doc == nil {
		return trimmed
	}

	blocks := renderMarkdownBlocks(doc.GetChildren())
	if len(blocks) == 0 {
		return trimmed
	}

	return strings.Join(blocks, "\n\n")
}

func renderMarkdownBlocks(nodes []ast.Node) []string {
	blocks := make([]string, 0, len(nodes))
	for _, node := range nodes {
		block := strings.TrimRight(renderMarkdownBlock(node), "\n")
		if strings.TrimSpace(stripControlWhitespace(block)) == "" {
			continue
		}
		blocks = append(blocks, block)
	}
	return blocks
}

func renderMarkdownBlock(node ast.Node) string {
	switch n := node.(type) {
	case *ast.Paragraph:
		return strings.TrimSpace(renderMarkdownInlineChildren(n.GetChildren()))
	case *ast.Heading:
		text := strings.TrimSpace(renderMarkdownInlineChildren(n.GetChildren()))
		if text == "" {
			return ""
		}
		switch {
		case n.Level <= 1:
			return styleMarkdownH1.Render(text)
		case n.Level == 2:
			return styleMarkdownH2.Render("## " + text)
		default:
			level := n.Level
			if level > 6 {
				level = 6
			}
			return styleMarkdownH3.Render(strings.Repeat("#", level) + " " + text)
		}
	case *ast.BlockQuote:
		inner := strings.Join(renderMarkdownBlocks(n.GetChildren()), "\n\n")
		if strings.TrimSpace(inner) == "" {
			return ""
		}
		prefix := styleMarkdownQuote.Render("│ ")
		return prefixMultiline(inner, prefix, prefix)
	case *ast.List:
		return renderMarkdownList(n)
	case *ast.CodeBlock:
		code := strings.TrimRight(string(n.Literal), "\n")
		if strings.TrimSpace(code) == "" {
			return ""
		}
		return renderCodeFence(code, strings.TrimSpace(string(n.Info)))
	case *ast.HorizontalRule:
		return styleMarkdownRule.Render(strings.Repeat("─", 36))
	case *ast.Table:
		return renderMarkdownTable(n)
	case *ast.HTMLBlock:
		return strings.TrimSpace(string(n.Literal))
	case *ast.Image:
		alt := strings.TrimSpace(renderMarkdownInlineChildren(n.GetChildren()))
		if alt == "" {
			alt = "image"
		}
		dest := strings.TrimSpace(string(n.Destination))
		if dest == "" {
			return styleMuted.Render("[image] " + alt)
		}
		return styleMuted.Render("[image] " + alt + " (" + dest + ")")
	default:
		if leaf := node.AsLeaf(); leaf != nil {
			if literal := strings.TrimSpace(string(leaf.Literal)); literal != "" {
				return literal
			}
		}
		return strings.TrimSpace(renderMarkdownInlineChildren(node.GetChildren()))
	}
}

func renderMarkdownList(list *ast.List) string {
	ordered := list.ListFlags&ast.ListTypeOrdered != 0
	index := list.Start
	if index <= 0 {
		index = 1
	}

	items := make([]string, 0, len(list.GetChildren()))
	for _, child := range list.GetChildren() {
		item, ok := child.(*ast.ListItem)
		if !ok {
			if block := renderMarkdownBlock(child); strings.TrimSpace(block) != "" {
				items = append(items, block)
			}
			continue
		}

		marker := "•"
		if ordered {
			marker = fmt.Sprintf("%d.", index)
			index++
		}
		items = append(items, renderMarkdownListItem(item, marker))
	}

	return strings.Join(items, "\n")
}

func renderMarkdownListItem(item *ast.ListItem, marker string) string {
	blocks := renderMarkdownBlocks(item.GetChildren())
	if len(blocks) == 0 {
		return styleMarkdownBullet.Render(marker)
	}

	parts := make([]string, 0, len(blocks))
	for idx, block := range blocks {
		if idx > 0 {
			parts = append(parts, "")
		}
		parts = append(parts, strings.Split(block, "\n")...)
	}

	firstPrefix := styleMarkdownBullet.Render(marker) + " "
	restPrefix := strings.Repeat(" ", lipgloss.Width(marker)+1)
	return prefixMultiline(strings.Join(parts, "\n"), firstPrefix, restPrefix)
}

func renderMarkdownInlineChildren(nodes []ast.Node) string {
	var b strings.Builder
	for _, node := range nodes {
		b.WriteString(renderMarkdownInline(node))
	}
	return b.String()
}

func renderMarkdownInline(node ast.Node) string {
	switch n := node.(type) {
	case *ast.Text:
		return string(n.Literal)
	case *ast.Code:
		return styleMarkdownCode.Render(string(n.Literal))
	case *ast.Emph:
		return lipgloss.NewStyle().Italic(true).Render(renderMarkdownInlineChildren(n.GetChildren()))
	case *ast.Strong:
		return lipgloss.NewStyle().Bold(true).Render(renderMarkdownInlineChildren(n.GetChildren()))
	case *ast.Del:
		return lipgloss.NewStyle().Strikethrough(true).Render(renderMarkdownInlineChildren(n.GetChildren()))
	case *ast.Link:
		label := strings.TrimSpace(renderMarkdownInlineChildren(n.GetChildren()))
		dest := strings.TrimSpace(string(n.Destination))
		switch {
		case label == "" && dest == "":
			return ""
		case label == "":
			return styleMarkdownLink.Render(dest)
		case dest == "" || label == dest:
			return styleMarkdownLink.Render(label)
		default:
			return styleMarkdownLink.Render(label) + styleMuted.Render(" ("+dest+")")
		}
	case *ast.Image:
		alt := strings.TrimSpace(renderMarkdownInlineChildren(n.GetChildren()))
		if alt == "" {
			alt = "image"
		}
		dest := strings.TrimSpace(string(n.Destination))
		if dest == "" {
			return styleMuted.Render("[image] " + alt)
		}
		return styleMuted.Render("[image] " + alt + " (" + dest + ")")
	case *ast.Softbreak, *ast.NonBlockingSpace:
		return " "
	case *ast.Hardbreak:
		return "\n"
	case *ast.HTMLSpan:
		return string(n.Literal)
	default:
		if leaf := node.AsLeaf(); leaf != nil && len(leaf.Literal) > 0 {
			return string(leaf.Literal)
		}
		return renderMarkdownInlineChildren(node.GetChildren())
	}
}

func renderMarkdownTable(table *ast.Table) string {
	rows := make([][]string, 0)
	headerRows := 0

	for _, child := range table.GetChildren() {
		switch section := child.(type) {
		case *ast.TableHeader:
			for _, rowNode := range section.GetChildren() {
				if row := renderMarkdownTableRow(rowNode); len(row) > 0 {
					rows = append(rows, row)
					headerRows++
				}
			}
		case *ast.TableBody:
			for _, rowNode := range section.GetChildren() {
				if row := renderMarkdownTableRow(rowNode); len(row) > 0 {
					rows = append(rows, row)
				}
			}
		case *ast.TableRow:
			if row := renderMarkdownTableRow(section); len(row) > 0 {
				rows = append(rows, row)
			}
		}
	}

	if len(rows) == 0 {
		return ""
	}
	if headerRows == 0 && len(rows) > 1 {
		headerRows = 1
	}

	widths := make([]int, 0)
	for _, row := range rows {
		for len(widths) < len(row) {
			widths = append(widths, 0)
		}
		for i, cell := range row {
			if width := lipgloss.Width(cell); width > widths[i] {
				widths[i] = width
			}
		}
	}

	lines := make([]string, 0, len(rows)+1)
	for idx, row := range rows {
		lines = append(lines, renderMarkdownTableLine(row, widths))
		if headerRows > 0 && idx == headerRows-1 {
			lines = append(lines, renderMarkdownTableDivider(widths))
		}
	}

	return strings.Join(lines, "\n")
}

func renderMarkdownTableRow(node ast.Node) []string {
	rowNode, ok := node.(*ast.TableRow)
	if !ok {
		return nil
	}

	cells := make([]string, 0, len(rowNode.GetChildren()))
	for _, child := range rowNode.GetChildren() {
		cell, ok := child.(*ast.TableCell)
		if !ok {
			continue
		}
		cells = append(cells, strings.TrimSpace(renderMarkdownPlainText(cell)))
	}
	return cells
}

func renderMarkdownTableLine(row []string, widths []int) string {
	cells := make([]string, len(widths))
	for i := range widths {
		cell := ""
		if i < len(row) {
			cell = row[i]
		}
		cells[i] = padRightVisible(cell, widths[i])
	}
	return "| " + strings.Join(cells, " | ") + " |"
}

func renderMarkdownTableDivider(widths []int) string {
	parts := make([]string, len(widths))
	for i, width := range widths {
		if width < 3 {
			width = 3
		}
		parts[i] = strings.Repeat("-", width)
	}
	return "|-" + strings.Join(parts, "-|-") + "-|"
}

func renderMarkdownPlainText(node ast.Node) string {
	switch n := node.(type) {
	case *ast.Text:
		return string(n.Literal)
	case *ast.Code:
		return string(n.Literal)
	case *ast.Softbreak, *ast.Hardbreak, *ast.NonBlockingSpace:
		return " "
	case *ast.Link:
		label := strings.TrimSpace(renderMarkdownPlainTextChildren(n.GetChildren()))
		dest := strings.TrimSpace(string(n.Destination))
		switch {
		case label == "" && dest == "":
			return ""
		case label == "":
			return dest
		case dest == "" || label == dest:
			return label
		default:
			return label + " (" + dest + ")"
		}
	default:
		if leaf := node.AsLeaf(); leaf != nil && len(leaf.Literal) > 0 {
			return string(leaf.Literal)
		}
		return renderMarkdownPlainTextChildren(node.GetChildren())
	}
}

func renderMarkdownPlainTextChildren(nodes []ast.Node) string {
	var b strings.Builder
	for _, node := range nodes {
		b.WriteString(renderMarkdownPlainText(node))
	}
	return b.String()
}

func prefixMultiline(text, firstPrefix, restPrefix string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		prefix := restPrefix
		if i == 0 {
			prefix = firstPrefix
		}
		if line == "" {
			lines[i] = prefix
			continue
		}
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}

func indentRenderedBlock(text, prefix string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if line == "" {
			continue
		}
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}

func renderCodeFence(text, info string) string {
	lines := make([]string, 0, 2)
	if strings.TrimSpace(info) != "" {
		lines = append(lines, styleMuted.Render(info))
	}
	lines = append(lines, prefixMultiline(text, styleRule.Render("│ "), styleRule.Render("│ ")))
	return strings.Join(lines, "\n")
}

func padRightVisible(text string, width int) string {
	padding := width - lipgloss.Width(text)
	if padding <= 0 {
		return text
	}
	return text + strings.Repeat(" ", padding)
}

func wrapWithPrefixes(text, firstPrefix, restPrefix string, width int) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return strings.TrimRight(firstPrefix, " ")
	}

	if width < 8 {
		width = 8
	}

	var lines []string
	currentPrefix := firstPrefix
	remaining := trimmed

	for len(remaining) > 0 {
		lineWidth := width - lipgloss.Width(currentPrefix)
		if lineWidth < 8 {
			lineWidth = 8
		}

		chunk, rest := wrapChunk(remaining, lineWidth)
		lines = append(lines, currentPrefix+chunk)
		remaining = strings.TrimLeft(rest, " ")
		currentPrefix = restPrefix
	}

	return strings.Join(lines, "\n")
}

func wrapChunk(text string, width int) (string, string) {
	runes := []rune(text)
	if len(runes) == 0 {
		return "", ""
	}
	if lipgloss.Width(text) <= width {
		return text, ""
	}

	lastBreak := -1
	for i := 0; i < len(runes); i++ {
		chunk := string(runes[:i+1])
		if lipgloss.Width(chunk) > width {
			if lastBreak > 0 {
				return strings.TrimSpace(string(runes[:lastBreak])), strings.TrimSpace(string(runes[lastBreak:]))
			}
			if i == 0 {
				return string(runes[:1]), strings.TrimSpace(string(runes[1:]))
			}
			return strings.TrimSpace(string(runes[:i])), strings.TrimSpace(string(runes[i:]))
		}

		switch runes[i] {
		case ' ', '/', '?', '&', '=', '-', '_', ':':
			lastBreak = i + 1
		}
	}

	return text, ""
}

func stripControlWhitespace(text string) string {
	text = ansiEscapePattern.ReplaceAllString(text, "")

	var b strings.Builder
	b.Grow(len(text))
	for _, r := range text {
		if r == '\n' || r == '\r' || r == '\t' || unicode.IsPrint(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
