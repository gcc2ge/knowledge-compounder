// OpenAI 兼容提供商:同一协议覆盖 OpenAI/DeepSeek/Moonshot/OpenRouter/Ollama/vLLM/Gemini-OpenAI 端点。
package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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

func (p *OpenAICompatible) Capabilities() Capabilities { return Capabilities{Tools: true} }

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
	if len(tools) > 0 {
		var defs []map[string]any
		for _, t := range tools {
			defs = append(defs, map[string]any{
				"type": "function",
				"function": map[string]any{
					"name": t.Name, "description": t.Description, "parameters": t.Parameters,
				},
			})
		}
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
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return Message{}, err
	}
	if len(data.Choices) == 0 {
		return Message{}, fmt.Errorf("LLM 无输出")
	}
	apiMsg := data.Choices[0].Message
	out := Message{Role: "assistant", Content: apiMsg.Content}
	for _, tc := range apiMsg.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, ToolCall{ID: tc.ID, Name: tc.Function.Name, Arguments: tc.Function.Arguments})
	}
	return out, nil
}
