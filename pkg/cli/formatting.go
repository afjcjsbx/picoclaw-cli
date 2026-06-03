package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"
)

var ansiEscapePattern = regexp.MustCompile("\x1b\\[[0-9;]*m")

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
