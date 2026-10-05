// Package agent M04 Agent 运行时:Think-Act-Observe 循环 + 工具 + 停止条件。
// - 工具结果作为 Observation 回填(messages 追加式,记忆 = Messages)
// - 错误自愈:工具 panic → 观察文本喂回;LLM 调用失败 → MaxHeal 退避重试
// - 停止条件:MaxSteps + MaxSameAction + MaxTokens + Deadline(ctx)
// - 上下文治理(M09):Compaction 超预算折叠早期历史(见 compaction.go)
// - 可观测(M10):OnEvent 结构化事件流(见 event.go);OnStream 是文本子集
// - checkpoint:StateFile 持久化 Messages,可中断/可恢复/可审计(M04 Store)
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"
	"unicode"

	"github.com/gcc2ge/knowledge-compounder/internal/provider"
)

type Runtime struct {
	Provider        provider.Provider
	SystemPrompt    string
	Tools           []provider.ToolDef
	MaxSteps        int
	MaxSameAction   int
	MaxTokens       int // 0 = 不限(预算:真实 usage 优先,缺省估算)
	MaxHeal         int // 0 = 默认 1 次机会
	StateFile       string
	OnStream        func(string)     // SSE 增量文本回调(兼容;OnEvent 非 nil 时忽略)
	OnEvent         EventHandler     // 结构化事件流(可观测,M10);非 nil 优先于 OnStream
	Compaction      CompactionPolicy // 上下文治理(M09);零值 = 关闭
	CheckpointEvery int              // 写 checkpoint 的步频;0 = 默认每 3 步

	// FinishGuard:可选。模型给出最终答复(无工具调用)时,若返回非空消息,视为任务未完成,
	// 注入该消息强制续跑(compiler 落盘兜底:防弱模型「读而不写」直接退出,glm 实测连读 13 页零落盘)。
	// MaxFinishPushes 限制续跑次数(0 = 关闭)。
	FinishGuard     func(reply provider.Message) string
	MaxFinishPushes int

	// 运行时内部状态(非并发,单循环)
	usage        *provider.Usage // 累计真实 token 消耗
	finishPushes int             // 已注入的续跑次数
}

// Run 执行一次 agent 任务:LLM 决策 → 执行工具 → 观察回填 → 再决策,直到无工具调用或达到停止条件。
// StateFile 存在时从中恢复消息(断点续跑);否则从 system+input 初始化。
func (r *Runtime) Run(ctx context.Context, input, state string) (string, error) {
	// 断点续跑:恢复消息与累计 usage(MaxTokens 预算不归零)
	msgs, savedUsage := r.loadCheckpoint()
	r.usage = savedUsage
	if len(msgs) == 0 {
		r.usage = nil
		msgs = []provider.Message{{Role: "system", Content: r.SystemPrompt}}
		if state != "" {
			msgs = append(msgs, provider.Message{Role: "user", Content: "[任务状态]\n" + state})
		}
		msgs = append(msgs, provider.Message{Role: "user", Content: input})
	}

	every := r.CheckpointEvery
	if every <= 0 {
		every = 3
	}
	same := map[string]int{}
	for step := 0; step < r.MaxSteps; step++ {
		if err := ctx.Err(); err != nil {
			r.emit(Event{Kind: EvStop, Step: step, Err: err})
			return "", err
		}
		// M09 上下文治理:callProvider 前先压缩,避免超预算请求真的发出(压缩后必写 checkpoint)
		if next, ok := r.maybeCompact(msgs); ok {
			r.emit(Event{Kind: EvCompact, Step: step, Dropped: len(msgs) - len(next)})
			msgs = next
			_ = r.saveCheckpoint(msgs)
		}
		if r.MaxTokens > 0 && r.consumedTokens(msgs) >= r.MaxTokens {
			err := fmt.Errorf("达到 token 预算 %d(累计已用 %d),停止", r.MaxTokens, r.consumedTokens(msgs))
			r.emit(Event{Kind: EvStop, Step: step, Tokens: r.consumedTokens(msgs), Err: err})
			return "", err
		}
		r.emit(Event{Kind: EvStep, Step: step, Tokens: r.consumedTokens(msgs)})
		reply, err := r.callProvider(ctx, msgs)
		if err != nil {
			r.emit(Event{Kind: EvError, Step: step, Err: err})
			return "", err
		}
		r.accumulateUsage(reply)
		if len(reply.ToolCalls) == 0 {
			// 落盘兜底:最终答复但任务未完成 → 注入续跑指令,堵住「总结文本代替写文件」的退出路径。
			if r.FinishGuard != nil && r.finishPushes < r.MaxFinishPushes {
				if cont := r.FinishGuard(reply); cont != "" {
					r.finishPushes++
					r.emit(Event{Kind: EvFinishGuard, Step: step, Text: trunc(cont, 100)})
					msgs = append(msgs, provider.Message{Role: "assistant", Content: reply.Content})
					msgs = append(msgs, provider.Message{Role: "user", Content: cont})
					continue
				}
			}
			_ = r.saveCheckpoint(msgs) // 保留最终对话供审计
			r.emit(Event{Kind: EvDone, Step: step, Text: reply.Content, Tokens: r.consumedTokens(msgs)})
			return reply.Content, nil
		}
		for _, tc := range reply.ToolCalls {
			r.emit(Event{Kind: EvToolCall, Step: step, Tool: tc.Name, Args: tc.Arguments})
			obs := r.execTool(tc)
			r.emit(Event{Kind: EvToolResult, Step: step, Tool: tc.Name, Result: trunc(obs, 300)})
			msgs = append(msgs, provider.Message{Role: "assistant", Content: reply.Content, ToolCalls: []provider.ToolCall{tc}})
			msgs = append(msgs, provider.Message{Role: "tool", Content: obs, ToolCallID: tc.ID})

			sig := tc.Name + ":" + tc.Arguments
			same[sig]++
			if same[sig] > r.MaxSameAction {
				err := fmt.Errorf("同一动作重复 %d 次(%s),判定原地打转,停止", r.MaxSameAction, sig)
				r.emit(Event{Kind: EvStop, Step: step, Err: err})
				return "", err
			}
		}
		// checkpoint 降频写,避免每步全量序列化(O(n²) 写放大);压缩已在循环顶部闭环时写
		if step%every == every-1 {
			_ = r.saveCheckpoint(msgs)
		}
	}
	err := fmt.Errorf("达到最大步数 %d,停止", r.MaxSteps)
	r.emit(Event{Kind: EvStop, Step: r.MaxSteps, Err: err})
	return "", err
}

