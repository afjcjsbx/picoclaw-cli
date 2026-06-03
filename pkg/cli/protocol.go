package cli

import (
	"encoding/json"
	"regexp"
	"strings"
)

const (
	typeMessageSend   = "message.send"
	typeMessageCreate = "message.create"
	typeMessageUpdate = "message.update"
	typeMessageDelete = "message.delete"
	typeMediaCreate   = "media.create"
	typeTypingStart   = "typing.start"
	typeTypingStop    = "typing.stop"
	typeError         = "error"
	typePong          = "pong"

	payloadKeyContent = "content"
	payloadKeyKind    = "kind"
	payloadKeyTools   = "tool_calls"
	payloadKeyModel   = "model_name"

	messageKindThought   = "thought"
	messageKindToolCalls = "tool_calls"
)

var (
	toolFeedbackPattern = regexp.MustCompile("^\\s*(?:[^A-Za-z0-9`\\r\\n]+\\s*)?`([^`\\n\\r]*?)(?:\\.{1,2})?`[^\\n\\r]*(?:\\r?\\n([\\s\\S]*))?$")
	codeFencePattern    = regexp.MustCompile("(?m)```(?:json)?\\r?\\n([\\s\\S]*?)\\r?\\n```")
)

type picoMessage struct {
	Type      string         `json:"type"`
	ID        string         `json:"id,omitempty"`
	SessionID string         `json:"session_id,omitempty"`
	Timestamp int64          `json:"timestamp,omitempty"`
	Payload   map[string]any `json:"payload,omitempty"`
}

type displayKind string

const (
	displayNormal       displayKind = "normal"
	displayThought      displayKind = "thought"
	displayToolCalls    displayKind = "tool_calls"
	displayToolFeedback displayKind = "tool_feedback"
	displayPlaceholder  displayKind = "placeholder"
)

type messageView struct {
	kind      displayKind
	modelName string
	content   string
	toolCalls []VisibleToolCall
}

func buildMessageView(payload map[string]any, prev messageView) messageView {
	content, _ := payload[payloadKeyContent].(string)
	modelName := strings.TrimSpace(stringValue(payload[payloadKeyModel]))
	if modelName == "" {
		modelName = prev.modelName
	}

	if payload["placeholder"] == true {
		return messageView{
			kind:      displayPlaceholder,
			modelName: modelName,
			content:   content,
		}
	}

	toolCalls := parseVisibleToolCalls(payload[payloadKeyTools])
	kind := normalizedKind(payload)
	switch {
	case kind == messageKindThought:
		return messageView{
			kind:      displayThought,
			modelName: modelName,
			content:   content,
		}
	case kind == messageKindToolCalls || len(toolCalls) > 0:
		return messageView{
			kind:      displayToolCalls,
			modelName: modelName,
			content:   content,
			toolCalls: toolCalls,
		}
	}

	if legacyCalls, ok := parseLegacyToolFeedback(content); ok {
		return messageView{
			kind:      displayToolFeedback,
			modelName: modelName,
			content:   content,
			toolCalls: legacyCalls,
		}
	}

	return messageView{
		kind:      displayNormal,
		modelName: modelName,
		content:   content,
	}
}

func parseVisibleToolCalls(raw any) []VisibleToolCall {
	if raw == nil {
		return nil
	}

	data, err := json.Marshal(raw)
	if err != nil {
		return nil
	}

	var toolCalls []VisibleToolCall
	if err := json.Unmarshal(data, &toolCalls); err != nil || len(toolCalls) == 0 {
		return nil
	}
	return toolCalls
}

func parseLegacyToolFeedback(content string) ([]VisibleToolCall, bool) {
	trimmed := strings.TrimSpace(content)
	matches := toolFeedbackPattern.FindStringSubmatch(trimmed)
	if len(matches) == 0 {
		return nil, false
	}

	toolName := strings.TrimSpace(matches[1])
	body := ""
	if len(matches) > 2 {
		body = strings.TrimSpace(matches[2])
	}
	argsText, explanation := splitToolFeedbackBody(body)

	call := VisibleToolCall{
		Type: "function",
	}
	if toolName != "" || argsText != "" {
		call.Function = &VisibleToolCallFunction{
			Name:      toolName,
			Arguments: argsText,
		}
	}
	if explanation != "" {
		call.ExtraContent = &VisibleToolCallExtraContent{
			ToolFeedbackExplanation: explanation,
		}
	}

	if call.Function == nil && call.ExtraContent == nil {
		return nil, false
	}
	return []VisibleToolCall{call}, true
}

func splitToolFeedbackBody(body string) (string, string) {
	if strings.TrimSpace(body) == "" {
		return "", ""
	}

	codeFence := codeFencePattern.FindStringSubmatch(body)
	argsText := ""
	if len(codeFence) > 1 {
		argsText = strings.TrimSpace(codeFence[1])
	}

	explanation := strings.TrimSpace(codeFencePattern.ReplaceAllString(body, ""))
	return argsText, explanation
}

func normalizedKind(payload map[string]any) string {
	return strings.ToLower(strings.TrimSpace(stringValue(payload[payloadKeyKind])))
}

func stringValue(raw any) string {
	value, _ := raw.(string)
	return value
}
