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
	"regexp"
	"strings"
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

	// NoResume:true 时忽略磁盘 checkpoint,从空白历史开始且不再写 checkpoint。
	// 用于 relink/repair 这类「幂等、有界」的补强/修复会话——旧 checkpoint 里装着上次
	// 撞循环/卡死的对话,续跑会把模型重新拖进同一循环(get_page/read_file 大源页截断
	// →同参重试→原地打转,实测)。会话重启的收益 < 毒化历史的风险,干脆无状态。
	// compile 保留续跑(长任务、单轮可能撞预算,断点续跑收益大)。
	NoResume bool

	// FinishGuard:可选。模型给出最终答复(无工具调用)时,若返回非空消息,视为任务未完成,
	// 注入该消息强制续跑(compiler 落盘兜底:防弱模型「读而不写」直接退出,glm 实测连读 13 页零落盘)。
	// MaxFinishPushes 限制续跑次数(0 = 关闭)。
	FinishGuard     func(reply provider.Message) string
	MaxFinishPushes int

	// MidRunGuard 每步工具执行后校验(compile 用):写页工具调用后追加一次 preflight 检查,
	// 巨量硬资产缺失时把指引注入该工具观察——当场纠正「自造/近似代码」,避免收尾才抓、被迫整页重写(M11 实证)。
	MidRunGuard func(step int, tool, args string) string

	// ToolCalls 本轮实际执行的工具调用数(cli 编译收尾判据:区分「模型真实参与但幂等跳写源页」
	// 与「零工作假成功」——零工作 = 0 次工具调用;幂等重编译 = ≥1 次调用但源页已最新无需重写)。
	ToolCalls int

	// 运行时内部状态(非并发,单循环)
	usage        *provider.Usage // 累计真实 token 消耗
	finishPushes int             // 已注入的续跑次数
}

// Run 执行一次 agent 任务:LLM 决策 → 执行工具 → 观察回填 → 再决策,直到无工具调用或达到停止条件。
// StateFile 存在时从中恢复消息(断点续跑);否则从 system+input 初始化。
func (r *Runtime) Run(ctx context.Context, input, state string) (string, error) {
	// 断点续跑:恢复消息与累计 usage(MaxTokens 预算不归零)。
	// NoResume 角色(relink/repair)不续跑——旧 checkpoint 是毒化历史,从空白开始。
	var msgs []provider.Message
	var savedUsage *provider.Usage
	if !r.NoResume {
		msgs, savedUsage = r.loadCheckpoint()
	}
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
	step, budget := 0, r.MaxSteps
	for {
		if step >= budget {
			// 步数用尽:MaxSteps 是预算不是完成标志——中途撞上限时任务可能只完成一半
			// (源页半成品/未跑核对)。给 FinishGuard 一个判断机会,未达标则注入续跑
			// 指令并延长预算(最多 MaxFinishPushes 次),而不是带半成品死掉。
			// 边界裁决:预算耗尽 ≠ 失败——FinishGuard 通过(页面按闸门定义已完成)时
			// 正常收尾;否则 push 未满则续跑、push 已满则如实失败。
			if r.FinishGuard != nil {
				cont := r.FinishGuard(provider.Message{})
				if cont == "" {
					// 闸门判定任务已完成:预算边界完成与正常收尾同判成功,
					// 交由 cli 的 postCompileQA 独立复核,不在此误杀。
					_ = r.saveCheckpoint(msgs)
					r.emit(Event{Kind: EvDone, Step: step})
					return "", nil
				}
				if r.finishPushes < r.MaxFinishPushes {
					r.finishPushes++
					r.emit(Event{Kind: EvFinishGuard, Step: step, Text: trunc(cont, 100)})
					msgs = append(msgs, provider.Message{Role: "user", Content: cont})
					budget += r.MaxSteps // 每次续跑再给一整份预算
					continue
				}
				err := fmt.Errorf("达到最大步数 %d,停止: 任务未通过闸门(续跑 %d 次仍不合格)", r.MaxSteps, r.finishPushes)
				r.emit(Event{Kind: EvStop, Step: step, Err: err})
				return "", err
			}
			err := fmt.Errorf("达到最大步数 %d,停止", r.MaxSteps)
			r.emit(Event{Kind: EvStop, Step: step, Err: err})
			return "", err
		}
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
			r.ToolCalls++ // 真实执行计数器:编译收尾区分幂等跳写与零工作假成功
			obs := r.execTool(tc)
			if r.MidRunGuard != nil {
				if note := r.MidRunGuard(step, tc.Name, tc.Arguments); note != "" {
					obs = obs + "\n\n" + note // 指引并入工具观察,模型当场可见
				}
			}
			r.emit(Event{Kind: EvToolResult, Step: step, Tool: tc.Name, Result: trunc(obs, 300)})
			// 历史里 write_file 的 arguments 压缩成占位:整页 content 是上下文最大消费源,
			// 而分块写页的每轮全文若不压缩会永久留在上下文(compaction 保留所有 write 轮)——
			// 上下文钉在阈值上、折叠读轮、模型忘 raw、重读、再填满的履带(实测 51 次压缩/文件)。
			histTC := tc
			if tc.Name == "write_file" {
				histTC.Arguments = compactWriteArgs(tc.Arguments)
			}
			msgs = append(msgs, provider.Message{Role: "assistant", Content: reply.Content, ToolCalls: []provider.ToolCall{histTC}})
			msgs = append(msgs, provider.Message{Role: "tool", Content: obs, ToolCallID: tc.ID})

			sig := actionKey(tc.Name, tc.Arguments)
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
		step++
	}
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
	if r.StateFile == "" || r.NoResume {
		return nil // NoResume 角色:不写 checkpoint,避免残留文件误导后续会话
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
			// 容错恢复:weak 模型(deepseek/glm 系)常把大 content 里的引号/换行写成
			// 非法 JSON,严格解析失败若直接丢参数,write_file 只收到空 path、模型反复
			// 重试烧光步数(实测 M01 编译 15+ 步全卡在 write_file 拒绝上,最终撞 Deadline)。
			args = repairToolArgs(tc.Arguments)
		}
		if args == nil && tool.Name == "write_file" {
			// 再兜底:长 content 常被模型输出截断成未闭合字符串(转义修复救不了),
			// 定向恢复 path(短字段写得好)+ content(整块取到末尾未转义引号)。
			args = recoverWriteFileArgs(tc.Arguments)
		}
		// 截断特征:JSON 对象未以 } 收尾 = 参数被模型输出中途切断。
		// 此前放在 args!=nil 分支内——但 repair 对「截断到无收尾引号」返回 nil(修复后仍
		// 未闭合),标记被跳过,工具收到空参数、给出误导性「缺字段」拒绝,弱模型同参重试烧步。
		// 与解析成败解耦:裸参数不以 } 收尾一律标 truncated,由工具按截断语义拒绝(write_file
		// 走 recoverWriteFileArgs 的 content_truncated 既有路径;edit_file 必须拒绝——new_string
		// 只替换一半会静默破坏已有页,比整页覆盖更危险)。
		if strings.TrimSpace(tc.Arguments) != "" && !strings.HasSuffix(strings.TrimSpace(tc.Arguments), "}") {
			if args == nil {
				args = map[string]any{}
			}
			args["truncated"] = true
		}
	}
	if args == nil {
		args = map[string]any{}
	}
	return tool.Func(args)
}

