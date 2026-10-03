// Package agent M04 Agent 运行时:Think-Act-Observe 循环 + 工具 + 停止条件。
// - 工具结果作为 Observation 回填(messages 追加式,记忆 = Messages)
// - 错误自愈:工具 panic → 观察文本喂回;LLM 调用失败 → MaxHeal 退避重试
// - 停止条件:MaxSteps + MaxSameAction + MaxTokens + Deadline(ctx)
// - checkpoint:StateFile 持久化 Messages,可中断/可恢复/可审计(M04 Store)
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/knowledge-compounder/kcp/internal/provider"
)

type Runtime struct {
	Provider      provider.Provider
	SystemPrompt  string
	Tools         []provider.ToolDef
	MaxSteps      int
	MaxSameAction int
	MaxTokens     int // 0 = 不限
	MaxHeal       int // 0 = 默认 1 次机会
	StateFile     string
}

// Run 执行一次 agent 任务:LLM 决策 → 执行工具 → 观察回填 → 再决策,直到无工具调用或达到停止条件。
// StateFile 存在时从中恢复消息(断点续跑);否则从 system+input 初始化。
func (r *Runtime) Run(ctx context.Context, input, state string) (string, error) {
	msgs := r.loadCheckpoint()
	if len(msgs) == 0 {
		msgs = []provider.Message{{Role: "system", Content: r.SystemPrompt}}
		if state != "" {
			msgs = append(msgs, provider.Message{Role: "user", Content: "[任务状态]\n" + state})
		}
		msgs = append(msgs, provider.Message{Role: "user", Content: input})
	}

	same := map[string]int{}
	for step := 0; step < r.MaxSteps; step++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if r.MaxTokens > 0 && tokensUsed(msgs) >= r.MaxTokens {
			return "", fmt.Errorf("达到 token 预算 %d,停止", r.MaxTokens)
		}
		reply, err := r.chatWithHeal(ctx, msgs)
		if err != nil {
			return "", err
		}
		if len(reply.ToolCalls) == 0 {
			_ = r.saveCheckpoint(msgs) // 保留最终对话供审计
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
		if err := r.saveCheckpoint(msgs); err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("达到最大步数 %d,停止", r.MaxSteps)
}

// chatWithHeal LLM 调用失败时退避重试(至多 MaxHeal 次),吞瞬态错误。
func (r *Runtime) chatWithHeal(ctx context.Context, msgs []provider.Message) (provider.Message, error) {
	heal := r.MaxHeal
	if heal <= 0 {
		heal = 1
	}
	reply, err := r.Provider.Chat(ctx, msgs, r.Tools, nil)
	for tries := 1; err != nil && tries <= heal; tries++ {
		select {
		case <-ctx.Done():
			return provider.Message{}, ctx.Err()
		case <-time.After(time.Duration(tries) * time.Second): // 线性退避
		}
		reply, err = r.Provider.Chat(ctx, msgs, r.Tools, nil)
	}
	return reply, err
}

// saveCheckpoint 持久化当前消息序列(可审计/可恢复)。
func (r *Runtime) saveCheckpoint(msgs []provider.Message) error {
	if r.StateFile == "" {
		return nil
	}
	b, err := json.Marshal(msgs)
	if err != nil {
		return err
	}
	return os.WriteFile(r.StateFile, b, 0o644)
}

// loadCheckpoint 从 StateFile 恢复消息序列;无文件或损坏返回空。
func (r *Runtime) loadCheckpoint() []provider.Message {
	if r.StateFile == "" {
		return nil
	}
	b, err := os.ReadFile(r.StateFile)
	if err != nil {
		return nil
	}
	var msgs []provider.Message
	if err := json.Unmarshal(b, &msgs); err != nil || len(msgs) == 0 {
		return nil
	}
	return msgs
}

// tokensUsed 粗略估算已用 token(≈字符数/4)。
func tokensUsed(msgs []provider.Message) int {
	n := 0
	for _, m := range msgs {
		n += len([]rune(m.Content))
		for _, tc := range m.ToolCalls {
			n += len([]rune(tc.Arguments))
		}
	}
	return n / 4
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
