// OpenAI 兼容提供商:同一协议覆盖 OpenAI/DeepSeek/Moonshot/OpenRouter/Ollama/vLLM/Gemini-OpenAI 端点。
package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

type OpenAICompatible struct {
	Model     string
	APIKey    string
	BaseURL   string
	MaxTokens int // 0 = 模型默认上限;>0 透传 max_tokens 限制单次输出长度
	Client    *http.Client

	// 工具定义序列化缓存:一次运行的工具集固定(工具名唯一),避免每步 Chat 重建 map。
	toolCacheKey  string
	toolCacheDefs []map[string]any
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

// openAITools 缓存版 buildOpenAITools:一次运行的工具集固定,按工具名列表签名缓存,
// 避免每步 Chat/Stream 重建 map。签名含 name+参数个数,足以区分不同工具集。
func (p *OpenAICompatible) openAITools(tools []ToolDef) []map[string]any {
	if len(tools) == 0 {
		return nil
	}
	key := toolsSignature(tools)
	if key == p.toolCacheKey {
		return p.toolCacheDefs
	}
	defs := buildOpenAITools(tools)
	p.toolCacheKey, p.toolCacheDefs = key, defs
	return defs
}

// toolsSignature 工具集指纹:name+参数个数。一次运行内工具名唯一且参数绑定 name,
// 此签名足以区分不同工具集;O(len(tools)) 免哈希。
func toolsSignature(tools []ToolDef) string {
	var b strings.Builder
	for _, t := range tools {
		b.WriteString(t.Name)
		b.WriteByte(':')
		b.WriteString(strconv.Itoa(len(t.Parameters)))
		b.WriteByte('\n')
	}
	return b.String()
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
	if p.MaxTokens > 0 {
		payload["max_tokens"] = p.MaxTokens
	}
	if defs := p.openAITools(tools); len(defs) > 0 {
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
				Reasoning string `json:"reasoning_content"`
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
	out := Message{Role: "assistant", Content: apiMsg.Content, Reasoning: apiMsg.Reasoning, Usage: data.Usage}
	for _, tc := range apiMsg.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, ToolCall{ID: tc.ID, Name: tc.Function.Name, Arguments: tc.Function.Arguments})
	}
	// 推理兜底:推理模型只回 reasoning_content 且无工具调用时,把它当最终文本给出
	if out.Content == "" && len(out.ToolCalls) == 0 && out.Reasoning != "" {
		out.Content = out.Reasoning
	}
	return out, nil
}

// Stream SSE 流式版 Chat:delta.content → onDelta;tool_calls 增量按 index 归并。
func (p *OpenAICompatible) Stream(ctx context.Context, msgs []Message, tools []ToolDef, stop []string, onDelta func(string)) (Message, error) {
	payload := map[string]any{"model": p.Model, "messages": messagesToAPI(msgs), "stream": true}
	if p.MaxTokens > 0 {
		payload["max_tokens"] = p.MaxTokens
	}
	if defs := p.openAITools(tools); len(defs) > 0 {
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
					Reasoning string `json:"reasoning_content"`
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
			out.Reasoning += c.Delta.Reasoning
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
	// 推理兜底:流式下只收到 reasoning_content 且无工具调用 → 当作最终文本
	if out.Content == "" && len(out.ToolCalls) == 0 && out.Reasoning != "" {
		out.Content = out.Reasoning
	}
	return out, nil
}

// Vision 让多模态模型描述一张本地图片(compiler 编译 raw/ 图片源的入口)。
// 走 OpenAI 兼容的 image_url content part;模型无视觉能力时 API 返回错误,原样透出。
func (p *OpenAICompatible) Vision(ctx context.Context, imagePath, prompt string) (string, error) {
	b, err := os.ReadFile(imagePath)
	if err != nil {
		return "", err
	}
	if len(b) > 5<<20 {
		return "", fmt.Errorf("图片 %d KB 超过 5MB 上限", len(b)>>10)
	}
	mime := imageMIME(imagePath)
	if mime == "" {
		return "", fmt.Errorf("不支持的图片扩展名(仅 png/jpg/jpeg/gif/webp/bmp)")
	}
	payload := map[string]any{
		"model": p.Model,
		"messages": []map[string]any{{
			"role": "user",
			"content": []map[string]any{
				{"type": "text", "text": prompt},
				{"type": "image_url", "image_url": map[string]any{"url": "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(b)}},
			},
		}},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.APIKey)
	}
	resp, err := p.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var e struct{ Error map[string]any }
		_ = json.NewDecoder(resp.Body).Decode(&e)
		return "", fmt.Errorf("视觉 API %d: %v", resp.StatusCode, e.Error)
	}
	var data struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", err
	}
	if len(data.Choices) == 0 {
		return "", fmt.Errorf("视觉 API 无输出")
	}
	return data.Choices[0].Message.Content, nil
}

// imageMIME 按扩展名给 MIME;不认识返回空(调用方报错)。
func imageMIME(path string) string {
	switch strings.ToLower(filepathExt(path)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".bmp":
		return "image/bmp"
	}
	return ""
}

func filepathExt(path string) string {
	if i := strings.LastIndexByte(path, '.'); i >= 0 {
		return path[i:]
	}
	return ""
}
