package cli

import (
	"strings"
	"testing"
)

func TestBuildWSURLAddsSessionIDAndTokenQuery(t *testing.T) {
	got, err := buildWSURL("ws://example.com/pico/ws?existing=1", "session-123", "secret", true)
	if err != nil {
		t.Fatalf("buildWSURL() error = %v", err)
	}

	if !strings.Contains(got, "existing=1") {
		t.Fatalf("buildWSURL() = %q, want existing query preserved", got)
	}
	if !strings.Contains(got, "session_id=session-123") {
		t.Fatalf("buildWSURL() = %q, want session_id added", got)
	}
	if !strings.Contains(got, "token=secret") {
		t.Fatalf("buildWSURL() = %q, want token query added", got)
	}
}

func TestParseToggle(t *testing.T) {
	tests := []struct {
		input string
		want  bool
		ok    bool
	}{
		{input: "on", want: true, ok: true},
		{input: "YES", want: true, ok: true},
		{input: "0", want: false, ok: true},
		{input: "off", want: false, ok: true},
		{input: "maybe", want: false, ok: false},
	}

	for _, tt := range tests {
		got, ok := parseToggle(tt.input)
		if got != tt.want || ok != tt.ok {
			t.Fatalf("parseToggle(%q) = (%v, %v), want (%v, %v)", tt.input, got, ok, tt.want, tt.ok)
		}
	}
}

func TestSplitToolFeedbackBody(t *testing.T) {
	body := "Fetching page results.\n```json\n{\"q\":\"tailscale\"}\n```\nDone."

	args, explanation := splitToolFeedbackBody(body)

	if args != "{\"q\":\"tailscale\"}" {
		t.Fatalf("splitToolFeedbackBody() args = %q, want compact json", args)
	}
	if explanation != "Fetching page results.\n\nDone." {
		t.Fatalf("splitToolFeedbackBody() explanation = %q", explanation)
	}
}

func TestParseLegacyToolFeedback(t *testing.T) {
	content := "🔧 `web_search`\nLooking things up.\n```json\n{\"q\":\"picoclaw\"}\n```"

	calls, ok := parseLegacyToolFeedback(content)
	if !ok {
		t.Fatal("parseLegacyToolFeedback() ok = false, want true")
	}
	if len(calls) != 1 {
		t.Fatalf("parseLegacyToolFeedback() len = %d, want 1", len(calls))
	}
	if calls[0].Function == nil || calls[0].Function.Name != "web_search" {
		t.Fatalf("parseLegacyToolFeedback() function = %#v, want web_search", calls[0].Function)
	}
	if calls[0].Function.Arguments != "{\"q\":\"picoclaw\"}" {
		t.Fatalf("parseLegacyToolFeedback() args = %q", calls[0].Function.Arguments)
	}
	if calls[0].ExtraContent == nil || !strings.Contains(calls[0].ExtraContent.ToolFeedbackExplanation, "Looking things up.") {
		t.Fatalf("parseLegacyToolFeedback() explanation = %#v", calls[0].ExtraContent)
	}
}

func TestRenderToolEntriesCompactOmitsPayloadDetails(t *testing.T) {
	view := messageView{
		modelName: "deepseek/deepseek-v4-flash",
		toolCalls: []VisibleToolCall{
			{
				Function: &VisibleToolCallFunction{
					Name:      "web_fetch",
					Arguments: "{\"url\":\"https://example.com\"}",
				},
				ExtraContent: &VisibleToolCallExtraContent{
					ToolFeedbackExplanation: "Fetching remote page.",
				},
			},
			{
				Function: &VisibleToolCallFunction{
					Name:      "web_fetch",
					Arguments: "{\"url\":\"https://example.org\"}",
				},
			},
		},
	}

	entries := renderToolEntries(view, true)
	if len(entries) != 2 {
		t.Fatalf("renderToolEntries(..., true) len = %d, want 2", len(entries))
	}

	rendered := stripControlWhitespace(strings.Join(entries, "\n"))
	if !strings.Contains(rendered, "⏺ web_fetch  deepseek/deepseek-v4-flash · call 1/2") {
		t.Fatalf("compact render missing first header: %q", rendered)
	}
	if strings.Contains(rendered, "Fetching remote page.") || strings.Contains(rendered, "\"url\"") || strings.Contains(rendered, "args") {
		t.Fatalf("compact render should omit payload details, got %q", rendered)
	}
}

func TestRenderToolEntriesFullIncludesPayloadDetails(t *testing.T) {
	view := messageView{
		modelName: "deepseek/deepseek-v4-flash",
		toolCalls: []VisibleToolCall{
			{
				Function: &VisibleToolCallFunction{
					Name:      "web_search",
					Arguments: "{\"q\":\"picoclaw-cli\"}",
				},
				ExtraContent: &VisibleToolCallExtraContent{
					ToolFeedbackExplanation: "Searching the web.",
				},
			},
		},
	}

	entries := renderToolEntries(view, false)
	if len(entries) != 1 {
		t.Fatalf("renderToolEntries(..., false) len = %d, want 1", len(entries))
	}

	rendered := stripControlWhitespace(entries[0])
	if !strings.Contains(rendered, "Searching the web.") {
		t.Fatalf("full render should include explanation, got %q", rendered)
	}
	if !strings.Contains(rendered, "\"q\": \"picoclaw-cli\"") {
		t.Fatalf("full render should include pretty json args, got %q", rendered)
	}
}
