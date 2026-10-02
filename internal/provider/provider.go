// Package provider LLM 决策器抽象(M02 能力位 + M04 决策器)。
// 能力位 Tools:支持则 function calling,否则降级 ReAct。
package provider

import "context"

// ToolCall 一次工具调用请求(assistant 消息里的 tool_calls)。
type ToolCall struct {
	ID        string
	Name      string
	Arguments string // JSON 字符串
}

// Message 统一消息模型:system | user | assistant | tool。
type Message struct {
	Role       string
	Content    string
	ToolCalls  []ToolCall
	ToolCallID string
}

// ToolDef 工具定义(JSON Schema 参数 + Go 实现)。
type ToolDef struct {
	Name        string
	Description string
	Parameters  map[string]any
	Func        func(map[string]any) string
}

// Capabilities 提供商能力位:Tools=false 时循环降级 ReAct。
type Capabilities struct {
	Tools bool
}

// Provider 任意 LLM 的统一接口。
type Provider interface {
	Chat(ctx context.Context, msgs []Message, tools []ToolDef, stop []string) (Message, error)
	Capabilities() Capabilities
}
