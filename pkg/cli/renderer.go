package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

const assistantFlushDelay = 180 * time.Millisecond

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

	out          io.Writer
	managedInput bool
	overlay      transientLine

	messages map[string]messageView
}

func newRenderer(wsURL, sessionID string, showThoughts, showTools, compactTools bool, out io.Writer, managedInput bool, overlay transientLine) *renderer {
	return &renderer{
		wsURL:         wsURL,
		sessionID:     sessionID,
		showThoughts:  showThoughts,
		showTools:     showTools,
		compactTools:  compactTools,
		out:           out,
		managedInput:  managedInput,
		overlay:       overlay,
		messages:      make(map[string]messageView),
		activeOpen:    false,
		activeContent: "",
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

func (r *renderer) printWelcome() {
	body := strings.Join([]string{
		r.wrapKeyValue("Connected to", r.wsURL),
		r.wrapKeyValue("Session", r.sessionID),
		r.wrapKeyValue("Thoughts", onOff(r.showThoughts)+"   Tools: "+onOff(r.showTools)),
		r.wrapKeyValue("Tool view", ternary(r.compactTools, "compact", "full")),
		r.wrapKeyValue("Assistant replies", "markdown-rendered"),
		r.wrapKeyValue("Commands", "/help /status /thoughts on|off /tools on|off /clear /quit"),
		r.wrapKeyValue("Input", fmt.Sprintf("Use up/down for saved history (last %d), live hints + Tab for /commands, multiline paste is previewed before send", defaultHistoryMaxEntries)),
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
		"Use the up/down arrow keys to browse previously submitted input.",
		"Live suggestions appear while typing local /commands.",
		"Press Tab to autocomplete local /commands and toggle values.",
		fmt.Sprintf("History is persisted between sessions and keeps the last %d entries.", defaultHistoryMaxEntries),
		"Multiline paste stays in one draft and is previewed before send; Enter sends it, Backspace or Ctrl+U discard it.",
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
	managed := r.managedInput
	overlay := r.overlay
	if typing {
		if managed {
			r.spinnerSeq++
			seq := r.spinnerSeq
			r.mu.Unlock()
			go r.runTypingSpinner(seq)
			return
		}
		r.spinnerSeq++
		seq := r.spinnerSeq
		r.mu.Unlock()
		go r.runTypingSpinner(seq)
		return
	}
	if managed {
		r.mu.Unlock()
		if overlay != nil {
			overlay.ClearSpinner()
		}
		return
	}
	r.clearSpinnerLocked()
	r.mu.Unlock()
}

func (r *renderer) clear() {
	r.finishActive()
	fmt.Fprint(r.out, "\033[2J\033[H")
}

func (r *renderer) printPrompt() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.flushAssistantLocked()
	fmt.Fprint(r.out, stylePrompt.Render(promptText))
	r.promptOpen = true
}

func (r *renderer) closePrompt() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.promptOpen = false
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
	fmt.Fprintf(r.out, "%s%s\n", prefix, text)
}

func (r *renderer) printPanel(title, body string, accent lipgloss.Color) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.ensureOutputLineLocked()
	fmt.Fprintln(r.out, r.renderPanel(title, body, accent))
	fmt.Fprintln(r.out)
}

func (r *renderer) renderPanel(title, body string, accent lipgloss.Color) string {
	ruleWidth := r.panelWidth()
	if ruleWidth > 36 {
		ruleWidth = 36
	}

	header := lipgloss.NewStyle().Foreground(accent).Bold(true).Render(title)
	rule := styleRule.Render(strings.Repeat("─", ruleWidth))
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
	fmt.Fprintln(r.out, renderTranscriptEntry(icon, title, meta, body, accent))
	fmt.Fprintln(r.out)
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
			fmt.Fprintln(r.out)
		}
		fmt.Fprintln(r.out, entry)
	}
	fmt.Fprintln(r.out)
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
	fmt.Fprintln(r.out, renderTranscriptEntry("●", "Assistant", r.activeModel, renderAssistantMarkdown(r.activeContent), colorAssistant))
	fmt.Fprintln(r.out)
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
	fmt.Fprintln(r.out)
	r.promptOpen = false
	if r.spinnerVisible {
		r.clearSpinnerLocked()
	}
}

func (r *renderer) clearSpinnerLocked() {
	if !r.spinnerVisible {
		return
	}
	fmt.Fprint(r.out, "\r\033[K")
	r.spinnerVisible = false
}

func (r *renderer) runTypingSpinner(seq uint64) {
	ticker := time.NewTicker(90 * time.Millisecond)
	defer ticker.Stop()

	frame := 0
	for {
		r.mu.Lock()
		managed := r.managedInput
		overlay := r.overlay
		if !r.typing || r.spinnerSeq != seq {
			if !managed {
				r.clearSpinnerLocked()
			}
			r.mu.Unlock()
			if managed && overlay != nil {
				overlay.ClearSpinner()
			}
			return
		}

		label := styleStatus.Render(typingSpinnerFrames[frame] + " Thinking")
		if managed {
			r.mu.Unlock()
			if overlay != nil {
				overlay.ShowSpinner(label)
			}
		} else {
			if r.promptOpen {
				fmt.Fprintln(r.out)
				r.promptOpen = false
			}

			fmt.Fprint(r.out, "\r\033[K"+label)
			r.spinnerVisible = true
			r.mu.Unlock()
		}

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
