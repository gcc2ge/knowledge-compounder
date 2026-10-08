// M09 上下文治理:Messages 是无限增长的内存,长任务历史逐轮重放。
// maybeCompact 在估算 token 超过预算时把早期轮次折叠为一条占位消息,
// 保留 system + 任务输入 + 最近 KeepRounds 轮,控制请求体平方增长。

package agent

import (
	"fmt"

	"github.com/gcc2ge/knowledge-compounder/internal/provider"
)

// CompactionPolicy 压缩策略。零值 = 关闭(行为与旧版一致)。
type CompactionPolicy struct {
	Enabled    bool // 是否启用治理
	MaxTokens  int  // 估算 token 超此值触发压缩;0 = 默认 80_000
	KeepRounds int  // 保留最近几轮(assistant+tool 成对);0 = 默认 8
}

const defaultCompactTokens = 80_000
const defaultKeepRounds = 8

// maybeCompact 超预算时压缩 msgs,返回压缩后的消息与是否发生了压缩。
// 触发口径:基于「当前消息序列估算」(即即将发送的请求体),与 MaxTokens 的累计闸门
// (runtime.consumedTokens)互补——Compaction 控单次请求体大小,MaxTokens 控总消耗。
// 压缩以「轮」为单位:一轮 = 一条 assistant(含 tool_calls)+ 紧随其后的 tool 结果,
// 绝不把 assistant 与其 Observation 拆开(OpenAI/Anthropic 协议要求成对)。
func (r *Runtime) maybeCompact(msgs []provider.Message) ([]provider.Message, bool) {
	if !r.Compaction.Enabled {
		return msgs, false
	}
	budget := r.Compaction.MaxTokens
	if budget <= 0 {
		budget = defaultCompactTokens
	}
	if estimateTokens(msgs) < budget {
		return msgs, false
	}
	keep := r.Compaction.KeepRounds
	if keep <= 0 {
		keep = defaultKeepRounds
	}
	compact := compactMessages(msgs, keep)
	if len(compact) == len(msgs) {
		return msgs, false
	}
	return compact, true
}

// compactMessages 折叠早期历史。保留:
//  1. system 指令(模型身份,必保)
//  2. 任务输入(第一条 user,SCHEMA/任务说明,压了任务就丢了)
//  3. 最近 KeepRounds 轮(assistant+tool)
//  4. 含 write_file 调用的轮(写入结果是「已发生的动作」,不可重查,免裁剪)
//
// 早期折叠为一条占位 user 消息,明示模型历史被截断、事实需重新查询。
func compactMessages(msgs []provider.Message, keepRounds int) []provider.Message {
	var sys []provider.Message
	i := 0
	for i < len(msgs) && msgs[i].Role == "system" {
		sys = append(sys, msgs[i])
		i++
	}
	// 任务输入 = system 之后第一个 user
	head := sys
	if i < len(msgs) && msgs[i].Role == "user" {
		head = append(head, msgs[i])
		i++
	}
	rest := msgs[i:]
	tail := keepRoundsAndWrites(rest, keepRounds)
	if len(tail) >= len(rest) {
		return msgs // 不足可压轮次,不动
	}
	dropped := len(rest) - len(tail)
	note := provider.Message{
		Role: "user",
		Content: fmt.Sprintf("[上下文治理] 早期 %d 条消息已折叠为历史摘要(节省 %dk tokens)。"+
			"如需早期工具结果或页面内容,请重新调用检索/读取工具获取,不要依赖记忆。若此前执行过 write_file,"+
			"已写入的文件不在本上下文中,请读取对应文件确认当前状态。", dropped, estimateTokens(rest)-estimateTokens(tail)),
	}
	out := make([]provider.Message, 0, len(head)+1+len(tail))
	out = append(out, head...)
	out = append(out, note)
	out = append(out, tail...)
	return out
}

// keepRoundsAndWrites 保留最近 n 轮 + 所有含 write_file 的轮(不可重查动作,免裁剪)。
// 返回的消息保持原顺序;不足 n 轮且无 write 轮时返回全部。
func keepRoundsAndWrites(msgs []provider.Message, n int) []provider.Message {
	// 按轮切分:一轮 = assistant(含 tool_calls) + 其后的 tool Observation 们。
	// 切分边界:遇到新 assistant 时,先把之前累积的 tool 残块封给上一轮,
	// 否则 tool 会错误地并入下一轮(assistant 的 Observation 被拆散)。
	var rounds [][]provider.Message
	var cur []provider.Message
	for _, m := range msgs {
		if m.Role == "assistant" && len(cur) > 0 {
			rounds = append(rounds, cur)
			cur = nil
		}
		cur = append(cur, m)
	}
	if len(cur) > 0 {
		rounds = append(rounds, cur) // 尾部残块(非 assistant 结尾,如占位 user)
	}
	keep := make([]bool, len(rounds))
	cnt := 0
	for i := len(rounds) - 1; i >= 0 && cnt < n; i-- {
		keep[i] = true
		cnt++
	}
	for i, rd := range rounds {
		if !keep[i] && hasWrite(rd) {
			keep[i] = true
		}
	}
	var out []provider.Message
	for i, rd := range rounds {
		if keep[i] {
			out = append(out, rd...)
		}
	}
	return out
}

// hasWrite 判断一轮中是否有 write_file 调用。
func hasWrite(round []provider.Message) bool {
	for _, m := range round {
		for _, tc := range m.ToolCalls {
			if tc.Name == "write_file" {
				return true
			}
		}
	}
	return false
}
