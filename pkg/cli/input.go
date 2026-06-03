package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"golang.org/x/term"
)

const promptText = "› "

type lineReader interface {
	ReadLine() (string, error)
	Close() error
	Output() io.Writer
	ManagesPrompt() bool
	AfterReadLine()
}

type transientLine interface {
	ShowTransient(rendered string)
	ClearTransient()
}

func newLineReader() (lineReader, error) {
	stdinFD := int(os.Stdin.Fd())
	stdoutFD := int(os.Stdout.Fd())
	if !term.IsTerminal(stdinFD) || !term.IsTerminal(stdoutFD) {
		return &basicLineReader{reader: bufio.NewReader(os.Stdin)}, nil
	}

	state, err := term.MakeRaw(stdinFD)
	if err != nil {
		return &basicLineReader{reader: bufio.NewReader(os.Stdin)}, nil
	}

	reader := &terminalLineReader{
		fd:    stdinFD,
		state: state,
	}
	reader.output = &terminalOutputWriter{owner: reader}

	editor := term.NewTerminal(&terminalReadWriter{
		stdin:  os.Stdin,
		stdout: os.Stdout,
		owner:  reader,
	}, promptText)
	reader.editor = editor
	editor.AutoCompleteCallback = reader.autoCompleteCallback
	if historyPath, err := defaultHistoryPath(); err == nil {
		if history, err := newPersistentHistory(historyPath, defaultHistoryMaxEntries); err == nil {
			editor.History = history
		} else {
			fmt.Fprintf(os.Stderr, "warn> history persistence disabled: %v\n", err)
		}
	} else {
		fmt.Fprintf(os.Stderr, "warn> history persistence disabled: %v\n", err)
	}
	if width, height, err := term.GetSize(stdoutFD); err == nil && width > 0 && height > 0 {
		_ = editor.SetSize(width, height)
	}

	return reader, nil
}

type basicLineReader struct {
	reader *bufio.Reader
}

func (r *basicLineReader) ReadLine() (string, error) {
	line, err := r.reader.ReadString('\n')
	return strings.TrimRight(line, "\r\n"), err
}

func (r *basicLineReader) Close() error {
	return nil
}

func (r *basicLineReader) Output() io.Writer {
	return os.Stdout
}

func (r *basicLineReader) ManagesPrompt() bool {
	return false
}

func (r *basicLineReader) AfterReadLine() {}

type terminalLineReader struct {
	editor *term.Terminal
	fd     int
	state  *term.State

	output *terminalOutputWriter

	mu               sync.Mutex
	transientVisible bool
}

func (r *terminalLineReader) ReadLine() (string, error) {
	return r.editor.ReadLine()
}

func (r *terminalLineReader) Close() error {
	r.clearSuggestion()
	return term.Restore(r.fd, r.state)
}

func (r *terminalLineReader) Output() io.Writer {
	return r.output
}

func (r *terminalLineReader) ManagesPrompt() bool {
	return true
}

func (r *terminalLineReader) AfterReadLine() {
	r.clearSuggestion()
}

func (r *terminalLineReader) autoCompleteCallback(line string, pos int, key rune) (string, int, bool) {
	if key == '\t' {
		completed, newPos, ok := slashCommandAutoComplete(line, pos, key)
		if ok {
			r.showSuggestionForLine(completed, newPos)
		} else {
			r.clearSuggestion()
		}
		return completed, newPos, ok
	}

	nextLine, nextPos, ok := applyKeyToLine(line, pos, key)
	if !ok {
		r.clearSuggestion()
		return "", 0, false
	}

	r.showSuggestionForLine(nextLine, nextPos)
	return "", 0, false
}

func (r *terminalLineReader) showSuggestionForLine(line string, pos int) {
	text := suggestionTextForLine(line, pos)
	if text == "" {
		r.clearSuggestion()
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.renderSuggestionLocked(text)
}

func (r *terminalLineReader) clearSuggestion() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.clearSuggestionLocked()
}

func (r *terminalLineReader) clearSuggestionLocked() {
	if !r.transientVisible {
		return
	}
	fmt.Fprint(os.Stdout, "\x1b7\r\n\x1b[K\x1b8")
	r.transientVisible = false
}

func (r *terminalLineReader) renderSuggestionLocked(text string) {
	rendered := styleMuted.Render(text)
	r.renderTransientLocked(rendered)
}

func (r *terminalLineReader) writeOutput(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.clearSuggestionLocked()
	return r.editor.Write(p)
}

func (r *terminalLineReader) ShowTransient(rendered string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.renderTransientLocked(rendered)
}

func (r *terminalLineReader) ClearTransient() {
	r.clearSuggestion()
}

func (r *terminalLineReader) renderTransientLocked(rendered string) {
	if rendered == "" {
		r.clearSuggestionLocked()
		return
	}
	fmt.Fprintf(os.Stdout, "\x1b7\r\n\x1b[K%s\x1b8", rendered)
	r.transientVisible = true
}

type terminalOutputWriter struct {
	owner *terminalLineReader
}

func (w *terminalOutputWriter) Write(p []byte) (int, error) {
	return w.owner.writeOutput(p)
}

type terminalReadWriter struct {
	stdin  *os.File
	stdout *os.File
	owner  *terminalLineReader
}

func (rw *terminalReadWriter) Read(p []byte) (int, error) {
	n, err := rw.stdin.Read(p)
	if n > 0 && shouldClearSuggestionOnInput(p[:n]) {
		rw.owner.clearSuggestion()
	}
	return n, err
}

func (rw *terminalReadWriter) Write(p []byte) (int, error) {
	return rw.stdout.Write(p)
}

func shouldClearSuggestionOnInput(raw []byte) bool {
	for _, b := range raw {
		switch b {
		case 3, 4, 8, 12, 13, 21, 23, 27, 127:
			return true
		}
	}
	return false
}
