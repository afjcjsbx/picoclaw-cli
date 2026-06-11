package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type tuiMessage struct {
	message picoMessage
}

type tuiError struct {
	err error
}

type tuiSendResult struct {
	err error
}

type tuiEntryKind int

const (
	tuiEntryMessage tuiEntryKind = iota
	tuiEntryRaw
)

type tuiEntry struct {
	kind tuiEntryKind
	id   string
	view messageView
	raw  string
}

type tuiModel struct {
	wsURL   string
	opts    cliOptions
	session string
	conn    *safeConn
	history *persistentHistory

	input    textarea.Model
	viewport viewport.Model
	spinner  spinner.Model

	width  int
	height int

	entries           []tuiEntry
	messageViews      map[string]messageView
	messageEntryIndex map[string]int
	turnStartIndex    int

	status             string
	typing             bool
	lastPasteLineCount int
	followTranscript   bool
	selection          transcriptSelection

	historyIndex       int
	draftBeforeHistory string
}

func runBubbleTeaCLI(ctx context.Context, wsURL string, opts cliOptions, conn *websocket.Conn, ws *safeConn) error {
	model := newTUIModel(wsURL, opts, ws)
	programOptions := []tea.ProgramOption{
		tea.WithAltScreen(),
		tea.WithContext(ctx),
	}
	if opts.mouseMode {
		programOptions = append(programOptions, tea.WithMouseCellMotion())
	}
	program := tea.NewProgram(model, programOptions...)

	go readLoopToProgram(ctx, conn, program)
	if opts.pingEvery > 0 {
		go pingLoop(ctx, ws, opts.pingEvery)
	}

	_, err := program.Run()
	return err
}

func readLoopToProgram(ctx context.Context, conn *websocket.Conn, program *tea.Program) {
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
			program.Send(tuiError{err: fmt.Errorf("connection closed: %w", err)})
			return
		}
		program.Send(tuiMessage{message: msg})
	}
}

func newTUIModel(wsURL string, opts cliOptions, conn *safeConn) tuiModel {
	input := textarea.New()
	input.Prompt = promptText
	input.Placeholder = "Scrivi un messaggio. Enter invia, Ctrl+J va a capo."
	input.ShowLineNumbers = false
	input.EndOfBufferCharacter = ' '
	input.CharLimit = 0
	input.MaxHeight = 0
	input.FocusedStyle.Prompt = stylePrompt
	input.BlurredStyle.Prompt = stylePrompt
	input.FocusedStyle.Placeholder = styleMuted
	input.BlurredStyle.Placeholder = styleMuted
	input.FocusedStyle.CursorLine = lipgloss.NewStyle()
	input.BlurredStyle.CursorLine = lipgloss.NewStyle()
	input.FocusedStyle.Text = styleBody
	input.BlurredStyle.Text = styleBody
	input.SetWidth(80)
	input.SetHeight(3)
	_ = input.Focus()

	spin := spinner.New(spinner.WithSpinner(spinner.Spinner{
		Frames: typingSpinnerFrames,
		FPS:    90 * time.Millisecond,
	}), spinner.WithStyle(styleStatus))

	history, historyStatus := loadTUIHistory()
	model := tuiModel{
		wsURL:             wsURL,
		opts:              opts,
		session:           opts.sessionID,
		conn:              conn,
		history:           history,
		input:             input,
		viewport:          viewport.New(80, 12),
		spinner:           spin,
		messageViews:      make(map[string]messageView),
		messageEntryIndex: make(map[string]int),
		turnStartIndex:    0,
		status:            "Ready",
		followTranscript:  true,
		historyIndex:      -1,
	}
	model.viewport.MouseWheelEnabled = opts.mouseMode
	if historyStatus != "" {
		model.addInfo(historyStatus)
	}
	return model
}

func loadTUIHistory() (*persistentHistory, string) {
	path, err := defaultHistoryPath()
	if err != nil {
		return nil, fmt.Sprintf("history disabled: %v", err)
	}
	history, err := newPersistentHistory(path, defaultHistoryMaxEntries)
	if err != nil {
		return nil, fmt.Sprintf("history disabled: %v", err)
	}
	return history, ""
}