// callProvider 调用 LLM:Provider 实现 Streamer 且订阅了流式时走 SSE;
// 失败时退避重试(至多 MaxHeal 次)——流式与非流式统一重试,吞瞬态错误(M04 错误三分类)。
func (r *Runtime) callProvider(ctx context.Context, msgs []provider.Message) (provider.Message, error) {
	heal := r.MaxHeal
	if heal <= 0 {
		heal = 1
	}
	call := func() (provider.Message, error) {
		if r.OnEvent != nil || r.OnStream != nil {
			if st, ok := r.Provider.(provider.Streamer); ok {
				onDelta := func(t string) { r.emit(Event{Kind: EvText, Text: t}) }
				return st.Stream(ctx, msgs, r.Tools, nil, onDelta)
			}
		}
		return r.Provider.Chat(ctx, msgs, r.Tools, nil)
	}
	reply, err := call()
	for tries := 1; err != nil && tries <= heal; tries++ {
		select {
		case <-ctx.Done():
			return provider.Message{}, ctx.Err()
		case <-time.After(time.Duration(tries) * time.Second): // 线性退避
		}
		reply, err = call()
	}
	return reply, err
}

// accumulateUsage 累计真实 token 消耗(provider 未返回 usage 时为 nil,走估算)。
func (r *Runtime) accumulateUsage(reply provider.Message) {
	if reply.Usage == nil {
		return
	}
	if r.usage == nil {
		r.usage = reply.Usage
		return
	}
	r.usage.InputTokens += reply.Usage.InputTokens
	r.usage.OutputTokens += reply.Usage.OutputTokens
	r.usage.TotalTokens += reply.Usage.TotalTokens
}

// consumedTokens 预算闸门口径:当前请求体估算 + 累计真实消耗。
//   - 累计 usage:真实输出/输入总消耗(provider 已解析)
//   - estimateTokens:当前 msgs 即将发送的输入估算——补上「第一轮超长输入」,
//     让 MaxTokens 在累计 usage 为 0 时也能拦住超预算的首次请求
func (r *Runtime) consumedTokens(msgs []provider.Message) int {
	total := 0
	if r.usage != nil && r.usage.TotalTokens > 0 {
		total = r.usage.TotalTokens
	}
	return total + estimateTokens(msgs)
}

// checkpoint 持久化结构:Messages + 累计 usage(M04 Store,断点续跑预算不归零)。
// 兼容旧版纯 Messages 数组(loadCheckpoint 回退解析)。
type checkpoint struct {
	Messages []provider.Message `json:"messages"`
	Usage    *provider.Usage    `json:"usage,omitempty"`
}

// saveCheckpoint 持久化当前消息序列与累计消耗(可审计/可恢复)。
func (r *Runtime) saveCheckpoint(msgs []provider.Message) error {
	if r.StateFile == "" {
		return nil
	}
	b, err := json.Marshal(checkpoint{Messages: msgs, Usage: r.usage})
	if err != nil {
		return err
	}
	return os.WriteFile(r.StateFile, b, 0o644)
}

// loadCheckpoint 恢复消息序列与累计 usage;无文件/损坏/空返回 nil。
func (r *Runtime) loadCheckpoint() ([]provider.Message, *provider.Usage) {
	if r.StateFile == "" {
		return nil, nil
	}
	b, err := os.ReadFile(r.StateFile)
	if err != nil {
		return nil, nil
	}
	var cp checkpoint
	if err := json.Unmarshal(b, &cp); err == nil && len(cp.Messages) > 0 {
		return cp.Messages, cp.Usage
	}
	var msgs []provider.Message // 旧版纯数组
	if err := json.Unmarshal(b, &msgs); err == nil && len(msgs) > 0 {
		return msgs, nil
	}
	return nil, nil
}

// estimateTokens 粗略估算消息序列 token 量,中文感知:
// CJK ≈ 1 token/字(BPE 实测 1.0–1.5 字/token),其余 ≈ 4 字符/token。
// 旧版统一 /4 对中文低估 2.5 倍+,长中文源读入后压缩永不触发——实测 438KB 源毒化上下文。
// 宁可高估:压缩与预算闸门偏保守是安全方向。
func estimateTokens(msgs []provider.Message) int {
	n := 0
	for _, m := range msgs {
		n += textTokens(m.Content)
		for _, tc := range m.ToolCalls {
			n += textTokens(tc.Arguments)
		}
	}
	return n
}

func textTokens(s string) int {
	cjk, other := 0, 0
	for _, r := range s {
		if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) {
			cjk++
		} else {
			other++
		}
	}
	return cjk + other/4
}

// trunc 截断长文本(工具结果/参数渲染),避免事件刷屏。
func trunc(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
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
