package cli

import (
	"strings"
	"testing"
)

func TestStructuredPasteTransformerUsesCompactPlaceholderAndRestoresOriginalText(t *testing.T) {
	var transformer structuredPasteTransformer

	transformer.Push([]byte("preface "))
	transformer.Push([]byte("\x1b[20"))
	transformer.Push([]byte("0~line 1\r\n\tline 2\x1b[20"))
	transformer.Push([]byte("1~"))

	got := drainStructuredPasteOutput(t, &transformer)
	if got != "preface "+structuredPasteToken {
		t.Fatalf("drainStructuredPasteOutput() = %q, want %q", got, "preface "+structuredPasteToken)
	}
	if !transformer.HasActiveDraft() {
		t.Fatal("HasActiveDraft() = false, want true")
	}

	transformer.Push([]byte("\r"))
	if got := drainStructuredPasteOutput(t, &transformer); got != "\r" {
		t.Fatalf("submit output = %q, want carriage return", got)
	}

	rewritten, ok := transformer.ConsumeSubmittedLine("preface " + structuredPasteToken)
	if !ok {
		t.Fatal("ConsumeSubmittedLine() ok = false, want true")
	}
	if rewritten != "preface line 1\n\tline 2" {
		t.Fatalf("ConsumeSubmittedLine() = %q, want %q", rewritten, "preface line 1\n\tline 2")
	}
}

func TestStructuredPasteTransformerTypingKeepsActiveDraft(t *testing.T) {
	var transformer structuredPasteTransformer

	transformer.Push([]byte("\x1b[200~alpha\nbeta\x1b[201~"))
	if got := drainStructuredPasteOutput(t, &transformer); got != structuredPasteToken {
		t.Fatalf("drainStructuredPasteOutput() = %q, want %q", got, structuredPasteToken)
	}

	transformer.Push([]byte("!"))
	if got := drainStructuredPasteOutput(t, &transformer); got != "!" {
		t.Fatalf("typed suffix should pass through the editor, got %q", got)
	}
	if !transformer.HasActiveDraft() {
		t.Fatal("HasActiveDraft() = false, want true after typing")
	}

	transformer.Push([]byte("\r"))
	if got := drainStructuredPasteOutput(t, &transformer); got != "\r" {
		t.Fatalf("submit output = %q, want carriage return", got)
	}

	rewritten, ok := transformer.ConsumeSubmittedLine(structuredPasteToken + "!")
	if !ok {
		t.Fatal("ConsumeSubmittedLine() ok = false, want true")
	}
	if rewritten != "alpha\nbeta!" {
		t.Fatalf("ConsumeSubmittedLine() = %q, want %q", rewritten, "alpha\nbeta!")
	}
}

func TestStructuredPasteTransformerBackspaceClearsOnlyActiveDraft(t *testing.T) {
	var transformer structuredPasteTransformer

	transformer.Push([]byte("prefix "))
	transformer.Push([]byte("\x1b[200~alpha\nbeta\x1b[201~"))
	if got := drainStructuredPasteOutput(t, &transformer); got != "prefix "+structuredPasteToken {
		t.Fatalf("drainStructuredPasteOutput() = %q, want %q", got, "prefix "+structuredPasteToken)
	}
	if !transformer.HasActiveDraft() {
		t.Fatal("HasActiveDraft() = false, want true")
	}

	transformer.Push([]byte{127})
	if got := drainStructuredPasteOutput(t, &transformer); got != "\x7f" {
		t.Fatalf("Backspace output = %q, want %q", got, "\x7f")
	}
	if transformer.HasActiveDraft() {
		t.Fatal("HasActiveDraft() = true, want false after Backspace")
	}
}

func TestStructuredPasteTransformerCtrlUClearsOnlyActiveDraft(t *testing.T) {
	var transformer structuredPasteTransformer

	transformer.Push([]byte("prefix "))
	transformer.Push([]byte("\x1b[200~alpha\nbeta\x1b[201~"))
	if got := drainStructuredPasteOutput(t, &transformer); got != "prefix "+structuredPasteToken {
		t.Fatalf("drainStructuredPasteOutput() = %q, want %q", got, "prefix "+structuredPasteToken)
	}
	if !transformer.HasActiveDraft() {
		t.Fatal("HasActiveDraft() = false, want true")
	}

	transformer.Push([]byte{21})
	if got := drainStructuredPasteOutput(t, &transformer); got != "\x15" {
		t.Fatalf("Ctrl+U output = %q, want %q", got, "\x15")
	}
	if transformer.HasActiveDraft() {
		t.Fatal("HasActiveDraft() = true, want false after Ctrl+U")
	}
}

func TestStructuredPasteTransformerLeavesSingleLinePasteUntouched(t *testing.T) {
	var transformer structuredPasteTransformer

	transformer.Push([]byte("before "))
	transformer.Push(append(append([]byte(nil), bracketedPasteStart...), []byte("single line")...))
	transformer.Push(append(append([]byte(nil), bracketedPasteEnd...), []byte(" after")...))

	got := drainStructuredPasteOutput(t, &transformer)
	if got != "before single line after" {
		t.Fatalf("drainStructuredPasteOutput() = %q, want %q", got, "before single line after")
	}
	if transformer.HasActiveDraft() {
		t.Fatal("HasActiveDraft() = true, want false")
	}
}

func TestRenderStructuredDraftPreviewShowsCompactSummary(t *testing.T) {
	preview := renderStructuredDraftPreview("first line\n\n\tindented")

	if !strings.Contains(preview, "Pasted Block") {
		t.Fatalf("preview = %q, want compact summary", preview)
	}
	if !strings.Contains(preview, "3 line(s)") {
		t.Fatalf("preview = %q, want line count", preview)
	}
	if !strings.Contains(preview, "Backspace/Ctrl+U discard") {
		t.Fatalf("preview = %q, want discard hint", preview)
	}
}

func drainStructuredPasteOutput(t *testing.T, transformer *structuredPasteTransformer) string {
	t.Helper()

	var builder strings.Builder
	buf := make([]byte, 3)
	for {
		n := transformer.Pop(buf)
		if n == 0 {
			return builder.String()
		}
		builder.Write(buf[:n])
	}
}
