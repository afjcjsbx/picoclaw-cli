package cli

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"golang.org/x/term"
)

const promptText = "› "

var (
	bracketedPasteStart = []byte("\x1b[200~")
	bracketedPasteEnd   = []byte("\x1b[201~")
)

const structuredPasteToken = "⧉"

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
	ShowSpinner(rendered string)
	ClearSpinner()
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
	reader.paste.owner = reader
	reader.output = &terminalOutputWriter{owner: reader}

	editor := term.NewTerminal(&terminalReadWriter{
		stdin:  os.Stdin,
		stdout: os.Stdout,
		owner:  reader,
	}, promptText)
	reader.editor = editor
	editor.AutoCompleteCallback = reader.autoCompleteCallback
	editor.SetBracketedPasteMode(true)
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
	paste  structuredPasteTransformer

	mu               sync.Mutex
	transientVisible bool
	spinnerVisible   bool
}

func (r *terminalLineReader) ReadLine() (string, error) {
	line, err := r.editor.ReadLine()
	if rewritten, ok := r.paste.ConsumeSubmittedLine(line); ok {
		line = rewritten
	}
	if errors.Is(err, term.ErrPasteIndicator) {
		return line, nil
	}
	return line, err
}

func (r *terminalLineReader) Close() error {
	r.clearSuggestion()
	r.editor.SetBracketedPasteMode(false)
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
	if r.paste.HasActiveDraft() {
		return
	}

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
	fmt.Fprint(os.Stdout, "\x1b7\r\n\x1b[J\x1b8")
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

func (r *terminalLineReader) ShowSpinner(rendered string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.renderSpinnerLocked(rendered)
}

func (r *terminalLineReader) ClearSpinner() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.clearSpinnerLocked()
}

func (r *terminalLineReader) renderTransientLocked(rendered string) {
	if rendered == "" {
		r.clearSuggestionLocked()
		return
	}
	r.clearSuggestionLocked()
	rendered = strings.ReplaceAll(rendered, "\n", "\r\n")
	fmt.Fprintf(os.Stdout, "\x1b7\r\n\x1b[J%s\x1b8", rendered)
	r.transientVisible = true
}

func (r *terminalLineReader) clearSpinnerLocked() {
	if !r.spinnerVisible {
		return
	}
	fmt.Fprint(os.Stdout, "\x1b7\r\n\x1b[J\x1b[A\r\x1b[K\x1b8")
	r.spinnerVisible = false
}

func (r *terminalLineReader) renderSpinnerLocked(rendered string) {
	if rendered == "" {
		r.clearSpinnerLocked()
		return
	}
	rendered = strings.ReplaceAll(rendered, "\n", " ")
	if r.spinnerVisible {
		fmt.Fprintf(os.Stdout, "\x1b7\r\n\x1b[A\r\x1b[K%s\x1b8", rendered)
		return
	}
	fmt.Fprintf(os.Stdout, "\x1b7\r\n\x1b[K%s\x1b8", rendered)
	r.spinnerVisible = true
}

func (r *terminalLineReader) showStructuredDraftPreview(text string) {
	r.printPersistent(renderStructuredDraftPreview(text) + "\n\n")
}

func (r *terminalLineReader) printStructuredDraftDiscarded() {
	r.printPersistent(styleStatus.Render("· ") + "Discarded pasted block.\n")
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

	pendingErr error
}

