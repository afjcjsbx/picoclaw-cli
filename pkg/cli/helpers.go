package cli

import (
	"net/url"
	"os"
	"strings"
)

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func buildWSURL(rawURL, sessionID, token string, tokenQuery bool) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}

	query := parsed.Query()
	query.Set("session_id", sessionID)
	if tokenQuery && token != "" {
		query.Set("token", token)
	}
	parsed.RawQuery = query.Encode()

	return parsed.String(), nil
}

func parseToggle(value string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "on", "true", "1", "yes":
		return true, true
	case "off", "false", "0", "no":
		return false, true
	default:
		return false, false
	}
}

func onOff(v bool) string {
	if v {
		return "on"
	}
	return "off"
}

func ternary[T any](cond bool, whenTrue, whenFalse T) T {
	if cond {
		return whenTrue
	}
	return whenFalse
}
