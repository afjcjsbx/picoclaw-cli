package cli

import (
	"bufio"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

const promptText = "› "

type lineReader interface {
	ReadLine() (string, error)
	Close() error
	Output() io.Writer
	ManagesPrompt() bool
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

	editor := term.NewTerminal(stdioReadWriter{Reader: os.Stdin, Writer: os.Stdout}, promptText)
	if width, height, err := term.GetSize(stdoutFD); err == nil && width > 0 && height > 0 {
		_ = editor.SetSize(width, height)
	}

	return &terminalLineReader{
		editor: editor,
		fd:     stdinFD,
		state:  state,
	}, nil
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

type terminalLineReader struct {
	editor *term.Terminal
	fd     int
	state  *term.State
}

func (r *terminalLineReader) ReadLine() (string, error) {
	return r.editor.ReadLine()
}

func (r *terminalLineReader) Close() error {
	return term.Restore(r.fd, r.state)
}

func (r *terminalLineReader) Output() io.Writer {
	return r.editor
}

func (r *terminalLineReader) ManagesPrompt() bool {
	return true
}

type stdioReadWriter struct {
	io.Reader
	io.Writer
}
