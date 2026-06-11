package cli

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestTUIEnterSubmitsDraftAndClearsInput(t *testing.T) {
	model := newTUITestModel()
	model.input.SetValue("ciao pico")

	updated, cmd := model.handleKey(tea.KeyMsg{Type: tea.KeyEnter})

	next := updated.(tuiModel)
	if next.input.Value() != "" {
		t.Fatalf("input after submit = %q, want empty", next.input.Value())
	}
	if len(next.entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(next.entries))
	}
	if !strings.Contains(next.entries[0].raw, "ciao pico") {
		t.Fatalf("user entry = %q, want submitted content", next.entries[0].raw)
	}
	if cmd == nil {
		t.Fatal("submit command = nil, want send command")
	}
}

func TestTUIPasteKeepsMultilineDraft(t *testing.T) {
	model := newTUITestModel()

	updated, _ := model.handleKey(tea.KeyMsg{
		Type:  tea.KeyRunes,
		Runes: []rune("first\nsecond"),
		Paste: true,
	})

	next := updated.(tuiModel)
	if next.input.Value() != "first\nsecond" {
		t.Fatalf("input value = %q, want multiline paste", next.input.Value())
	}
	if !strings.Contains(next.status, "Pasted 2 lines") {
		t.Fatalf("status = %q, want paste summary", next.status)
	}
}

func TestTUILocalHelpCommandDoesNotSend(t *testing.T) {
	model := newTUITestModel()
	model.input.SetValue("/help")

	updated, cmd := model.handleKey(tea.KeyMsg{Type: tea.KeyEnter})

	next := updated.(tuiModel)
	if cmd != nil {
		t.Fatal("help command returned send command, want nil")
	}
	if len(next.entries) != 1 || !strings.Contains(next.entries[0].raw, "Help") {
		t.Fatalf("entries = %#v, want help panel", next.entries)
	}
}

func TestTUIHistoryNavigation(t *testing.T) {
	model := newTUITestModel()
	model.history = &persistentHistory{maxEntries: 10}
	model.history.Add("first")
	model.history.Add("second")

	if !model.historyPrev() {
		t.Fatal("historyPrev() = false, want true")
	}
	if model.input.Value() != "second" {
		t.Fatalf("input after first prev = %q, want newest", model.input.Value())
	}
	if !model.historyPrev() {
		t.Fatal("second historyPrev() = false, want true")
	}
	if model.input.Value() != "first" {
		t.Fatalf("input after second prev = %q, want older", model.input.Value())
	}
	if !model.historyNext() {
		t.Fatal("historyNext() = false, want true")
	}
	if model.input.Value() != "second" {
		t.Fatalf("input after next = %q, want newer", model.input.Value())
	}
}

func TestTUIMessageCreatesAppendEvenWithSameID(t *testing.T) {
	model := newTUITestModel()
	msg := picoMessage{
		Type: typeMessageCreate,
		Payload: map[string]any{
			"message_id":      "shared-id",
			payloadKeyContent: "first message",
		},
	}

	model.handlePicoMessage(msg)
	model.handlePicoMessage(picoMessage{
		Type: typeMessageCreate,
		Payload: map[string]any{
			"message_id":      "shared-id",
			payloadKeyContent: "second message",
		},
	})

	if len(model.entries) != 2 {
		t.Fatalf("entries = %d, want 2 appended messages", len(model.entries))
	}
	if !strings.Contains(model.renderTranscript(), "first message") || !strings.Contains(model.renderTranscript(), "second message") {
		t.Fatalf("rendered transcript = %q, want both messages preserved", model.renderTranscript())
	}
}

func TestTUIMessageUpdateReplacesLatestMatchingEntry(t *testing.T) {
	model := newTUITestModel()
	model.handlePicoMessage(picoMessage{
		Type: typeMessageCreate,
		Payload: map[string]any{
			"message_id":      "shared-id",
			payloadKeyContent: "first draft",
		},
	})
	model.handlePicoMessage(picoMessage{
		Type: typeMessageUpdate,
		Payload: map[string]any{
			"message_id":      "shared-id",
			payloadKeyContent: "final text",
		},
	})

	if len(model.entries) != 1 {
		t.Fatalf("entries = %d, want 1 updated message", len(model.entries))
	}
	if !strings.Contains(model.renderTranscript(), "final text") {
		t.Fatalf("rendered transcript = %q, want updated content", model.renderTranscript())
	}
}

