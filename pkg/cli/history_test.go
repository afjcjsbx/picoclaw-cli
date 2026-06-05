package cli

import (
	"path/filepath"
	"testing"
)

func TestPersistentHistoryKeepsMostRecentEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")

	history, err := newPersistentHistory(path, 3)
	if err != nil {
		t.Fatalf("newPersistentHistory() error = %v", err)
	}

	history.Add("first")
	history.Add("  second  ")
	history.Add("")
	history.Add("third")
	history.Add("fourth")

	if got := history.Len(); got != 3 {
		t.Fatalf("history.Len() = %d, want 3", got)
	}

	if got := history.At(0); got != "fourth" {
		t.Fatalf("history.At(0) = %q, want fourth", got)
	}
	if got := history.At(1); got != "third" {
		t.Fatalf("history.At(1) = %q, want third", got)
	}
	if got := history.At(2); got != "  second  " {
		t.Fatalf("history.At(2) = %q, want %q", got, "  second  ")
	}
}

func TestPersistentHistoryReloadsFromDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")

	history, err := newPersistentHistory(path, 2)
	if err != nil {
		t.Fatalf("newPersistentHistory() error = %v", err)
	}
	history.Add("alpha")
	history.Add("beta")
	history.Add("gamma")

	reloaded, err := newPersistentHistory(path, 2)
	if err != nil {
		t.Fatalf("newPersistentHistory() reload error = %v", err)
	}

	if got := reloaded.Len(); got != 2 {
		t.Fatalf("reloaded.Len() = %d, want 2", got)
	}
	if got := reloaded.At(0); got != "gamma" {
		t.Fatalf("reloaded.At(0) = %q, want gamma", got)
	}
	if got := reloaded.At(1); got != "beta" {
		t.Fatalf("reloaded.At(1) = %q, want beta", got)
	}
}

func TestPersistentHistoryReloadsStructuredFormattingVerbatim(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")

	history, err := newPersistentHistory(path, 3)
	if err != nil {
		t.Fatalf("newPersistentHistory() error = %v", err)
	}

	history.Add("↩    indented⇥value  ")

	reloaded, err := newPersistentHistory(path, 3)
	if err != nil {
		t.Fatalf("newPersistentHistory() reload error = %v", err)
	}

	if got := reloaded.At(0); got != "↩    indented⇥value  " {
		t.Fatalf("reloaded.At(0) = %q, want %q", got, "↩    indented⇥value  ")
	}
}
