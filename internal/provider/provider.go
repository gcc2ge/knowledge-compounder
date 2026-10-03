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

// Usage LLM 响应的 token 用量(provider 解析 API 返回;缺失时 Runtime 用估算兜底)。
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// Message 统一消息模型:system | user | assistant | tool。
type Message struct {
	Role       string
	Content    string
	ToolCalls  []ToolCall
	ToolCallID string
	Usage      *Usage // 仅 assistant 响应携带;累计消费供 MaxTokens 闸门
}

// ToolDef 工具定义(JSON Schema 参数 + Go 实现)。
type ToolDef struct {
	Name        string
	Description string
	Parameters  map[string]any
	Func        func(map[string]any) string
}

// Capabilities 提供商能力位:Tools=false 时循环降级 ReAct;Stream=true 支持 SSE 增量。
type Capabilities struct {
	Tools  bool
	Stream bool
}

// Provider 任意 LLM 的统一接口。
type Provider interface {
	Chat(ctx context.Context, msgs []Message, tools []ToolDef, stop []string) (Message, error)
	Capabilities() Capabilities
}

// Streamer 流式决策(SSE 增量输出)。Provider 实现它且 runtime.OnStream 非 nil 时走流式。
type Streamer interface {
	Stream(ctx context.Context, msgs []Message, tools []ToolDef, stop []string, onDelta func(string)) (Message, error)
}
