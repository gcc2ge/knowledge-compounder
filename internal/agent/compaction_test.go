package agent

import (
	"testing"

	"github.com/knowledge-compounder/kcp/internal/provider"
)

// 构造一条带 tool_calls 的 assistant 消息与其 tool 结果,组成一轮。
func round(content string, tools int, toolContent string) []provider.Message {
	a := provider.Message{Role: "assistant", Content: content}
	for i := 0; i < tools; i++ {
		a.ToolCalls = append(a.ToolCalls, provider.ToolCall{ID: "t", Name: "search_wiki", Arguments: "{}"})
	}
	out := []provider.Message{a}
	for i := 0; i < tools; i++ {
		out = append(out, provider.Message{Role: "tool", Content: toolContent, ToolCallID: "t"})
	}
	return out
}

func msgs() []provider.Message {
	// system + 任务输入 + 3 轮(最后一轮纯答案,无工具)
	m := []provider.Message{
		{Role: "system", Content: "你是编译器"},
		{Role: "user", Content: "编译任务"},
	}
	m = append(m, round("搜索", 2, "结果A")...)
	m = append(m, round("搜索", 1, "结果B")...)
	m = append(m, provider.Message{Role: "assistant", Content: "最终答案"})
	return m
}

func TestLastNRounds(t *testing.T) {
	m := msgs()
	tail := lastNRounds(m, 2)
	// 最近 2 轮 = assistant(搜索) + tool + assistant(最终答案),共 3 条;前面的全裁剪
	if len(tail) != 3 {
		t.Fatalf("最近 2 轮应含 3 条(assistant+tool+assistant),得 %d 条: %#v", len(tail), tail)
	}
	if tail[0].Role != "assistant" || tail[1].Role != "tool" || tail[2].Role != "assistant" {
		t.Fatalf("轮切分破坏了 assistant/tool 配对: %#v", tail)
	}
}

func TestCompactMessages(t *testing.T) {
	m := msgs()
	compacted := compactMessages(m, 2)
	// system + 任务输入 + 占位 + 最近 2 轮(assistant+tool+assistant) = 6 条
	if len(compacted) != 6 {
		t.Fatalf("压缩后应有 system+输入+占位+2轮 = 6 条,得 %d 条", len(compacted))
	}
	if compacted[0].Role != "system" || compacted[1].Role != "user" {
		t.Fatalf("system/任务输入必须保留: %#v", compacted[:2])
	}
	if compacted[2].Content == "" || compacted[2].Role != "user" {
		t.Fatalf("占位消息缺失: %#v", compacted[2])
	}
	// 尾部完整保留最近两轮,不拆配对
	rest := compacted[3:]
	if rest[0].Role != "assistant" || rest[1].Role != "tool" || rest[2].Role != "assistant" {
		t.Fatalf("最近轮次被破坏: %#v", rest)
	}
}

func TestCompactMessagesNoopWhenFewRounds(t *testing.T) {
	m := msgs()
	compacted := compactMessages(m, 10) // 要留 10 轮,只有 3 轮,不动
	if len(compacted) != len(m) {
		t.Fatalf("不足可压轮次不应压缩: %d != %d", len(compacted), len(m))
	}
}

func TestMaybeCompactDisabled(t *testing.T) {
	r := &Runtime{Compaction: CompactionPolicy{Enabled: false}}
	m := msgs()
	next, ok := r.maybeCompact(m)
	if ok || len(next) != len(m) {
		t.Fatalf("关闭治理不应压缩: ok=%v len=%d", ok, len(next))
	}
}

// 带 write_file 轮的消息:写入动作「已发生」,不可重查,压缩时必须保留。
func msgsWithWrite() []provider.Message {
	m := []provider.Message{
		{Role: "system", Content: "你是编译器"},
		{Role: "user", Content: "编译任务"},
	}
	m = append(m, provider.Message{Role: "assistant", Content: "写入",
		ToolCalls: []provider.ToolCall{{ID: "w", Name: "write_file", Arguments: "{}"}}})
	m = append(m, provider.Message{Role: "tool", Content: "已写入 wiki/sources/X.md", ToolCallID: "w"})
	m = append(m, round("搜索", 1, "结果A")...)
	m = append(m, provider.Message{Role: "assistant", Content: "最终答案"})
	return m
}

func TestKeepWrites(t *testing.T) {
	m := msgsWithWrite()
	compacted := compactMessages(m, 1) // 只留最近 1 轮
	// system + 任务输入 + 占位 + write轮(2条) + 最近1轮(最终答案) = 6 条
	if len(compacted) != 6 {
		t.Fatalf("应保留 write 轮: 得 %d 条 = %#v", len(compacted), compacted)
	}
	if compacted[0].Role != "system" || compacted[1].Role != "user" {
		t.Fatalf("system/任务输入必须保留: %#v", compacted[:2])
	}
	// write 轮(免裁剪)紧随占位,且 assistant+tool 结果完整
	if compacted[3].Role != "assistant" || len(compacted[3].ToolCalls) == 0 || compacted[3].ToolCalls[0].Name != "write_file" {
		t.Fatalf("write_file 轮被裁剪: %#v", compacted[3])
	}
	if compacted[4].Role != "tool" || compacted[4].Content != "已写入 wiki/sources/X.md" {
		t.Fatalf("write 轮的 tool 结果被拆散: %#v", compacted[4])
	}
	// 最近 1 轮(最终答案)保留
	last := compacted[len(compacted)-1]
	if last.Role != "assistant" || last.Content != "最终答案" {
		t.Fatalf("最近一轮被裁剪: %#v", last)
	}
}
