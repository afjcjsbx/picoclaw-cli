package cli

import (
	"strings"
	"unicode"
)

var localCommandNames = []string{
	"/help",
	"/status",
	"/session",
	"/thoughts",
	"/tools",
	"/clear",
	"/quit",
	"/exit",
}

var localCommandArgChoices = map[string][]string{
	"/thoughts": {"on", "off"},
	"/tools":    {"on", "off"},
}

func slashCommandAutoComplete(line string, pos int, key rune) (string, int, bool) {
	if key != '\t' {
		return "", 0, false
	}
	return completeLocalCommand(line, pos)
}

func applyKeyToLine(line string, pos int, key rune) (string, int, bool) {
	if pos < 0 || pos > len(line) {
		return "", 0, false
	}
	if key == '\t' || !unicode.IsPrint(key) {
		return "", 0, false
	}

	inserted := string(key)
	newLine := line[:pos] + inserted + line[pos:]
	return newLine, pos + len(inserted), true
}

func completeLocalCommand(line string, pos int) (string, int, bool) {
	if pos < 0 || pos > len(line) || !strings.HasPrefix(line, "/") {
		return "", 0, false
	}

	prefix := line[:pos]
	suffix := line[pos:]

	if !strings.ContainsAny(prefix, " \t") {
		return applyCompletion(line, prefix, suffix, 0, localCommandNames)
	}

	fields := strings.Fields(prefix)
	if len(fields) == 0 {
		return "", 0, false
	}

	command := fields[0]
	choices, ok := localCommandArgChoices[command]
	if !ok {
		return "", 0, false
	}

	if strings.HasSuffix(prefix, " ") || strings.HasSuffix(prefix, "\t") {
		if len(fields) != 1 {
			return "", 0, false
		}
		return applyCompletion(line, "", suffix, pos, choices)
	}

	if len(fields) != 2 {
		return "", 0, false
	}

	argPrefix := fields[1]
	start := pos - len(argPrefix)
	return applyCompletion(line, argPrefix, suffix, start, choices)
}

func applyCompletion(line, current, suffix string, start int, choices []string) (string, int, bool) {
	matches := matchingChoices(current, choices)
	if len(matches) == 0 {
		return "", 0, false
	}

	completed := longestSharedPrefix(matches)
	if completed == current && len(matches) != 1 {
		return "", 0, false
	}

	newLine := line[:start] + completed + suffix
	newPos := start + len(completed)

	if len(matches) == 1 && completed == matches[0] && !strings.HasPrefix(suffix, " ") {
		newLine = line[:start] + completed + " " + suffix
		newPos++
	}

	return newLine, newPos, true
}

func matchingChoices(prefix string, choices []string) []string {
	var matches []string
	for _, choice := range choices {
		if strings.HasPrefix(choice, prefix) {
			matches = append(matches, choice)
		}
	}
	return matches
}

func longestSharedPrefix(values []string) string {
	if len(values) == 0 {
		return ""
	}

	prefix := values[0]
	for _, value := range values[1:] {
		for !strings.HasPrefix(value, prefix) && prefix != "" {
			prefix = prefix[:len(prefix)-1]
		}
		if prefix == "" {
			return ""
		}
	}
	return prefix
}

func suggestionTextForLine(line string, pos int) string {
	matches := suggestionsForLine(line, pos)
	if len(matches) == 0 {
		return ""
	}

	if len(matches) == 1 {
		return "Suggestion: " + matches[0]
	}
	return "Suggestions: " + strings.Join(matches, ", ")
}

func suggestionsForLine(line string, pos int) []string {
	if pos < 0 || pos > len(line) {
		return nil
	}

	prefix := line[:pos]
	if !strings.HasPrefix(prefix, "/") {
		return nil
	}

	if !strings.ContainsAny(prefix, " \t") {
		matches := matchingChoices(prefix, localCommandNames)
		if len(matches) == 1 && matches[0] == prefix {
			return nil
		}
		return matches
	}

	fields := strings.Fields(prefix)
	if len(fields) == 0 {
		return nil
	}

	command := fields[0]
	choices, ok := localCommandArgChoices[command]
	if !ok {
		return nil
	}

	if strings.HasSuffix(prefix, " ") || strings.HasSuffix(prefix, "\t") {
		return choices
	}

	if len(fields) != 2 {
		return nil
	}

	argPrefix := fields[1]
	matches := matchingChoices(argPrefix, choices)
	if len(matches) == 1 && matches[0] == argPrefix {
		return nil
	}
	return matches
}