func TestTUIMessageUpdateAppendsWhenKindChanges(t *testing.T) {
	model := newTUITestModel()
	model.handlePicoMessage(picoMessage{
		Type: typeMessageCreate,
		Payload: map[string]any{
			"message_id":   "shared-id",
			payloadKeyKind: messageKindToolCalls,
			payloadKeyTools: []VisibleToolCall{
				{
					Function: &VisibleToolCallFunction{
						Name:      "web_search",
						Arguments: "{\"q\":\"feature store\"}",
					},
				},
			},
		},
	})
	model.handlePicoMessage(picoMessage{
		Type: typeMessageUpdate,
		Payload: map[string]any{
			"message_id":      "shared-id",
			payloadKeyContent: "final answer",
		},
	})

	if len(model.entries) != 2 {
		t.Fatalf("entries = %d, want 2 separate transcript entries", len(model.entries))
	}
	rendered := model.renderTranscript()
	if !strings.Contains(rendered, "web_search") {
		t.Fatalf("rendered transcript = %q, want tool call preserved", rendered)
	}
	if !strings.Contains(rendered, "final answer") {
		t.Fatalf("rendered transcript = %q, want final answer appended", rendered)
	}
}

func TestTUIMessageUpdateStaysInsideCurrentTurn(t *testing.T) {
	model := newTUITestModel()
	model.addUserMessage("first question")
	model.handlePicoMessage(picoMessage{
		Type: typeMessageCreate,
		Payload: map[string]any{
			"message_id":      "shared-id",
			payloadKeyContent: "first draft",
		},
	})
	model.handlePicoMessage(picoMessage{
		Type: typeMessageUpdate,
		Payload: map[string]any{
			"message_id":      "shared-id",
			payloadKeyContent: "first final",
		},
	})
	model.addUserMessage("second question")
	model.handlePicoMessage(picoMessage{
		Type: typeMessageUpdate,
		Payload: map[string]any{
			"message_id":      "shared-id",
			payloadKeyContent: "second final",
		},
	})

	if len(model.entries) != 4 {
		t.Fatalf("entries = %d, want 4 preserved transcript entries", len(model.entries))
	}

	rendered := model.renderTranscript()
	if !strings.Contains(rendered, "first final") {
		t.Fatalf("rendered transcript = %q, want first turn preserved", rendered)
	}
	if !strings.Contains(rendered, "second final") {
		t.Fatalf("rendered transcript = %q, want second turn appended", rendered)
	}
	if strings.Count(rendered, "first final") != 1 {
		t.Fatalf("rendered transcript = %q, want first turn to appear once", rendered)
	}
}

func TestTUIMouseSelectionCopiesTranscriptText(t *testing.T) {
	model := newTUITestModel()
	model.viewport.Width = 20
	model.viewport.Height = 5
	model.viewport.SetContent("alpha\nbeta\ngamma")

	var copied string
	prevClipboard := writeClipboard
	writeClipboard = func(text string) error {
		copied = text
		return nil
	}
	defer func() {
		writeClipboard = prevClipboard
	}()

	updated, _ := model.Update(tea.MouseMsg{
		X:      1,
		Y:      0,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	next := updated.(tuiModel)

	updated, _ = next.Update(tea.MouseMsg{
		X:      2,
		Y:      1,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionMotion,
	})
	next = updated.(tuiModel)

	updated, _ = next.Update(tea.MouseMsg{
		X:      2,
		Y:      1,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionRelease,
	})
	next = updated.(tuiModel)

	if copied != "lpha\nbet" {
		t.Fatalf("copied = %q, want selected transcript text", copied)
	}
	if !next.hasSelectionRange() {
		t.Fatal("selection cleared after copy, want visible selection to remain")
	}
	if !strings.Contains(next.renderViewportView(), "lpha") {
		t.Fatalf("rendered viewport = %q, want selected text to remain visible", next.renderViewportView())
	}
}

func TestTUIRightClickRecopiesActiveSelection(t *testing.T) {
	model := newTUITestModel()
	model.viewport.Width = 20
	model.viewport.Height = 5
	model.viewport.SetContent("alpha\nbeta\ngamma")
	model.selection = transcriptSelection{
		active: true,
		start:  selectionPoint{X: 1, Y: 0},
		end:    selectionPoint{X: 2, Y: 1},
	}

	var copied string
	prevClipboard := writeClipboard
	writeClipboard = func(text string) error {
		copied = text
		return nil
	}
	defer func() {
		writeClipboard = prevClipboard
	}()

	updated, _ := model.Update(tea.MouseMsg{
		X:      2,
		Y:      0,
		Button: tea.MouseButtonRight,
		Action: tea.MouseActionPress,
	})
	next := updated.(tuiModel)

	if copied != "lpha\nbet" {
		t.Fatalf("copied = %q, want selected transcript text on right click", copied)
	}
	if !next.hasSelectionRange() {
		t.Fatal("selection cleared after right click, want it preserved")
	}
}

func TestTUIWheelScrollsTranscript(t *testing.T) {
	model := newTUITestModel()
	for i := 0; i < 40; i++ {
		model.addRaw(strings.Repeat("message ", 6))
	}
	model.refreshViewport(false)
	model.viewport.SetYOffset(0)

	updated, _ := model.Update(tea.MouseMsg{
		Button: tea.MouseButtonWheelDown,
		Action: tea.MouseActionPress,
	})

	next := updated.(tuiModel)
	if next.viewport.YOffset <= model.viewport.YOffset {
		t.Fatalf("viewport offset = %d, want it to increase after wheel down", next.viewport.YOffset)
	}
}

func TestTUIPgUpScrollsTranscript(t *testing.T) {
	model := newTUITestModel()
	for i := 0; i < 40; i++ {
		model.addRaw(strings.Repeat("message ", 6))
	}
	model.refreshViewport(true)
	model.viewport.GotoBottom()

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyPgUp})

	next := updated.(tuiModel)
	if next.viewport.YOffset >= model.viewport.YOffset {
		t.Fatalf("viewport offset = %d, want it to decrease after pgup", next.viewport.YOffset)
	}
}