func (m tuiModel) Init() tea.Cmd {
	return m.input.Focus()
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.clearSelection()
		m.resize(msg.Width, msg.Height)
		m.refreshViewport(true)
		return m, nil
	case tea.MouseMsg:
		return m.handleMouse(msg)
	case tuiMessage:
		cmd := m.handlePicoMessage(msg.message)
		m.refreshViewport(m.followTranscript)
		return m, cmd
	case tuiError:
		m.addError(msg.err.Error())
		m.refreshViewport(true)
		return m, nil
	case tuiSendResult:
		if msg.err != nil {
			m.addError(fmt.Sprintf("send failed: %v", msg.err))
			m.refreshViewport(true)
		}
		return m, nil
	case spinner.TickMsg:
		var cmd tea.Cmd
		if m.typing {
			m.spinner, cmd = m.spinner.Update(msg)
			m.refreshViewport(m.followTranscript)
		}
		return m, cmd
	case tea.KeyMsg:
		m.clearSelection()
		return m.handleKey(msg)
	default:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.resize(m.width, m.height)
		return m, cmd
	}
}

func (m tuiModel) View() string {
	if m.width <= 0 {
		return ""
	}

	return strings.Join([]string{
		m.renderViewportView(),
		m.renderInput(),
	}, "\n")
}

func (m tuiModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "enter":
		return m.submitInput()
	case "ctrl+j":
		m.input.InsertString("\n")
		m.historyIndex = -1
		m.resize(m.width, m.height)
		return m, nil
	case "ctrl+p":
		m.historyPrev()
		m.resize(m.width, m.height)
		return m, nil
	case "ctrl+n":
		m.historyNext()
		m.resize(m.width, m.height)
		return m, nil
	case "pgup", "pgdown":
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		m.followTranscript = m.viewport.AtBottom()
		return m, cmd
	case "up":
		if m.inputLineCount() <= 1 && m.historyPrev() {
			m.resize(m.width, m.height)
			return m, nil
		}
	case "down":
		if m.inputLineCount() <= 1 && m.historyNext() {
			m.resize(m.width, m.height)
			return m, nil
		}
	}

	var cmd tea.Cmd
	if msg.Paste {
		m.lastPasteLineCount = strings.Count(string(msg.Runes), "\n") + 1
		if m.lastPasteLineCount > 1 {
			m.status = fmt.Sprintf("Pasted %d lines into draft", m.lastPasteLineCount)
		}
	}
	m.historyIndex = -1
	m.input, cmd = m.input.Update(msg)
	m.resize(m.width, m.height)
	return m, cmd
}

func (m tuiModel) submitInput() (tea.Model, tea.Cmd) {
	content, ok := submittedContent(m.input.Value())
	if !ok {
		m.input.SetValue("")
		m.resize(m.width, m.height)
		return m, nil
	}

	trimmed := strings.TrimSpace(content)
	if isLocalCommandLine(content) {
		handled, quit := m.handleLocalCommand(trimmed)
		if handled {
			m.input.SetValue("")
			m.historyIndex = -1
			m.refreshViewport(true)
			m.resize(m.width, m.height)
			if quit {
				return m, tea.Quit
			}
			return m, nil
		}
	}

	if m.history != nil {
		m.history.Add(content)
	}
	m.addUserMessage(content)
	m.input.SetValue("")
	m.historyIndex = -1
	m.status = "Message sent"
	m.followTranscript = true
	m.refreshViewport(true)
	m.resize(m.width, m.height)

	msg := picoMessage{
		Type:      typeMessageSend,
		ID:        uuid.NewString(),
		SessionID: m.session,
		Timestamp: time.Now().UnixMilli(),
		Payload: map[string]any{
			payloadKeyContent: content,
		},
	}
	return m, sendPicoMessageCmd(m.conn, msg)
}

func sendPicoMessageCmd(conn *safeConn, msg picoMessage) tea.Cmd {
	return func() tea.Msg {
		if err := conn.WriteJSON(msg); err != nil {
			return tuiSendResult{err: err}
		}
		return tuiSendResult{}
	}
}

