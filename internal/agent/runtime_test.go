package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/gcc2ge/knowledge-compounder/internal/provider"
)

// 内容里带裸换行 + 未转义引号(weak 模型典型坏输出)→ 恢复出正确 path/content。
func TestRepairToolArgs_ContentNewlinesAndQuotes(t *testing.T) {
	raw := "{\"path\": \"wiki/sources/X.md\", \"content\": \"# 标题\n## 节\n他说\"重要\"。\"}"
	args := repairToolArgs(raw)
	if args == nil {
		t.Fatal("应能恢复参数")
	}
	if args["path"] != "wiki/sources/X.md" {
		t.Fatalf("path 应为 wiki/sources/X.md,得 %v", args["path"])
	}
	want := "# 标题\n## 节\n他说\"重要\"。"
	if args["content"] != want {
		t.Fatalf("content 应还原换行与引号,得 %q", args["content"])
	}
}

// 内容里出现 ", " 序列(如 列表: "A", "B")→ 不应被误判为键闭合。
func TestRepairToolArgs_ContentCommaQuotes(t *testing.T) {
	raw := "{\"path\": \"wiki/sources/Y.md\", \"content\": \"列表: \"A\", \"B\" 结束\"}"
	args := repairToolArgs(raw)
	if args == nil {
		t.Fatal("应能恢复参数")
	}
	if args["content"] != "列表: \"A\", \"B\" 结束" {
		t.Fatalf("content 应保留逗号+引号,得 %q", args["content"])
	}
}

// 内容里带内嵌 JSON({"name": "x"})→ 不应被误判为字段收尾。
func TestRepairToolArgs_ContentEmbeddedJSON(t *testing.T) {
	raw := "{\"path\": \"wiki/sources/Z.md\", \"content\": \"代码: {\"name\": \"x\"} 结束\"}"
	args := repairToolArgs(raw)
	if args == nil {
		t.Fatal("应能恢复参数")
	}
	if args["content"] != "代码: {\"name\": \"x\"} 结束" {
		t.Fatalf("content 应保留内嵌 JSON,得 %q", args["content"])
	}
}

// 多字段(短字段在前的通用形态)完整恢复。
func TestRepairToolArgs_MultiField(t *testing.T) {
	raw := "{\"slug\": \"背压\", \"source\": \"源A\", \"claim\": \"队列满时反压\", \"action\": \"corroborate\"}"
	args := repairToolArgs(raw)
	if args == nil {
		t.Fatal("应能恢复参数")
	}
	if args["slug"] != "背压" || args["action"] != "corroborate" {
		t.Fatalf("短字段应恢复,得 %v", args)
	}
}

// 模型截掉收尾 } 时自动补全。
func TestRepairToolArgs_MissingClosingBrace(t *testing.T) {
	raw := "{\"path\": \"wiki/sources/X.md\", \"content\": \"正文\""
	args := repairToolArgs(raw)
	if args == nil {
		t.Fatal("缺收尾 } 应被补全后恢复")
	}
	if args["content"] != "正文" {
		t.Fatalf("content 应恢复,得 %q", args["content"])
	}
}

// 完全不可恢复的垃圾 → 返回 nil(由工具拒绝兜底)。
func TestRepairToolArgs_Unrecoverable(t *testing.T) {
	if args := repairToolArgs("garbage not json at all"); args != nil {
		t.Fatalf("垃圾输入应返回 nil,得 %v", args)
	}
}

// 合法 JSON 直接通过(unmarshal 成功不触发 repair,函数本身也不破坏)。
func TestRepairToolArgs_ValidJSON(t *testing.T) {
	raw := `{"path": "wiki/sources/ok.md", "content": "line1\nline2"}`
	args := repairToolArgs(raw)
	if args == nil {
		t.Fatal("合法 JSON 不应被破坏")
	}
	if args["content"] != "line1\nline2" {
		t.Fatalf("content 应保留转义换行,得 %q", args["content"])
	}
}

// ---- write_file 定向兜底 ----