// recoverWriteFileArgs write_file 定向兜底。write_file 只认 path+content 两参:
//   - path 用严格正则(短字段,模型写得好)
//   - content 取 "content": 开引号之后到「最后一个未转义引号」的整块(含中途未转义引号;
//     模型输出截断导致无收尾引号时取到 EOF),再还原 \n / \" / \\。
func recoverWriteFileArgs(raw string) map[string]any {
	args := map[string]any{}
	if m := regexp.MustCompile(`"path"\s*:\s*"([^"]*)"`).FindStringSubmatch(raw); m != nil {
		args["path"] = m[1]
	} else {
		return nil
	}
	idx := regexp.MustCompile(`"content"\s*:`).FindStringIndex(raw)
	if idx == nil {
		return nil
	}
	rest := strings.TrimLeft(raw[idx[1]:], " \t\r\n")
	if rest == "" || rest[0] != '"' {
		return nil
	}
	start := len(raw) - len(rest) + 1 // 值开引号之后
	last := -1
	for i := start; i < len(raw); i++ {
		if raw[i] == '"' && (i == 0 || raw[i-1] != '\\') {
			last = i
		}
	}
	end := len(raw)
	if last > start {
		end = last
	} else {
		// 无收尾未转义引号 = arguments 被模型输出截断(content 一直裸奔到 EOF)。
		// 标记给 write_file 工具,让它警告模型内容可能不完整、需分块写。
		args["content_truncated"] = true
	}
	args["content"] = unescapeJSONString(raw[start:end])
	return args
}