func (rw *terminalReadWriter) Read(p []byte) (int, error) {
	if n := rw.owner.paste.Pop(p); n > 0 {
		return n, nil
	}
	if rw.pendingErr != nil {
		err := rw.pendingErr
		rw.pendingErr = nil
		return 0, err
	}

	readBuf := make([]byte, len(p))
	for {
		n, err := rw.stdin.Read(readBuf)
		if n > 0 {
			raw := append([]byte(nil), readBuf[:n]...)
			if shouldClearSuggestionOnInput(raw) {
				rw.owner.clearSuggestion()
			}
			rw.owner.paste.Push(raw)
		}
		if err != nil {
			rw.pendingErr = err
		}
		if n := rw.owner.paste.Pop(p); n > 0 {
			return n, nil
		}
		if rw.pendingErr != nil {
			err := rw.pendingErr
			rw.pendingErr = nil
			return 0, err
		}
	}
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

func (r *terminalLineReader) printPersistent(rendered string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.clearSuggestionLocked()
	r.clearSpinnerLocked()
	_, _ = r.editor.Write([]byte(strings.ReplaceAll(rendered, "\n", "\r\n")))
}

type structuredPasteTransformer struct {
	owner *terminalLineReader

	inPaste bool

	remainder []byte
	pasteBuf  bytes.Buffer
	outBuf    []byte

	activeDraft    string
	submittedDraft string
}

func (t *structuredPasteTransformer) Push(raw []byte) {
	if len(raw) == 0 {
		return
	}
	data := append(append([]byte(nil), t.remainder...), raw...)
	t.remainder = nil
	t.outBuf = append(t.outBuf, t.transform(data)...)
}

func (t *structuredPasteTransformer) Pop(dst []byte) int {
	if len(t.outBuf) == 0 {
		return 0
	}
	n := copy(dst, t.outBuf)
	t.outBuf = t.outBuf[n:]
	if len(t.outBuf) == 0 {
		t.outBuf = nil
	}
	return n
}

func (t *structuredPasteTransformer) HasActiveDraft() bool {
	return t.activeDraft != ""
}

func (t *structuredPasteTransformer) ConsumeSubmittedLine(line string) (string, bool) {
	if t.submittedDraft == "" {
		return "", false
	}

	draft := t.submittedDraft
	t.submittedDraft = ""
	if strings.Contains(line, structuredPasteToken) {
		return strings.Replace(line, structuredPasteToken, draft, 1), true
	}
	if line == "" {
		return draft, true
	}
	return line + draft, true
}

func (t *structuredPasteTransformer) transform(data []byte) []byte {
	out := make([]byte, 0, len(data))
	for len(data) > 0 {
		if t.inPaste {
			idx := bytes.Index(data, bracketedPasteEnd)
			if idx >= 0 {
				t.pasteBuf.Write(data[:idx])
				data = data[idx+len(bracketedPasteEnd):]
				out = append(out, t.flushPaste()...)
				t.inPaste = false
				continue
			}

			keep := trailingSequencePrefixLen(data, bracketedPasteEnd)
			if keep > 0 {
				t.pasteBuf.Write(data[:len(data)-keep])
				t.remainder = append(t.remainder[:0], data[len(data)-keep:]...)
			} else {
				t.pasteBuf.Write(data)
			}
			break
		}

		idx := bytes.Index(data, bracketedPasteStart)
		if idx >= 0 {
			out = append(out, t.consumeRegularInput(data[:idx])...)
			data = data[idx+len(bracketedPasteStart):]
			t.inPaste = true
			continue
		}

		keep := trailingSequencePrefixLen(data, bracketedPasteStart)
		plain := data
		if keep > 0 {
			plain = data[:len(data)-keep]
			t.remainder = append(t.remainder[:0], data[len(data)-keep:]...)
		}
		out = append(out, t.consumeRegularInput(plain)...)
		break
	}
	return out
}

func (t *structuredPasteTransformer) flushPaste() []byte {
	text := normalizePasteText(t.pasteBuf.Bytes())
	t.pasteBuf.Reset()
	if t.activeDraft != "" {
		t.appendStructuredText(text, true)
		return nil
	}
	if !strings.ContainsAny(text, "\n\t") {
		return []byte(text)
	}

	t.activeDraft = text
	t.renderDraftPreview()
	return []byte(structuredPasteToken)
}

func trailingSequencePrefixLen(data, seq []byte) int {
	max := len(seq) - 1
	if max > len(data) {
		max = len(data)
	}
	for size := max; size > 0; size-- {
		if bytes.Equal(data[len(data)-size:], seq[:size]) {
			return size
		}
	}
	return 0
}

func normalizePasteText(raw []byte) string {
	var builder strings.Builder
	builder.Grow(len(raw))
	for i := 0; i < len(raw); i++ {
		switch raw[i] {
		case '\r':
			if i+1 < len(raw) && raw[i+1] == '\n' {
				i++
			}
			builder.WriteByte('\n')
		default:
			builder.WriteByte(raw[i])
		}
	}
	return builder.String()
}

func (t *structuredPasteTransformer) consumeRegularInput(raw []byte) []byte {
	if len(raw) == 0 || t.activeDraft == "" {
		return raw
	}

	switch {
	case raw[0] == '\r' || raw[0] == '\n':
		t.submitActiveDraft()
		return raw
	case raw[0] == 3 || raw[0] == 4 || raw[0] == 8 || raw[0] == 21 || raw[0] == 127:
		t.clearActiveDraft()
		return raw
	case bytes.HasPrefix(raw, []byte{27, '[', '3', '~'}):
		t.clearActiveDraft()
		return raw
	default:
		return raw
	}
}

func (t *structuredPasteTransformer) appendStructuredText(text string, refreshPreview bool) {
	if text == "" {
		return
	}
	t.activeDraft += text
	if refreshPreview {
		t.renderDraftPreview()
	}
}

func (t *structuredPasteTransformer) submitActiveDraft() {
	if t.activeDraft == "" {
		return
	}

	t.submittedDraft = t.activeDraft
	t.activeDraft = ""
}

func (t *structuredPasteTransformer) clearActiveDraft() {
	wasActive := t.activeDraft != ""
	t.activeDraft = ""
	if t.owner != nil && wasActive {
		t.owner.printStructuredDraftDiscarded()
	}
}

func (t *structuredPasteTransformer) renderDraftPreview() {
	if t.owner == nil {
		return
	}
	t.owner.showStructuredDraftPreview(t.activeDraft)
}

func renderStructuredDraftPreview(text string) string {
	lines := strings.Split(text, "\n")
	body := strings.ReplaceAll(text, "\t", "    ")
	meta := styleMuted.Render(fmt.Sprintf("%d line(s). Enter sends, Backspace/Ctrl+U discard.", len(lines)))
	return renderTranscriptEntry("◌", "Pasted Block", "", meta+"\n"+renderCodeFence(body, ""), colorAssistant)
}