// 长 content 截断成未闭合字符串(模型输出被 token 截断)→ 整块取回。
func TestRecoverWriteFileArgs_TruncatedContent(t *testing.T) {
	raw := `{"path": "wiki/sources/X.md", "content": "## 论证链\n关键点没说"` // 无收尾引号
	args := recoverWriteFileArgs(raw)
	if args == nil {
		t.Fatal("应能恢复截断 content")
	}
	if args["path"] != "wiki/sources/X.md" {
		t.Fatalf("path 应恢复,得 %v", args["path"])
	}
	if args["content"] != "## 论证链\n关键点没说" {
		t.Fatalf("content 应整块取回,得 %q", args["content"])
	}
}

// content 含中途未转义引号 + 收尾闭合 → 取到最后一个未转义引号,中途引号保留。
func TestRecoverWriteFileArgs_StrayQuotes(t *testing.T) {
	raw := `{"path": "wiki/sources/Y.md", "content": "他说"重要"。"}`
	args := recoverWriteFileArgs(raw)
	if args == nil {
		t.Fatal("应能恢复")
	}
	if args["content"] != "他说\"重要\"。" {
		t.Fatalf("content 应含中途引号,得 %q", args["content"])
	}
}

// 正常闭合的 content 不丢尾部。
func TestRecoverWriteFileArgs_NormalClose(t *testing.T) {
	raw := `{"path": "wiki/sources/Z.md", "content": "正文\n带转义\"引号\"的结束"}`
	args := recoverWriteFileArgs(raw)
	if args == nil {
		t.Fatal("应能恢复")
	}
	if args["content"] != "正文\n带转义\"引号\"的结束" {
		t.Fatalf("content 应还原转义,得 %q", args["content"])
	}
}

// 没有 path 的畸形输入 → nil(不强行造 path)。
func TestRecoverWriteFileArgs_NoPath(t *testing.T) {
	if args := recoverWriteFileArgs(`{"content": "只有内容"}`); args != nil {
		t.Fatalf("缺 path 应返回 nil,得 %v", args)
	}
}

// content 截断到 EOF(无收尾引号)→ 标记 content_truncated,供工具警告模型。
func TestRecoverWriteFileArgs_TruncatedMarker(t *testing.T) {
	raw := `{"path": "wiki/sources/X.md", "content": "## 论证链` // 裸奔到 EOF
	args := recoverWriteFileArgs(raw)
	if args == nil {
		t.Fatal("应能恢复截断 content")
	}
	tr, _ := args["content_truncated"].(bool)
	if !tr {
		t.Fatal("截断输入应标记 content_truncated=true")
	}
}

// 正常闭合的 content → 无 content_truncated 标记。
func TestRecoverWriteFileArgs_NotTruncated(t *testing.T) {
	raw := `{"path": "wiki/sources/Y.md", "content": "正文完整"}`
	args := recoverWriteFileArgs(raw)
	if args == nil {
		t.Fatal("应能恢复")
	}
	if tr, _ := args["content_truncated"].(bool); tr {
		t.Fatal("正常闭合不应标记 content_truncated")
	}
}

// ---- write_file 历史压缩 ----

// 正常参数 → 只留 path + 省略标记(不再把整页 content 留在上下文)。
func TestCompactWriteArgs(t *testing.T) {
	out := compactWriteArgs(`{"path": "wiki/sources/X.md", "content": "一二三四五六"}`)
	if !strings.Contains(out, `"wiki/sources/X.md"`) {
		t.Fatalf("应保留 path,得 %q", out)
	}
	if strings.Contains(out, "一二三四五六") {
		t.Fatalf("content 不应原文保留,得 %q", out)
	}
	if !strings.Contains(out, "省略") {
		t.Fatalf("应有省略标记,得 %q", out)
	}
}

// 坏 JSON 也不留原文,退化到正则抓 path。
func TestCompactWriteArgs_BrokenJSON(t *testing.T) {
	out := compactWriteArgs(`{"path": "wiki/sources/Y.md", "content": "没说"完"}`)
	if !strings.Contains(out, `"wiki/sources/Y.md"`) {
		t.Fatalf("坏 JSON 应仍保留 path,得 %q", out)
	}
	if strings.Contains(out, "没说") {
		t.Fatalf("坏 JSON 的 content 也不应原文保留,得 %q", out)
	}
}

// 纯空对象 → 兜底标记,不 panic。
func TestCompactWriteArgs_Empty(t *testing.T) {
	if out := compactWriteArgs(`{}`); out == "" {
		t.Fatal("空对象应产出占位标记")
	}
}

// ---- 截断标记(edit_file 拒绝对已有页做半截修改)----