func TestTUIArrowKeysNavigateHistoryWhenDraftIsSingleLine(t *testing.T) {
	model := newTUITestModel()
	model.history = &persistentHistory{maxEntries: 10}
	model.history.Add("first")
	model.history.Add("second")
	model.input.SetValue("draft")

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyUp})
	next := updated.(tuiModel)
	if next.input.Value() != "second" {
		t.Fatalf("input after first up = %q, want newest history entry", next.input.Value())
	}

	updated, _ = next.Update(tea.KeyMsg{Type: tea.KeyUp})
	next = updated.(tuiModel)
	if next.input.Value() != "first" {
		t.Fatalf("input after second up = %q, want older history entry", next.input.Value())
	}

	updated, _ = next.Update(tea.KeyMsg{Type: tea.KeyDown})
	next = updated.(tuiModel)
	if next.input.Value() != "second" {
		t.Fatalf("input after first down = %q, want newer history entry", next.input.Value())
	}

	updated, _ = next.Update(tea.KeyMsg{Type: tea.KeyDown})
	next = updated.(tuiModel)
	if next.input.Value() != "draft" {
		t.Fatalf("input after second down = %q, want original draft restored", next.input.Value())
	}
	if next.historyIndex != -1 {
		t.Fatalf("historyIndex = %d, want reset after restoring draft", next.historyIndex)
	}
}

func TestTUITranscriptWrapsLongLines(t *testing.T) {
	model := newTUITestModel()
	model.resize(32, 20)
	model.addRaw(strings.Repeat("longword ", 8))

	rendered := model.renderTranscript()
	for _, line := range strings.Split(rendered, "\n") {
		if lipgloss.Width(stripControlWhitespace(line)) > 32 {
			t.Fatalf("line too wide: %q", line)
		}
	}
}

func TestTUITranscriptWrapsRepresentativeMarkdown(t *testing.T) {
	model := newTUITestModel()
	model.resize(64, 20)

	content := strings.Join([]string{
		"[This repository](https://github.mpi-internal.com/SCM-Italy/data-team_dp-luna-lovegood) contains the job that creates the pulse consumer aligned data product",
		"",
		"## Datasets list",
		"",
		"| Feature Store        | Azienda | Stack                                    |",
		"|----------------------|---------|------------------------------------------|",
		"| Michelangelo Palette | Uber    | Hive (offline), Redis/Cassandra (online) |",
		"| Zipline -> Chronon    | Airbnb  | Hive/Spark                               |",
		"| FBLearner            | Meta    | Interno                                  |",
	}, "\n")
	model.addRaw(renderTranscriptEntry("●", "Assistant", "", renderAssistantMarkdown(content), colorAssistant))

	rendered := model.renderTranscript()
	for _, line := range strings.Split(rendered, "\n") {
		if lipgloss.Width(stripControlWhitespace(line)) > 64 {
			t.Fatalf("line too wide: %q", line)
		}
	}
	if !strings.Contains(rendered, "Michelangelo Palette") {
		t.Fatalf("rendered transcript = %q, want markdown table preserved", rendered)
	}
}