// unescapeJSONString 还原 JSON 字符串转义:\n \t \r \" \\;未知转义取其原字符。
func unescapeJSONString(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			case '"':
				b.WriteByte('"')
			case '\\':
				b.WriteByte('\\')
			default:
				b.WriteByte(s[i+1])
			}
			i++
		} else {
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// compactWriteArgs 把 write_file 的 arguments 压成占位再存入历史。write_file 的整页
// content 是上下文最大消费源,而分块写页的每轮全文若不压缩会永久留在上下文(compaction
// 保留所有 write 轮)→ 上下文钉在阈值上、折叠读轮、模型忘 raw、重读、再填满的履带。
// 占位必须明确标注「系统压缩」——否则模型读自己历史以为只写了占位符,会反复重写测试
// (实测模型把 [省略] 当自己写的内容,step 内连续 read/write 测试循环)。
func compactWriteArgs(arguments string) string {
	var args map[string]any
	if err := json.Unmarshal([]byte(arguments), &args); err == nil {
		if p, ok := args["path"].(string); ok {
			if c, ok := args["content"].(string); ok {
				return fmt.Sprintf(`{"path": %q, "content": "[系统已压缩省略 %d 字符——你实际写入的是完整内容,如需查看请 read_file 读回文件当前状态]"}`, p, len([]rune(c)))
			}
			return fmt.Sprintf(`{"path": %q}`, p)
		}
	}
	if m := regexp.MustCompile(`"path"\s*:\s*"([^"]*)"`).FindStringSubmatch(arguments); m != nil {
		return fmt.Sprintf(`{"path": %q, "content": "[系统已压缩省略]"}`, m[1])
	}
	return `{"content": "[系统已压缩省略]"}`
}

// repairToolArgs 容错解析模型输出的工具参数 JSON。适用形态:扁平对象、短字段在前、
// 最后一个大字符串字段(如 write_file 的 content)在后。weak 模型常在这类字段里写
// 未转义的 " 与裸换行,使严格 json.Unmarshal 失败。本函数逐字符扫描修复:
//   - 字符串内的裸换行/回车/Tab → 转义为 \n \r \t
//   - 字符串内未转义的 " → 依据后随字符判定「真实终结符 vs 内容引号」:
//     后随 , } ] 或 EOF 且为「键闭合」语境 → 终结;后随内容字符 → 转义为 \"
//   - 键语境:短字段(键)的收尾 " 后随 : → 终结(键闭合)
//
// 修复后重新解析;仍失败返回 nil(由工具拒绝兜底,错误信息更清晰)。
func repairToolArgs(raw string) map[string]any {
	rs := []rune(raw)
	var b strings.Builder
	inStr := false
	keyCtx := false // 下一个字符串是否为对象键(由前一个结构字符决定)
	for i := 0; i < len(rs); i++ {
		c := rs[i]
		if !inStr {
			b.WriteRune(c)
			switch c {
			case '{', ',':
				keyCtx = true
			case ':':
				keyCtx = false
			case '"':
				inStr = true
			}
			continue
		}
		switch c {
		case '\\': // 已转义:原样保留并跳过后一字符
			b.WriteRune(c)
			if i+1 < len(rs) {
				b.WriteRune(rs[i+1])
				i++
			}
		case '"':
			j := i + 1
			for j < len(rs) && isJSONWS(rs[j]) {
				j++
			}
			switch {
			case j >= len(rs):
				b.WriteRune('"')
				inStr = false
				keyCtx = false
			case rs[j] == '}' || rs[j] == ']':
				// 需确认 } 之后就到尾(真正收尾) —— 否则是内容里的内嵌 JSON 结束
				k := j + 1
				for k < len(rs) && isJSONWS(rs[k]) {
					k++
				}
				if k >= len(rs) {
					b.WriteRune('"')
					inStr = false
					keyCtx = false
				} else {
					b.WriteString(`\"`) // 内容里的 "} (内嵌 JSON)
				}
			case rs[j] == ',':
				if looksLikeNextKey(rs, j) { // 终结后接下一键;而非内容里的 ", "
					b.WriteRune('"')
					inStr = false
					keyCtx = true
				} else {
					b.WriteString(`\"`) // 内容引号(如 列表: "A", "B")
				}
			case rs[j] == ':':
				if keyCtx { // 短键收尾
					b.WriteRune('"')
					inStr = false
					keyCtx = false
				} else { // 长值里的内嵌 JSON("x": ...),视为内容
					b.WriteString(`\"`)
				}
			default:
				b.WriteString(`\"`)
			}
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(c)
		}
	}
	out := strings.TrimSpace(b.String())
	if !strings.HasSuffix(out, "}") {
		out += "}" // 模型可能截掉了收尾花括号
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(out), &args); err != nil {
		return nil
	}
	return args
}

// looksLikeNextKey 判断从逗号之后是否是一个「对象键」起点:即 "short"": 形态。
// 用于区分 值收尾后接下一键(, "path": ...) 与 内容里的 ", " 序列。
func looksLikeNextKey(rs []rune, commaIdx int) bool {
	k := commaIdx + 1
	for k < len(rs) && isJSONWS(rs[k]) {
		k++
	}
	if k >= len(rs) || rs[k] != '"' {
		return false
	}
	for m := k + 1; m < len(rs) && m-k < 64; m++ {
		if rs[m] == '\\' {
			m++
			continue
		}
		if rs[m] == '"' {
			n := m + 1
			for n < len(rs) && isJSONWS(rs[n]) {
				n++
			}
			return n < len(rs) && rs[n] == ':'
		}
	}
	return false
}

func isJSONWS(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r'
}

func toolNames(tools []provider.ToolDef) []string {
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		names = append(names, t.Name)
	}
	return names
}

// actionKey 原地打转检测的动作签名。write_file 用「工具:path」——
// 反复重写同一文件(即使 content 每次略变)就是打转(实测「read/write 测试循环」,
// 旧签名含完整 content 导致每次 sig 不同、检测器管不住);其余工具保留完整参数
// (查询/对象即语义,get_page 同页重复、edit_file 同锚点重复都会被计数)。
func actionKey(name, args string) string {
	if name == "write_file" {
		if m := regexp.MustCompile(`"path"\s*:\s*"([^"]*)"`).FindStringSubmatch(args); m != nil {
			return "write_file:" + m[1]
		}
	}
	return name + ":" + args
}
