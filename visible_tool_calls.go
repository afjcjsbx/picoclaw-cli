package main

type VisibleToolCall struct {
	ID           string                       `json:"id,omitempty"`
	Type         string                       `json:"type,omitempty"`
	Function     *VisibleToolCallFunction     `json:"function,omitempty"`
	ExtraContent *VisibleToolCallExtraContent `json:"extra_content,omitempty"`
}

type VisibleToolCallFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type VisibleToolCallExtraContent struct {
	ToolFeedbackExplanation string `json:"tool_feedback_explanation,omitempty"`
}