func (m *tuiModel) handlePicoMessage(msg picoMessage) tea.Cmd {
	payload := msg.Payload
	if payload == nil {
		payload = map[string]any{}
	}

	switch msg.Type {
	case typeTypingStart:
		m.typing = true
		m.messageViews = make(map[string]messageView)
		m.messageEntryIndex = make(map[string]int)
		m.turnStartIndex = len(m.entries)
		return m.spinner.Tick
	case typeTypingStop:
		m.typing = false
		return nil
	case typeMessageDelete:
		messageID, _ := payload["message_id"].(string)
		m.addStatus(fmt.Sprintf("message deleted: %s", strings.TrimSpace(messageID)))
		return nil
	case typeError:
		message, _ := payload["message"].(string)
		code, _ := payload["code"].(string)
		if code != "" {
			m.addError(fmt.Sprintf("%s: %s", code, message))
		} else {
			m.addError(message)
		}
		return nil
	case typePong:
		return nil
	case typeMessageCreate, typeMessageUpdate, typeMediaCreate:
	default:
		m.addStatus(fmt.Sprintf("received %s", msg.Type))
		return nil
	}

	m.typing = false
	messageID, _ := payload["message_id"].(string)
	prev := m.messageViews[messageID]
	next := buildMessageView(payload, prev)
	if messageID != "" {
		m.messageViews[messageID] = next
	}
	switch msg.Type {
	case typeMessageCreate, typeMediaCreate:
		m.appendMessageEntry(messageID, next)
	case typeMessageUpdate:
		m.upsertMessageEntry(messageID, next)
	}
	return nil
}

func (m *tuiModel) handleLocalCommand(input string) (handled bool, quit bool) {
	fields := strings.Fields(input)
	if len(fields) == 0 {
		return false, false
	}

	switch fields[0] {
	case "/quit", "/exit":
		return true, true
	case "/help":
		m.addRaw(m.renderHelp())
		m.followTranscript = true
		return true, false
	case "/status":
		m.addRaw(m.renderSessionStatus())
		m.followTranscript = true
		return true, false
	case "/session":
		m.addInfo("Session: " + m.session)
		m.followTranscript = true
		return true, false
	case "/clear":
		m.entries = nil
		m.messageViews = make(map[string]messageView)
		m.messageEntryIndex = make(map[string]int)
		m.turnStartIndex = 0
		m.status = "Transcript cleared"
		m.followTranscript = true
		return true, false
	case "/thoughts":
		if len(fields) == 1 {
			m.addInfo("Thoughts: " + onOff(m.opts.showThoughts))
			return true, false
		}
		next, ok := parseToggle(fields[1])
		if !ok {
			m.addError("usage: /thoughts on|off")
			return true, false
		}
		m.opts.showThoughts = next
		m.addInfo("Thoughts: " + onOff(next))
		m.followTranscript = true
		return true, false
	case "/tools":
		if len(fields) == 1 {
			m.addInfo("Tools: " + onOff(m.opts.showTools))
			return true, false
		}
		next, ok := parseToggle(fields[1])
		if !ok {
			m.addError("usage: /tools on|off")
			return true, false
		}
		m.opts.showTools = next
		m.addInfo("Tools: " + onOff(next))
		m.followTranscript = true
		return true, false
	default:
		return false, false
	}
}

func (m *tuiModel) appendMessageEntry(messageID string, view messageView) {
	entry := tuiEntry{kind: tuiEntryMessage, id: messageID, view: view}
	m.entries = append(m.entries, entry)
	if messageID != "" {
		m.messageEntryIndex[messageID] = len(m.entries) - 1
	}
}

func (m *tuiModel) upsertMessageEntry(messageID string, view messageView) {
	entry := tuiEntry{kind: tuiEntryMessage, id: messageID, view: view}
	if messageID != "" {
		if idx, ok := m.messageEntryIndex[messageID]; ok && idx >= m.turnStartIndex && idx < len(m.entries) && m.entries[idx].kind == tuiEntryMessage && m.entries[idx].view.kind == view.kind {
			m.entries[idx] = entry
			return
		}
	}
	m.appendMessageEntry(messageID, view)
}

func (m *tuiModel) addUserMessage(content string) {
	body := content
	if strings.Contains(content, "\n") {
		body = renderCodeFence(content, "")
	}
	m.addRaw(renderTranscriptEntry("›", "You", "", body, colorUser))
	m.turnStartIndex = len(m.entries)
}

func (m *tuiModel) addInfo(text string) {
	m.addRaw(styleStatus.Render("· ") + text)
	m.status = text
}

func (m *tuiModel) addStatus(text string) {
	m.addRaw(styleStatus.Render("… ") + text)
	m.status = text
}

func (m *tuiModel) addError(text string) {
	m.addRaw(styleError.Render("✕ ") + text)
	m.status = text
}

func (m *tuiModel) addRaw(rendered string) {
	m.entries = append(m.entries, tuiEntry{kind: tuiEntryRaw, raw: rendered})
}

