// OpenAI 兼容提供商:同一协议覆盖 OpenAI/DeepSeek/Moonshot/OpenRouter/Ollama/vLLM/Gemini-OpenAI 端点。
package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

type OpenAICompatible struct {
	Model   string
	APIKey  string
	BaseURL string
	Client  *http.Client
}

func NewOpenAICompatible(model, apiKey, baseURL string) *OpenAICompatible {
	return &OpenAICompatible{
		Model:   model,
		APIKey:  apiKey,
		BaseURL: baseURL,
		Client:  &http.Client{Timeout: 180 * time.Second},
	}
}

func (p *OpenAICompatible) Capabilities() Capabilities {
	return Capabilities{Tools: true, Stream: true}
}

// buildOpenAITools 序列化工具定义(OpenAI function calling 格式)。
func buildOpenAITools(tools []ToolDef) []map[string]any {
	if len(tools) == 0 {
		return nil
	}
	defs := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		defs = append(defs, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name": t.Name, "description": t.Description, "parameters": t.Parameters,
			},
		})
	}
	return defs
}

// messagesToAPI 把统一 Message 转成 OpenAI chat 格式(tool_calls / tool 角色)。
func messagesToAPI(msgs []Message) []map[string]any {
	out := make([]map[string]any, 0, len(msgs))
	for _, m := range msgs {
		switch m.Role {
		case "assistant":
			if len(m.ToolCalls) > 0 {
				var tcs []map[string]any
				for _, tc := range m.ToolCalls {
					tcs = append(tcs, map[string]any{
						"id": tc.ID, "type": "function",
						"function": map[string]any{"name": tc.Name, "arguments": tc.Arguments},
					})
				}
				out = append(out, map[string]any{"role": "assistant", "content": m.Content, "tool_calls": tcs})
			} else {
				out = append(out, map[string]any{"role": "assistant", "content": m.Content})
			}
		case "tool":
			out = append(out, map[string]any{"role": "tool", "tool_call_id": m.ToolCallID, "content": m.Content})
		default:
			out = append(out, map[string]any{"role": m.Role, "content": m.Content})
		}
	}
	return out
}

func (p *OpenAICompatible) Chat(ctx context.Context, msgs []Message, tools []ToolDef, stop []string) (Message, error) {
	payload := map[string]any{"model": p.Model, "messages": messagesToAPI(msgs)}
	if defs := buildOpenAITools(tools); len(defs) > 0 {
		payload["tools"] = defs
	}
	if len(stop) > 0 {
		payload["stop"] = stop
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return Message{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Message{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.APIKey)
	}

	resp, err := p.Client.Do(req)
	if err != nil {
		return Message{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var e struct{ Error map[string]any }
		_ = json.NewDecoder(resp.Body).Decode(&e)
		return Message{}, fmt.Errorf("LLM API %d: %v", resp.StatusCode, e.Error)
	}
	var data struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
		Usage *Usage `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return Message{}, err
	}
	if len(data.Choices) == 0 {
		return Message{}, fmt.Errorf("LLM 无输出")
	}
	apiMsg := data.Choices[0].Message
	out := Message{Role: "assistant", Content: apiMsg.Content, Usage: data.Usage}
	for _, tc := range apiMsg.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, ToolCall{ID: tc.ID, Name: tc.Function.Name, Arguments: tc.Function.Arguments})
	}
	return out, nil
}

// Stream SSE 流式版 Chat:delta.content → onDelta;tool_calls 增量按 index 归并。
func (p *OpenAICompatible) Stream(ctx context.Context, msgs []Message, tools []ToolDef, stop []string, onDelta func(string)) (Message, error) {
	payload := map[string]any{"model": p.Model, "messages": messagesToAPI(msgs), "stream": true}
	if defs := buildOpenAITools(tools); len(defs) > 0 {
		payload["tools"] = defs
	}
	// stream_options.include_usage 让流式末帧携带 usage(真实 token 消耗,供 MaxTokens 闸门)
	payload["stream_options"] = map[string]any{"include_usage": true}
	if len(stop) > 0 {
		payload["stop"] = stop
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return Message{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Message{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.APIKey)
	}
	resp, err := p.Client.Do(req)
	if err != nil {
		return Message{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var e struct{ Error map[string]any }
		_ = json.NewDecoder(resp.Body).Decode(&e)
		return Message{}, fmt.Errorf("LLM API %d: %v", resp.StatusCode, e.Error)
	}

	out := Message{Role: "assistant"}
	toolByIndex := map[int]*ToolCall{}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
			Usage *Usage `json:"usage"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if chunk.Usage != nil {
			out.Usage = chunk.Usage
		}
		for _, c := range chunk.Choices {
			if c.Delta.Content != "" {
				out.Content += c.Delta.Content
				if onDelta != nil {
					onDelta(c.Delta.Content)
				}
			}
			for _, tc := range c.Delta.ToolCalls {
				t := toolByIndex[tc.Index]
				if t == nil {
					t = &ToolCall{}
					toolByIndex[tc.Index] = t
				}
				if tc.ID != "" {
					t.ID = tc.ID
				}
				if tc.Function.Name != "" {
					t.Name = tc.Function.Name
				}
				t.Arguments += tc.Function.Arguments
			}
		}
	}
	if err := sc.Err(); err != nil {
		return Message{}, err
	}
	indices := make([]int, 0, len(toolByIndex))
	for i := range toolByIndex {
		indices = append(indices, i)
	}
	sort.Ints(indices)
	for _, i := range indices {
		out.ToolCalls = append(out.ToolCalls, *toolByIndex[i])
	}
	return out, nil
}
