package cli

import (
	"io"
	"testing"
	"time"
)

type spyTransientLine struct {
	shown          []string
	cleared        int
	spinnerShown   []string
	spinnerCleared int
}

func (s *spyTransientLine) ShowTransient(rendered string) {
	s.shown = append(s.shown, rendered)
}

func (s *spyTransientLine) ClearTransient() {
	s.cleared++
}

func (s *spyTransientLine) ShowSpinner(rendered string) {
	s.spinnerShown = append(s.spinnerShown, rendered)
}

func (s *spyTransientLine) ClearSpinner() {
	s.spinnerCleared++
}

func TestRendererManagedTypingUsesSpinnerOverlay(t *testing.T) {
	overlay := &spyTransientLine{}
	r := newRenderer("ws://example.com", "session-1", false, false, false, io.Discard, true, overlay)

	r.setTyping(true)
	time.Sleep(120 * time.Millisecond)
	r.setTyping(false)

	if len(overlay.spinnerShown) == 0 {
		t.Fatal("ShowSpinner() was not called")
	}
	if overlay.spinnerShown[0] == "" {
		t.Fatal("ShowSpinner() rendered empty label")
	}
	if len(overlay.shown) != 0 {
		t.Fatalf("ShowTransient() calls = %d, want 0", len(overlay.shown))
	}
}

func TestRendererManagedTypingClearsSpinnerOnStop(t *testing.T) {
	overlay := &spyTransientLine{}
	r := newRenderer("ws://example.com", "session-1", false, false, false, io.Discard, true, overlay)

	r.setTyping(true)
	r.setTyping(false)

	if overlay.spinnerCleared != 1 {
		t.Fatalf("ClearSpinner() calls = %d, want 1", overlay.spinnerCleared)
	}
}