func (m *tuiModel) resize(width, height int) {
	if width <= 0 || height <= 0 {
		return
	}
	m.width = width
	m.height = height

	inputHeight := m.inputLineCount() + 1
	if inputHeight < 3 {
		inputHeight = 3
	}
	if inputHeight > 8 {
		inputHeight = 8
	}
	m.input.SetWidth(width)
	m.input.SetHeight(inputHeight)

	viewportHeight := height - inputHeight - 1
	if viewportHeight < 1 {
		viewportHeight = 1
	}
	m.viewport.Width = width
	m.viewport.Height = viewportHeight
	m.refreshViewport(m.followTranscript)
}

func (m *tuiModel) inputLineCount() int {
	return (&m.input).LineCount()
}

func (m *tuiModel) refreshViewport(forceBottom bool) {
	oldYOffset := m.viewport.YOffset
	m.viewport.SetContent(m.renderTranscript())
	if forceBottom || m.followTranscript {
		m.viewport.GotoBottom()
		return
	}
	m.viewport.SetYOffset(oldYOffset)
}

func (m tuiModel) renderHeader() string {
	mouseMode := "native terminal selection/copy"
	if m.opts.mouseMode {
		mouseMode = "captured for wheel scroll + drag copy"
	}
	body := strings.Join([]string{
		wrapWithPrefixes("Connected to: "+m.wsURL, "Connected to: ", strings.Repeat(" ", len("Connected to: ")), m.panelContentWidth()),
		wrapWithPrefixes("Session: "+m.session, "Session: ", strings.Repeat(" ", len("Session: ")), m.panelContentWidth()),
		"Thoughts: " + onOff(m.opts.showThoughts),
		"Tools: " + onOff(m.opts.showTools),
		"Tool view: " + ternary(m.opts.compactTools, "compact", "full"),
		wrapWithPrefixes("Mouse: "+mouseMode, "Mouse: ", strings.Repeat(" ", len("Mouse: ")), m.panelContentWidth()),
		wrapWithPrefixes("Commands: /help /status /thoughts on|off /tools on|off /clear /quit", "Commands: ", strings.Repeat(" ", len("Commands: ")), m.panelContentWidth()),
		wrapWithPrefixes("Input: textarea mode; paste is native, Enter sends, Ctrl+J inserts newline", "Input: ", strings.Repeat(" ", len("Input: ")), m.panelContentWidth()),
	}, "\n")
	return renderPanelForWidth("Remote Pico CLI", body, colorAssistant, m.panelWidth())
}

func (m tuiModel) renderStatus() string {
	parts := make([]string, 0, 3)
	if m.typing {
		parts = append(parts, m.spinner.View()+" Thinking")
	}
	if strings.TrimSpace(m.status) != "" {
		parts = append(parts, styleMuted.Render(m.status))
	}
	if len(parts) == 0 {
		parts = append(parts, styleMuted.Render("Ready"))
	}
	return lipgloss.NewStyle().Width(m.width).Render(strings.Join(parts, "  "))
}

func (m tuiModel) renderInput() string {
	return m.input.View()
}

func (m tuiModel) renderTranscript() string {
	rendered := make([]string, 0, len(m.entries)+3)
	if header := m.renderHeader(); strings.TrimSpace(stripControlWhitespace(header)) != "" {
		rendered = append(rendered, header)
	}
	if len(m.entries) == 0 {
		rendered = append(rendered, styleMuted.Render("No messages yet."))
	} else {
		width := m.viewport.Width
		if width <= 0 {
			width = m.width
		}

		for _, entry := range m.entries {
			text, ok := m.renderEntry(entry)
			if ok && strings.TrimSpace(stripControlWhitespace(text)) != "" {
				rendered = append(rendered, wrapTranscriptBlock(text, width))
			}
		}
	}
	if m.typing {
		rendered = append(rendered, styleStatus.Render(m.spinner.View()+" Thinking"))
	}
	if len(rendered) == 0 {
		return styleMuted.Render("No visible messages.")
	}
	return strings.Join(rendered, "\n\n")
}

func (m tuiModel) renderEntry(entry tuiEntry) (string, bool) {
	if entry.kind == tuiEntryRaw {
		return entry.raw, true
	}

	switch entry.view.kind {
	case displayPlaceholder:
		text := strings.TrimSpace(entry.view.content)
		if text == "" {
			return "", false
		}
		return styleStatus.Render("… ") + text, true
	case displayThought:
		if !m.opts.showThoughts {
			return "", false
		}
		return renderTranscriptEntry("◌", "Thought", entry.view.modelName, entry.view.content, colorThought), true
	case displayToolCalls, displayToolFeedback:
		if !m.opts.showTools {
			return "", false
		}
		entries := renderToolEntries(entry.view, m.opts.compactTools)
		return strings.Join(entries, "\n\n"), len(entries) > 0
	case displayNormal:
		return renderTranscriptEntry("●", "Assistant", entry.view.modelName, renderAssistantMarkdown(entry.view.content), colorAssistant), true
	default:
		return "", false
	}
}

