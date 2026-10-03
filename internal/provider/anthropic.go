// Anthropic 提供商:/v1/messages + tool_use/tool_result 块。
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

func (p *Anthropic) Capabilities() Capabilities { return Capabilities{Tools: true, Stream: true} }

// buildAnthropicTools 序列化工具定义(Anthropic tool 格式)。
func buildAnthropicTools(tools []ToolDef) []map[string]any {
	if len(tools) == 0 {
		return nil
	}
	defs := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		defs = append(defs, map[string]any{"name": t.Name, "description": t.Description, "input_schema": t.Parameters})
	}
	return defs
}

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
		payload["tools"] = buildAnthropicTools(tools)
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

// Stream SSE 流式版 Chat:content_block_delta(text_delta/input_json_delta)按 index 归并。
func (p *Anthropic) Stream(ctx context.Context, msgs []Message, tools []ToolDef, stop []string, onDelta func(string)) (Message, error) {
	system, blocks := messagesToAnthropic(msgs)
	payload := map[string]any{"model": p.Model, "messages": blocks, "max_tokens": 8192, "stream": true}
	if system != "" {
		payload["system"] = system
	}
	if defs := buildAnthropicTools(tools); len(defs) > 0 {
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

	out := Message{Role: "assistant"}
	textByIndex := map[int]string{}
	toolByIndex := map[int]*ToolCall{}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var ev struct {
			Type         string `json:"type"`
			Index        *int   `json:"index"`
			ContentBlock *struct {
				Type string `json:"type"`
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"content_block"`
			Delta *struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				PartialJSON string `json:"partial_json"`
			} `json:"delta"`
		}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev); err != nil {
			continue
		}
		switch ev.Type {
		case "content_block_start":
			if ev.ContentBlock != nil && ev.ContentBlock.Type == "tool_use" && ev.Index != nil {
				toolByIndex[*ev.Index] = &ToolCall{ID: ev.ContentBlock.ID, Name: ev.ContentBlock.Name}
			}
		case "content_block_delta":
			if ev.Delta == nil || ev.Index == nil {
				continue
			}
			switch ev.Delta.Type {
			case "text_delta":
				textByIndex[*ev.Index] += ev.Delta.Text
				if onDelta != nil {
					onDelta(ev.Delta.Text)
				}
			case "input_json_delta":
				if t := toolByIndex[*ev.Index]; t != nil {
					t.Arguments += ev.Delta.PartialJSON
				}
			}
		}
	}
	if err := sc.Err(); err != nil {
		return Message{}, err
	}
	// 按 index 归并:text 拼接, tool_use 按序
	var textIdx, toolIdx []int
	for i := range textByIndex {
		textIdx = append(textIdx, i)
	}
	for i := range toolByIndex {
		toolIdx = append(toolIdx, i)
	}
	sort.Ints(textIdx)
	sort.Ints(toolIdx)
	for _, i := range textIdx {
		out.Content += textByIndex[i]
	}
	for _, i := range toolIdx {
		out.ToolCalls = append(out.ToolCalls, *toolByIndex[i])
	}
	return out, nil
}
