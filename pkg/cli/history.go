package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	defaultHistoryMaxEntries = 100
	defaultHistoryFileName   = "history.json"
)

type persistentHistory struct {
	mu         sync.Mutex
	path       string
	maxEntries int
	entries    []string
}

func newPersistentHistory(path string, maxEntries int) (*persistentHistory, error) {
	if maxEntries <= 0 {
		maxEntries = defaultHistoryMaxEntries
	}

	history := &persistentHistory{
		path:       path,
		maxEntries: maxEntries,
	}
	if err := history.load(); err != nil {
		return nil, err
	}
	return history, nil
}

func defaultHistoryPath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config dir: %w", err)
	}
	return filepath.Join(configDir, "picoclaw-cli", defaultHistoryFileName), nil
}

func (h *persistentHistory) Add(entry string) {
	if strings.TrimSpace(entry) == "" {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	h.entries = append(h.entries, entry)
	if len(h.entries) > h.maxEntries {
		h.entries = append([]string(nil), h.entries[len(h.entries)-h.maxEntries:]...)
	}
	_ = h.saveLocked()
}

func (h *persistentHistory) Len() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.entries)
}

func (h *persistentHistory) At(idx int) string {
	h.mu.Lock()
	defer h.mu.Unlock()

	if idx < 0 || idx >= len(h.entries) {
		panic(fmt.Sprintf("cli: history index [%d] out of range [0,%d)", idx, len(h.entries)))
	}
	return h.entries[len(h.entries)-1-idx]
}

func (h *persistentHistory) load() error {
	data, err := os.ReadFile(h.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read history: %w", err)
	}

	var entries []string
	if err := json.Unmarshal(data, &entries); err != nil {
		return fmt.Errorf("decode history: %w", err)
	}

	for _, entry := range entries {
		if strings.TrimSpace(entry) != "" {
			h.entries = append(h.entries, entry)
		}
	}
	if len(h.entries) > h.maxEntries {
		h.entries = append([]string(nil), h.entries[len(h.entries)-h.maxEntries:]...)
	}
	return nil
}

func (h *persistentHistory) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(h.path), 0o700); err != nil {
		return fmt.Errorf("create history dir: %w", err)
	}

	data, err := json.MarshalIndent(h.entries, "", "  ")
	if err != nil {
		return fmt.Errorf("encode history: %w", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(h.path, data, 0o600); err != nil {
		return fmt.Errorf("write history: %w", err)
	}
	return nil
}
