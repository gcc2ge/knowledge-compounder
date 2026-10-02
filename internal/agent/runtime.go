// Package agent M04 Agent 运行时:Think-Act-Observe 循环 + 工具 + 停止条件。
// - 工具结果作为 Observation 回填(messages 追加式,记忆 = Messages)
// - 错误三分类自愈雏形:工具不存在/执行失败 → 转观察文本喂回模型
// - 停止条件:MaxSteps + MaxSameAction(防原地打转);MaxTokens/Deadline 见 TODO
package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/knowledge-compounder/kcp/internal/provider"
)

type Runtime struct {
	Provider      provider.Provider
	SystemPrompt  string
	Tools         []provider.ToolDef
	MaxSteps      int
	MaxSameAction int
}

// Run 执行一次 agent 任务:LLM 决策 → 执行工具 → 观察回填 → 再决策,直到无工具调用或达到停止条件。
func (r *Runtime) Run(ctx context.Context, input, state string) (string, error) {
	msgs := []provider.Message{{Role: "system", Content: r.SystemPrompt}}
	if state != "" {
		msgs = append(msgs, provider.Message{Role: "user", Content: "[任务状态]\n" + state})
	}
	msgs = append(msgs, provider.Message{Role: "user", Content: input})

	same := map[string]int{}
	for step := 0; step < r.MaxSteps; step++ {
		reply, err := r.Provider.Chat(ctx, msgs, r.Tools, nil)
		if err != nil {
			return "", err
		}
		if len(reply.ToolCalls) == 0 {
			return reply.Content, nil
		}
		for _, tc := range reply.ToolCalls {
			obs := r.execTool(tc)
			msgs = append(msgs, provider.Message{Role: "assistant", Content: reply.Content, ToolCalls: []provider.ToolCall{tc}})
			msgs = append(msgs, provider.Message{Role: "tool", Content: obs, ToolCallID: tc.ID})

			sig := tc.Name + ":" + tc.Arguments
			same[sig]++
			if same[sig] > r.MaxSameAction {
				return "", fmt.Errorf("同一动作重复 %d 次(%s),判定原地打转,停止", r.MaxSameAction, sig)
			}
		}
	}
	return "", fmt.Errorf("达到最大步数 %d,停止", r.MaxSteps)
}

// execTool 执行工具并返回 Observation;失败/panic 喂回模型而非崩溃(错误自愈)。
func (r *Runtime) execTool(tc provider.ToolCall) (out string) {
	defer func() {
		if p := recover(); p != nil {
			out = fmt.Sprintf("工具 panic: %v", p)
		}
	}()
	var tool *provider.ToolDef
	for i := range r.Tools {
		if r.Tools[i].Name == tc.Name {
			tool = &r.Tools[i]
			break
		}
	}
	if tool == nil {
		return fmt.Sprintf("工具不存在: %s。可用: %s", tc.Name, toolNames(r.Tools))
	}
	var args map[string]any
	if tc.Arguments != "" {
		if err := json.Unmarshal([]byte(tc.Arguments), &args); err != nil {
			args = map[string]any{}
		}
	}
	return tool.Func(args)
}

func toolNames(tools []provider.ToolDef) []string {
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		names = append(names, t.Name)
	}
	return names
}
