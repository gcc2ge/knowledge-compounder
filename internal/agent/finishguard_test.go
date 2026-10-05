package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/gcc2ge/knowledge-compounder/internal/provider"
)

// scriptedProvider 按脚本顺序返回答复,记录每次 Chat 收到的消息。
type scriptedProvider struct {
	replies  []provider.Message
	i        int
	lastMsgs []provider.Message
}

func (p *scriptedProvider) Chat(_ context.Context, msgs []provider.Message, _ []provider.ToolDef, _ []string) (provider.Message, error) {
	p.lastMsgs = msgs
	r := p.replies[p.i]
	if p.i < len(p.replies)-1 {
		p.i++
	}
	return r, nil
}

func (p *scriptedProvider) Capabilities() provider.Capabilities { return provider.Capabilities{} }

// 弱模型放弃模式:连续给出「总结文本」而不写文件。FinishGuard 应注入续跑,
// 达到 MaxFinishPushes 上限后才正常返回最后一条答复。
func TestFinishGuardForcesPushesThenDone(t *testing.T) {
	fp := &scriptedProvider{replies: []provider.Message{
		{Role: "assistant", Content: "总结A(没写文件)"},
		{Role: "assistant", Content: "总结B(还是没写)"},
		{Role: "assistant", Content: "总结C"},
	}}
	rt := &Runtime{
		Provider:     fp,
		SystemPrompt: "你是编译器",
		MaxSteps:     10,
		FinishGuard: func(provider.Message) string {
			return "⚠️ 编译未完成:必须 write_file 落盘"
		},
		MaxFinishPushes: 2,
	}
	out, err := rt.Run(context.Background(), "编译任务", "")
	if err != nil {
		t.Fatal(err)
	}
	if out != "总结C" {
		t.Fatalf("应返回第 3 条答复(前两条被续跑拦截),得 %q", out)
	}
	// 3 次 Chat;最后一次收到的消息里应有 2 条注入的续跑指令
	if fp.i != 2 {
		t.Fatalf("应调用 3 次 Chat,得 %d 次", fp.i+1)
	}
	pushes := 0
	for _, m := range fp.lastMsgs {
		if m.Role == "user" && strings.Contains(m.Content, "必须 write_file") {
			pushes++
		}
	}
	if pushes != 2 {
		t.Fatalf("应注入 2 次续跑指令,得 %d 次", pushes)
	}
}

// 源页已落盘(FinishGuard 返回空)→ 不注入,直接正常返回。
func TestFinishGuardCompletesWhenNoGuard(t *testing.T) {
	fp := &scriptedProvider{replies: []provider.Message{
		{Role: "assistant", Content: "编译完成"},
	}}
	rt := &Runtime{
		Provider:        fp,
		SystemPrompt:    "你是编译器",
		MaxSteps:        10,
		FinishGuard:     func(provider.Message) string { return "" },
		MaxFinishPushes: 2,
	}
	out, err := rt.Run(context.Background(), "编译任务", "")
	if err != nil {
		t.Fatal(err)
	}
	if out != "编译完成" {
		t.Fatalf("守卫放行应直接返回,得 %q", out)
	}
	if fp.i != 0 {
		t.Fatalf("守卫放行应只调 1 次 Chat,得 %d 次", fp.i+1)
	}
}

// 未配置 FinishGuard(零值)→ 行为不变,直接返回。
func TestFinishGuardDisabledByDefault(t *testing.T) {
	fp := &scriptedProvider{replies: []provider.Message{
		{Role: "assistant", Content: "答案"},
	}}
	rt := &Runtime{
		Provider:     fp,
		SystemPrompt: "你是查询助手",
		MaxSteps:     10,
	}
	out, err := rt.Run(context.Background(), "问题", "")
	if err != nil {
		t.Fatal(err)
	}
	if out != "答案" || fp.i != 0 {
		t.Fatalf("零值守卫不应改变行为: out=%q calls=%d", out, fp.i+1)
	}
}