// 参数未以 } 收尾 = 模型输出截断 → execTool 应在 args 里标 truncated=true,工具层可拒绝。
func TestExecToolSetsTruncatedFlag(t *testing.T) {
	fp := &scriptedProvider{replies: []provider.Message{
		{ToolCalls: []provider.ToolCall{{ID: "t1", Name: "edit_file", Arguments: `{"path": "wiki/sources/X.md", "old_string": "尾", "new_string": "尾\n新块"`}}}, // 无收尾 } → 截断
		{Role: "assistant", Content: "完成"},
	}}
	var sawTruncated bool
	rt := &Runtime{
		Provider:     fp,
		SystemPrompt: "你是编译器",
		MaxSteps:     10,
		MaxSameAction: 100,
		Tools: []provider.ToolDef{{
			Name: "edit_file",
			Func: func(args map[string]any) string {
				sawTruncated, _ = args["truncated"].(bool)
				if sawTruncated {
					return "拒绝:参数疑似截断,未做修改"
				}
				return "OK"
			},
		}},
	}
	if _, err := rt.Run(context.Background(), "编译任务", ""); err != nil {
		t.Fatal(err)
	}
	if !sawTruncated {
		t.Fatal("截断的参数(无收尾 } )应标记 truncated=true")
	}
}

// 正常闭合的参数不误标截断。
func TestExecToolNoFalseTruncatedFlag(t *testing.T) {
	fp := &scriptedProvider{replies: []provider.Message{
		{ToolCalls: []provider.ToolCall{{ID: "t1", Name: "edit_file", Arguments: `{"path": "wiki/sources/X.md", "old_string": "尾", "new_string": "尾\n新块"}`}}},
		{Role: "assistant", Content: "完成"},
	}}
	var sawTruncated bool
	rt := &Runtime{
		Provider:     fp,
		SystemPrompt: "你是编译器",
		MaxSteps:     10,
		MaxSameAction: 100,
		Tools: []provider.ToolDef{{
			Name: "edit_file",
			Func: func(args map[string]any) string {
				sawTruncated, _ = args["truncated"].(bool)
				return "OK"
			},
		}},
	}
	if _, err := rt.Run(context.Background(), "编译任务", ""); err != nil {
		t.Fatal(err)
	}
	if sawTruncated {
		t.Fatal("正常闭合的参数不应标记 truncated=true")
	}
}

// 截断到「无收尾引号」且 repair 无法复原(nil)时,截断标记也必须设置——
// 此前标记只在 args!=nil 时打,repair 失败后工具收到空参数、报误导性「缺字段」,
// 弱模型同参重试烧步。断在 new_string 值中间是长内容截断的典型形态。
func TestExecToolTruncatedEvenWhenRepairFails(t *testing.T) {
	fp := &scriptedProvider{replies: []provider.Message{
		{ToolCalls: []provider.ToolCall{{ID: "t1", Name: "edit_file", Arguments: `{"path": "wiki/sources/X.md", "old_string": "尾", "new_string": "## 论证链\n关键内容被截断在这里`}}}, // 断在字符串中,无收尾引号也无 }
		{Role: "assistant", Content: "完成"},
	}}
	var sawTruncated bool
	rt := &Runtime{
		Provider:     fp,
		SystemPrompt: "你是编译器",
		MaxSteps:     10,
		MaxSameAction: 100,
		Tools: []provider.ToolDef{{
			Name: "edit_file",
			Func: func(args map[string]any) string {
				sawTruncated, _ = args["truncated"].(bool)
				if sawTruncated {
					return "拒绝:截断"
				}
				return "OK"
			},
		}},
	}
	if _, err := rt.Run(context.Background(), "编译任务", ""); err != nil {
		t.Fatal(err)
	}
	if !sawTruncated {
		t.Fatal("repair 失败(nil)的截断参数也应标记 truncated=true——否则工具误报缺字段")
	}
}

// ---- mid-run 覆盖守卫 ----

