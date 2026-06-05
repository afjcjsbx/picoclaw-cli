package cli

import "testing"

func TestSubmittedContentPreservesStructuredText(t *testing.T) {
	input := "  line 1\n\tline 2\n"

	got, ok := submittedContent(input)
	if !ok {
		t.Fatal("submittedContent() ok = false, want true")
	}
	if got != input {
		t.Fatalf("submittedContent() = %q, want %q", got, input)
	}
}

func TestSubmittedContentRejectsWhitespaceOnlyInput(t *testing.T) {
	got, ok := submittedContent(" \n\t ")
	if ok {
		t.Fatalf("submittedContent() ok = true, want false with content %q", got)
	}
	if got != "" {
		t.Fatalf("submittedContent() = %q, want empty string", got)
	}
}

func TestIsLocalCommandLineRejectsStructuredInput(t *testing.T) {
	if isLocalCommandLine("/help\nmore") {
		t.Fatal("isLocalCommandLine() = true, want false for multiline content")
	}
	if !isLocalCommandLine(" /help ") {
		t.Fatal("isLocalCommandLine() = false, want true for single-line content")
	}
}
