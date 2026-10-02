// Anthropic 提供商:/v1/messages + tool_use/tool_result 块。
package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type Anthropic struct {
	Model   string
	APIKey  string
	BaseURL string
	Client  *http.Client
}

func NewAnthropic(model, apiKey, baseURL string) *Anthropic {
	return &Anthropic{
		Model:   model,
		APIKey:  apiKey,
		BaseURL: baseURL,
		Client:  &http.Client{Timeout: 180 * time.Second},
	}
}

func (p *Anthropic) Capabilities() Capabilities { return Capabilities{Tools: true} }

// messagesToAnthropic 把统一 Message 转成 Anthropic 消息格式:
// system 单独顶层;assistant 含 tool_use 块;tool 角色包成 user/tool_result。
func messagesToAnthropic(msgs []Message) (string, []map[string]any) {
	system := ""
	var blocks []map[string]any
	for _, m := range msgs {
		switch m.Role {
		case "system":
			system += m.Content + "\n"
		case "assistant":
			if len(m.ToolCalls) > 0 {
				var content []map[string]any
				if m.Content != "" {
					content = append(content, map[string]any{"type": "text", "text": m.Content})
				}
				for _, tc := range m.ToolCalls {
					var input any
					_ = json.Unmarshal([]byte(tc.Arguments), &input)
					content = append(content, map[string]any{
						"type": "tool_use", "id": tc.ID, "name": tc.Name, "input": input,
					})
				}
				blocks = append(blocks, map[string]any{"role": "assistant", "content": content})
			} else {
				blocks = append(blocks, map[string]any{"role": "assistant", "content": m.Content})
			}
		case "tool":
			blocks = append(blocks, map[string]any{
				"role": "user",
				"content": []map[string]any{
					{"type": "tool_result", "tool_use_id": m.ToolCallID, "content": m.Content},
				},
			})
		default:
			blocks = append(blocks, map[string]any{"role": m.Role, "content": m.Content})
		}
	}
	return system, blocks
}

func (p *Anthropic) Chat(ctx context.Context, msgs []Message, tools []ToolDef, stop []string) (Message, error) {
	system, blocks := messagesToAnthropic(msgs)
	payload := map[string]any{"model": p.Model, "messages": blocks, "max_tokens": 8192}
	if system != "" {
		payload["system"] = system
	}
	if len(tools) > 0 {
		var defs []map[string]any
		for _, t := range tools {
			defs = append(defs, map[string]any{"name": t.Name, "description": t.Description, "input_schema": t.Parameters})
		}
		payload["tools"] = defs
	}
	if len(stop) > 0 {
		payload["stop_sequences"] = stop
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return Message{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.BaseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return Message{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := p.Client.Do(req)
	if err != nil {
		return Message{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var e struct{ Error map[string]any }
		_ = json.NewDecoder(resp.Body).Decode(&e)
		return Message{}, fmt.Errorf("Anthropic API %d: %v", resp.StatusCode, e.Error)
	}
	var data struct {
		Content []struct {
			Type  string         `json:"type"`
			Text  string         `json:"text"`
			ID    string         `json:"id"`
			Name  string         `json:"name"`
			Input map[string]any `json:"input"`
		} `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return Message{}, err
	}
	out := Message{Role: "assistant"}
	for _, b := range data.Content {
		switch b.Type {
		case "text":
			out.Content += b.Text
		case "tool_use":
			raw, _ := json.Marshal(b.Input)
			out.ToolCalls = append(out.ToolCalls, ToolCall{ID: b.ID, Name: b.Name, Arguments: string(raw)})
		}
	}
	return out, nil
}