func (m tuiModel) renderHelp() string {
	lines := []string{
		"/help                Show commands",
		"/status              Show current session and display settings",
		"/session             Print the current session id",
		"/thoughts on|off     Toggle structured thought visibility",
		"/tools on|off        Toggle tool event visibility",
		"/clear               Clear the transcript",
		"/quit                Exit",
		"",
		"Enter sends the current draft.",
		"Ctrl+J inserts a newline.",
		"Paste keeps multiline structure in the textarea.",
		"Use PgUp/PgDn to scroll the transcript.",
		"Use Up/Down or Ctrl+P/Ctrl+N for input history.",
		fmt.Sprintf("History is persisted between sessions and keeps the last %d entries.", defaultHistoryMaxEntries),
	}
	if m.opts.mouseMode {
		lines = append(lines,
			"Mouse wheel scrolling is enabled.",
			"Drag on the transcript to copy the visible selection to the clipboard.",
			"Right-click an active transcript selection to copy it again.",
		)
	} else {
		lines = append(lines,
			"Mouse capture is off, so your terminal handles text selection and copy natively.",
			"Restart with -mouse if you want wheel scrolling and drag-to-copy inside the app.",
		)
	}
	body := strings.Join(lines, "\n")
	return renderPanelForWidth("Help", body, colorAssistant, m.panelWidth())
}

func (m tuiModel) renderSessionStatus() string {
	mouseMode := "native terminal selection/copy"
	if m.opts.mouseMode {
		mouseMode = "captured for wheel scroll + drag copy"
	}
	body := strings.Join([]string{
		wrapWithPrefixes("URL: "+m.wsURL, "URL: ", strings.Repeat(" ", len("URL: ")), m.panelContentWidth()),
		"Session: " + m.session,
		"Thoughts: " + onOff(m.opts.showThoughts),
		"Tools: " + onOff(m.opts.showTools),
		"Tool view: " + ternary(m.opts.compactTools, "compact", "full"),
		"Mouse: " + mouseMode,
		"Assistant: markdown-rendered",
		"Typing: " + onOff(m.typing),
		"Input: Bubble Tea textarea",
	}, "\n")
	return renderPanelForWidth("Status", body, colorAssistant, m.panelWidth())
}

func (m tuiModel) panelWidth() int {
	width := m.width
	if width <= 0 {
		width = 96
	}
	width -= 4
	if width < 12 {
		return 12
	}
	if width > 144 {
		return 144
	}
	return width
}

func (m tuiModel) panelContentWidth() int {
	width := m.panelWidth() - 4
	if width < 8 {
		return 8
	}
	return width
}

func renderPanelForWidth(title, body string, accent lipgloss.Color, width int) string {
	ruleWidth := width
	if ruleWidth > 36 {
		ruleWidth = 36
	}
	if ruleWidth < 8 {
		ruleWidth = 8
	}

	header := lipgloss.NewStyle().Foreground(accent).Bold(true).Render(title)
	rule := styleRule.Render(strings.Repeat("─", ruleWidth))
	return strings.Join([]string{
		header,
		indentRenderedBlock(styleBody.Render(body), "  "),
		rule,
	}, "\n")
}

func wrapTranscriptBlock(text string, width int) string {
	if width <= 0 {
		return text
	}

	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = ansi.Wrap(line, width, "")
	}
	return strings.Join(lines, "\n")
}

func (m *tuiModel) historyPrev() bool {
	if m.history == nil || m.history.Len() == 0 {
		return false
	}
	if m.historyIndex == -1 {
		m.draftBeforeHistory = m.input.Value()
	}
	if m.historyIndex < m.history.Len()-1 {
		m.historyIndex++
	}
	m.input.SetValue(m.history.At(m.historyIndex))
	m.status = "History"
	return true
}

func (m *tuiModel) historyNext() bool {
	if m.history == nil || m.historyIndex == -1 {
		return false
	}
	if m.historyIndex > 0 {
		m.historyIndex--
		m.input.SetValue(m.history.At(m.historyIndex))
	} else {
		m.historyIndex = -1
		m.input.SetValue(m.draftBeforeHistory)
		m.draftBeforeHistory = ""
	}
	m.status = "History"
	return true
}
