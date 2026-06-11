package cli

import (
	"fmt"
	"strings"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

var writeClipboard = clipboard.WriteAll

type selectionPoint struct {
	X int
	Y int
}

type transcriptSelection struct {
	active   bool
	dragging bool
	start    selectionPoint
	end      selectionPoint
}

func (m tuiModel) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.viewport.MouseWheelEnabled && msg.Action == tea.MouseActionPress && tea.MouseEvent(msg).IsWheel() {
		m.clearSelection()
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		m.followTranscript = m.viewport.AtBottom()
		return m, cmd
	}

	if m.handleTranscriptSelectionMouse(msg) {
		return m, nil
	}

	if m.selection.active {
		m.clearSelection()
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.resize(m.width, m.height)
	return m, cmd
}

func (m *tuiModel) handleTranscriptSelectionMouse(msg tea.MouseMsg) bool {
	switch msg.Action {
	case tea.MouseActionPress:
		if msg.Button == tea.MouseButtonRight && m.mouseInTranscript(msg) && m.hasSelectionRange() {
			if err := m.copySelectionToClipboard(); err != nil {
				m.addError(fmt.Sprintf("copy failed: %v", err))
				m.clearSelection()
			}
			return true
		}
		if msg.Button != tea.MouseButtonLeft || !m.mouseInTranscript(msg) {
			return false
		}
		point := m.clampSelectionPoint(msg.X, msg.Y)
		m.selection = transcriptSelection{
			active:   true,
			dragging: true,
			start:    point,
			end:      point,
		}
		m.followTranscript = false
		return true
	case tea.MouseActionMotion:
		if !m.selection.dragging {
			return false
		}
		m.selection.end = m.clampSelectionPoint(msg.X, msg.Y)
		return true
	case tea.MouseActionRelease:
		if !m.selection.dragging {
			return false
		}
		m.selection.dragging = false
		m.selection.end = m.clampSelectionPoint(msg.X, msg.Y)
		if !m.hasSelectionRange() {
			m.clearSelection()
			return true
		}
		if err := m.copySelectionToClipboard(); err != nil {
			m.addError(fmt.Sprintf("copy failed: %v", err))
			m.clearSelection()
		}
		return true
	default:
		return false
	}
}

func (m *tuiModel) clearSelection() {
	m.selection = transcriptSelection{}
}

func (m tuiModel) hasSelectionRange() bool {
	_, _, ok := m.normalizedSelection()
	return ok
}

func (m tuiModel) normalizedSelection() (selectionPoint, selectionPoint, bool) {
	if !m.selection.active {
		return selectionPoint{}, selectionPoint{}, false
	}

	start := m.selection.start
	end := m.selection.end
	if end.Y < start.Y || (end.Y == start.Y && end.X < start.X) {
		start, end = end, start
	}
	if start == end {
		return selectionPoint{}, selectionPoint{}, false
	}

	return start, end, true
}

func (m tuiModel) clampSelectionPoint(x, y int) selectionPoint {
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	if m.viewport.Width > 0 && x > m.viewport.Width {
		x = m.viewport.Width
	}
	if m.viewport.Height > 0 && y >= m.viewport.Height {
		y = m.viewport.Height - 1
	}
	return selectionPoint{X: x, Y: y}
}

func (m tuiModel) mouseInTranscript(msg tea.MouseMsg) bool {
	return msg.Y >= 0 && msg.Y < m.viewport.Height
}

func (m tuiModel) renderViewportView() string {
	view := m.viewport.View()
	if !m.hasSelectionRange() {
		return view
	}
	return m.renderSelectedViewportView(view)
}

func (m tuiModel) renderSelectedViewportView(view string) string {
	start, end, ok := m.normalizedSelection()
	if !ok {
		return view
	}

	lines := strings.Split(view, "\n")
	for i, line := range lines {
		left, right, ok := selectionColumnsForLine(i, ansi.StringWidth(line), start, end)
		if !ok {
			continue
		}

		prefix := ansi.Cut(line, 0, left)
		middle := stripControlWhitespace(ansi.Cut(line, left, right))
		suffix := ansi.Cut(line, right, ansi.StringWidth(line))
		if middle == "" {
			continue
		}

		lines[i] = prefix + styleSelection.Render(middle) + suffix
	}

	return strings.Join(lines, "\n")
}

func (m tuiModel) copySelectionToClipboard() error {
	text := m.selectedTextFromViewport()
	if text == "" {
		return nil
	}
	return writeClipboard(text)
}

func (m tuiModel) selectedTextFromViewport() string {
	start, end, ok := m.normalizedSelection()
	if !ok {
		return ""
	}

	lines := strings.Split(m.viewport.View(), "\n")
	selected := make([]string, 0, end.Y-start.Y+1)
	for i, line := range lines {
		plain := stripControlWhitespace(line)
		left, right, ok := selectionColumnsForLine(i, ansi.StringWidth(plain), start, end)
		if !ok {
			continue
		}
		selected = append(selected, strings.TrimRight(ansi.Cut(plain, left, right), " "))
	}

	return strings.Join(selected, "\n")
}

func selectionColumnsForLine(lineIndex, lineWidth int, start, end selectionPoint) (int, int, bool) {
	if lineIndex < start.Y || lineIndex > end.Y {
		return 0, 0, false
	}

	left := 0
	if lineIndex == start.Y {
		left = start.X
	}

	right := lineWidth
	if lineIndex == end.Y {
		right = end.X + 1
	}

	if left > lineWidth {
		left = lineWidth
	}
	if right > lineWidth {
		right = lineWidth
	}
	if right <= left {
		return 0, 0, false
	}

	return left, right, true
}
