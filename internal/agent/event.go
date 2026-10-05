// 结构化 Agent 事件流(M10 可观测性):runtime 在每个关键节点 emit 事件,
// 供 CLI 逐步渲染、未来 Supervisor 内省子 agent、日志/审计导出。OnStream 是它的
// 文本子集(EvText),保留兼容。

package agent

// EventKind 事件类型。
type EventKind int

const (
	EvText        EventKind = iota // 流式文本增量(SSE delta)
	EvStep                         // 每一轮循环开始(结构信号,携带步数)
	EvToolCall                     // 模型决策要执行工具
	EvToolResult                   // 工具执行返回(Observation 回填前)
	EvCompact                      // 上下文被压缩(M09 治理触发)
	EvDone                         // 正常完成(无工具调用,返回最终答案)
	EvStop                         // 因停止条件退出(MaxSteps/MaxSameAction/MaxTokens/Deadline)
	EvError                        // 不可恢复错误
	EvFinishGuard                  // 模型给出最终答复但任务未完成,注入续跑指令(compiler 落盘兜底)
)

// Event 一次 agent 生命周期事件。
type Event struct {
	Kind    EventKind
	Step    int    // 事件发生的步数(0 起)
	Text    string // EvText=增量文本;EvDone=最终答案
	Tool    string // EvToolCall/EvToolResult 工具名
	Args    string // EvToolCall 工具参数(JSON)
	Result  string // EvToolResult 工具返回(Observation,可能截断)
	Tokens  int    // 累计 token 消耗(真实 usage,缺省为估算)
	Dropped int    // EvCompact 折叠的消息条数
	Err     error  // EvStop/EvError 的原因
}

// EventHandler 事件回调。非并发安全:只在 runtime 主循环同步调用。
type EventHandler func(Event)

// emit 分发事件:有 OnEvent 走结构化;否则退化到 OnStream 文本兼容(仅 EvText)。
func (r *Runtime) emit(ev Event) {
	if r.OnEvent != nil {
		r.OnEvent(ev)
		return
	}
	if r.OnStream != nil && ev.Kind == EvText {
		r.OnStream(ev.Text)
	}
}
