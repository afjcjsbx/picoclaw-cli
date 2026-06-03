package cli

import (
	"strings"
	"testing"
)

func TestCompleteLocalCommand(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		pos     int
		want    string
		wantPos int
		ok      bool
	}{
		{
			name:    "completes unique command",
			line:    "/sta",
			pos:     4,
			want:    "/status ",
			wantPos: 8,
			ok:      true,
		},
		{
			name: "leaves ambiguous prefix unchanged",
			line: "/s",
			pos:  2,
			ok:   false,
		},
		{
			name:    "adds trailing space for exact command",
			line:    "/help",
			pos:     5,
			want:    "/help ",
			wantPos: 6,
			ok:      true,
		},
		{
			name:    "completes toggle argument",
			line:    "/thoughts of",
			pos:     len("/thoughts of"),
			want:    "/thoughts off ",
			wantPos: len("/thoughts off "),
			ok:      true,
		},
		{
			name:    "completes empty toggle argument",
			line:    "/tools ",
			pos:     len("/tools "),
			want:    "/tools o",
			wantPos: len("/tools o"),
			ok:      true,
		},
		{
			name: "ignores non command lines",
			line: "hello",
			pos:  5,
			ok:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotPos, ok := completeLocalCommand(tt.line, tt.pos)
			if ok != tt.ok {
				t.Fatalf("completeLocalCommand() ok = %v, want %v", ok, tt.ok)
			}
			if !ok {
				return
			}
			if got != tt.want || gotPos != tt.wantPos {
				t.Fatalf("completeLocalCommand() = (%q, %d), want (%q, %d)", got, gotPos, tt.want, tt.wantPos)
			}
		})
	}
}

func TestSlashCommandAutoCompleteIgnoresOtherKeys(t *testing.T) {
	got, gotPos, ok := slashCommandAutoComplete("/he", 3, 'x')
	if ok {
		t.Fatalf("slashCommandAutoComplete() ok = true, want false with result (%q, %d)", got, gotPos)
	}
}

func TestSuggestionTextForLine(t *testing.T) {
	tests := []struct {
		name string
		line string
		pos  int
		want string
	}{
		{
			name: "shows command matches",
			line: "/s",
			pos:  2,
			want: "Suggestions: /status, /session",
		},
		{
			name: "shows unique command suggestion",
			line: "/sta",
			pos:  4,
			want: "Suggestion: /status",
		},
		{
			name: "shows toggle values after space",
			line: "/tools ",
			pos:  len("/tools "),
			want: "Suggestions: on, off",
		},
		{
			name: "shows unique toggle suggestion",
			line: "/thoughts of",
			pos:  len("/thoughts of"),
			want: "Suggestion: off",
		},
		{
			name: "hides exact command without args",
			line: "/status",
			pos:  len("/status"),
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := suggestionTextForLine(tt.line, tt.pos)
			if got != tt.want {
				t.Fatalf("suggestionTextForLine() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestShouldClearSuggestionOnInput(t *testing.T) {
	if shouldClearSuggestionOnInput([]byte("abc")) {
		t.Fatal("shouldClearSuggestionOnInput() = true, want false for printable text")
	}
	if !shouldClearSuggestionOnInput([]byte{127}) {
		t.Fatal("shouldClearSuggestionOnInput() = false, want true for backspace")
	}
	if !shouldClearSuggestionOnInput([]byte{27, '[', 'A'}) {
		t.Fatal("shouldClearSuggestionOnInput() = false, want true for arrow escape")
	}
}

func TestSuggestionsForLineOrdering(t *testing.T) {
	got := suggestionsForLine("/s", 2)
	if strings.Join(got, ",") != "/status,/session" {
		t.Fatalf("suggestionsForLine() = %v", got)
	}
}