func TestTUITranscriptFollowsBottomWhenEnabled(t *testing.T) {
	model := newTUITestModel()
	model.resize(32, 12)
	model.followTranscript = true

	for i := 0; i < 20; i++ {
		model.addRaw(strings.Repeat("entry ", 6))
	}
	model.refreshViewport(false)

	if !model.viewport.AtBottom() {
		t.Fatalf("viewport YOffset = %d, want bottom follow", model.viewport.YOffset)
	}
}

func TestTUITranscriptKeepsPositionWhenFollowDisabled(t *testing.T) {
	model := newTUITestModel()
	model.resize(32, 12)
	model.followTranscript = false

	for i := 0; i < 20; i++ {
		model.addRaw(strings.Repeat("entry ", 6))
	}
	model.viewport.SetYOffset(0)
	model.refreshViewport(false)

	if model.viewport.YOffset != 0 {
		t.Fatalf("viewport YOffset = %d, want preserved position", model.viewport.YOffset)
	}
}

func TestTUIResizeClampsViewportOffsetWithoutForcingBottom(t *testing.T) {
	model := newTUITestModel()
	model.resize(32, 12)
	model.followTranscript = false

	for i := 0; i < 40; i++ {
		model.addRaw(strings.Repeat("entry ", 6))
	}
	model.refreshViewport(false)
	maxYOffset := model.viewport.TotalLineCount() - model.viewport.Height + model.viewport.Style.GetVerticalFrameSize()
	if maxYOffset < 0 {
		maxYOffset = 0
	}
	model.viewport.SetYOffset(maxYOffset)
	before := model.viewport.YOffset

	model.resize(32, 24)

	maxYOffset = model.viewport.TotalLineCount() - model.viewport.Height + model.viewport.Style.GetVerticalFrameSize()
	if maxYOffset < 0 {
		maxYOffset = 0
	}

	if model.viewport.YOffset > maxYOffset {
		t.Fatalf("viewport YOffset = %d, want clamped to max %d", model.viewport.YOffset, maxYOffset)
	}
	if model.viewport.YOffset == before && maxYOffset < before {
		t.Fatalf("viewport YOffset = %d, want it to move when the viewport height grows", model.viewport.YOffset)
	}
	if model.viewport.VisibleLineCount() == 0 {
		t.Fatal("viewport VisibleLineCount() = 0, want transcript still readable after resize")
	}
}

func TestTUIRefreshViewportPreservesScrollPositionWhenContentChanges(t *testing.T) {
	model := newTUITestModel()
	model.resize(32, 12)
	model.followTranscript = false

	for i := 0; i < 30; i++ {
		model.addRaw(strings.Repeat("entry ", 6))
	}
	model.refreshViewport(false)
	model.viewport.SetYOffset(8)
	before := model.viewport.YOffset

	model.entries = model.entries[:10]
	model.refreshViewport(false)

	if model.viewport.YOffset != before {
		t.Fatalf("viewport YOffset = %d, want preserved scroll position %d", model.viewport.YOffset, before)
	}
	if model.viewport.AtBottom() {
		t.Fatal("viewport AtBottom() = true, want preserved non-bottom scroll position")
	}
}

func TestNewTUIModelDisablesMouseWheelByDefault(t *testing.T) {
	model := newTUIModel("ws://example.test/pico/ws", cliOptions{
		sessionID: "session-1",
		showTools: true,
	}, &safeConn{})

	if model.viewport.MouseWheelEnabled {
		t.Fatal("MouseWheelEnabled = true, want false when mouse mode is disabled")
	}
}

func TestNewTUIModelEnablesMouseWheelWhenRequested(t *testing.T) {
	model := newTUIModel("ws://example.test/pico/ws", cliOptions{
		sessionID: "session-1",
		showTools: true,
		mouseMode: true,
	}, &safeConn{})

	if !model.viewport.MouseWheelEnabled {
		t.Fatal("MouseWheelEnabled = false, want true when mouse mode is enabled")
	}
}

func newTUITestModel() tuiModel {
	model := newTUIModel("ws://example.test/pico/ws", cliOptions{
		sessionID: "session-1",
		showTools: true,
		mouseMode: true,
	}, &safeConn{})
	model.history = nil
	_ = model.input.Focus()
	model.resize(100, 30)
	return model
}