// MidRunGuard 在写页工具执行后把指引注入工具观察(EvToolResult 可见)——模型当场看到
// 「逐字补代码」,拦截「先写自造代码→收尾才被抓→被迫整页重写」的预算黑洞。
func TestMidRunGuardInjectsNote(t *testing.T) {
	fp := &scriptedProvider{replies: []provider.Message{
		{ToolCalls: []provider.ToolCall{{ID: "t1", Name: "write_file", Arguments: `{"path": "wiki/sources/X.md", "content": "页"}`}}},
		{Role: "assistant", Content: "完成"},
	}}
	var toolResult string
	rt := &Runtime{
		Provider:      fp,
		SystemPrompt:  "你是编译器",
		MaxSteps:      10,
		MaxSameAction: 100,
		Tools: []provider.ToolDef{{
			Name: "write_file",
			Func: func(args map[string]any) string { return "已写入" },
		}},
		OnEvent: func(ev Event) {
			if ev.Kind == EvToolResult {
				toolResult = ev.Result
			}
		},
		MidRunGuard: func(step int, tool, args string) string {
			if tool == "write_file" && strings.Contains(args, "wiki/sources/X.md") {
				return "⚠️ 缺失 10 项硬资产,逐字补代码"
			}
			return ""
		},
	}
	if _, err := rt.Run(context.Background(), "编译任务", ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(toolResult, "逐字补代码") {
		t.Fatalf("MidRunGuard 指引应并入工具观察,得 %q", toolResult)
	}
	if !strings.Contains(toolResult, "已写入") {
		t.Fatalf("工具原始结果应保留,得 %q", toolResult)
	}
}

// ---- 原地打转检测(actionKey)----

// write_file 同 path 不同 content = 反复重写同一文件 = 打转,签名应一致;
// 不同 path 不同签名;其余工具保留完整参数(查询/对象即语义)。
func TestActionKey_WriteFilePathBased(t *testing.T) {
	a := actionKey("write_file", `{"path": "wiki/sources/X.md", "content": "版本一"}`)
	b := actionKey("write_file", `{"path": "wiki/sources/X.md", "content": "版本二"}`)
	if a != b {
		t.Fatalf("同 path 不同 content 应同签名,得 %q vs %q", a, b)
	}
	if !strings.Contains(a, "wiki/sources/X.md") || !strings.HasPrefix(a, "write_file:") {
		t.Fatalf("签名应含工具名与 path,得 %q", a)
	}
	c := actionKey("write_file", `{"path": "wiki/sources/Y.md", "content": "版本一"}`)
	if c == a {
		t.Fatal("不同 path 应不同签名")
	}
	// 其他工具:参数即语义,保留完整参数
	if actionKey("search_wiki", `{"query":"agent"}`) == actionKey("search_wiki", `{"query":"rag"}`) {
		t.Fatal("search_wiki 不同 query 应不同签名")
	}
	if actionKey("get_page", `{"page":"A"}`) == actionKey("get_page", `{"page":"B"}`) {
		t.Fatal("get_page 不同页应不同签名")
	}
}

// 集成:4 次 write_file 同 path 不同 content,MaxSameAction=3 → 第 4 次触发打转。
// 旧签名含 content 会漏判(4 个不同 sig),path 签名在第 4 次即停。
func TestSameActionDetectsWriteFileLoop(t *testing.T) {
	fp := &scriptedProvider{replies: []provider.Message{
		{ToolCalls: []provider.ToolCall{{ID: "w1", Name: "write_file", Arguments: `{"path": "wiki/sources/X.md", "content": "一"}`}}},
		{ToolCalls: []provider.ToolCall{{ID: "w2", Name: "write_file", Arguments: `{"path": "wiki/sources/X.md", "content": "二"}`}}},
		{ToolCalls: []provider.ToolCall{{ID: "w3", Name: "write_file", Arguments: `{"path": "wiki/sources/X.md", "content": "三"}`}}},
		{ToolCalls: []provider.ToolCall{{ID: "w4", Name: "write_file", Arguments: `{"path": "wiki/sources/X.md", "content": "四"}`}}},
	}}
	rt := &Runtime{
		Provider:      fp,
		SystemPrompt:  "你是编译器",
		MaxSteps:      10,
		MaxSameAction: 3,
		Tools: []provider.ToolDef{{Name: "write_file",
			Func: func(map[string]any) string { return "已写入" }}},
	}
	_, err := rt.Run(context.Background(), "编译任务", "")
	if err == nil {
		t.Fatal("同 path 反复 write_file 应触发原地打转停止")
	}
	if fp.i != 3 {
		t.Fatalf("应在第 4 次 write_file 停止(同 path 计数=4>3),得调用 %d 次", fp.i+1)
	}
}
