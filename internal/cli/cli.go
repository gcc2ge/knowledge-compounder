// Package cli kcp 命令行入口:状态/编译/查询/lint/观察/跑角色。
package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gcc2ge/knowledge-compounder/internal/agent"
	"github.com/gcc2ge/knowledge-compounder/internal/agents"
	"github.com/gcc2ge/knowledge-compounder/internal/config"
	"github.com/gcc2ge/knowledge-compounder/internal/eval"
	"github.com/gcc2ge/knowledge-compounder/internal/index"
	"github.com/gcc2ge/knowledge-compounder/internal/mcp"
	"github.com/gcc2ge/knowledge-compounder/internal/provider"
	"github.com/gcc2ge/knowledge-compounder/internal/retrieval"
	"github.com/gcc2ge/knowledge-compounder/internal/tools"
	"github.com/gcc2ge/knowledge-compounder/internal/wiki"
)

const usage = `kcp — 知识编译复利引擎(自研 agent,任意 LLM)

用法:
  kcp status                    查看知识库状态(页面计数 + 未编译 raw)
  kcp lint                      健康检查(断链/孤儿)
  kcp compile <raw文件>         单源编译(compiler agent 按 SCHEMA 生成 source 页)
  kcp query "<问题>"           检索知识库 + 综合回答
  kcp observe -s <策略> -T <标题> -c <内容>   捕获观察
  kcp update add-source <slug> <源>   确定性页面编辑(无 LLM)
  kcp update touch <slug>             更新 updated 日期
  kcp update add-link <slug> <目标>   往「相关」节加 wikilink
  kcp index                           重建 wiki/index.md(从页面 frontmatter)
  kcp skeleton <raw文件> [--tags a,b] [--origin external|self] [--write]
                                      生成 source 页骨架
  kcp preflight <raw> [--source-page <页>]
                                      硬资产完整性核验(代码块/表/示例不丢)
  kcp check-sources                   检查概念/实体页 sources 是否被错误替换(git)
  kcp search "<查询>"                混合检索诊断(词法+向量,零 LLM,带私有度徽标)
  kcp eval [--seeds <文件>] [--k <n>] RAG-vs-编译复利对照实验(Agent-as-a-Judge 打分)
  kcp mcp                      MCP stdio server(支柱 B:wiki 暴露为 5 个工具,喂 coding/trading agents)
  kcp <role> "<输入>"           直接跑一个角色(compiler/qa/query)
  kcp list                      列出角色

环境变量: KCP_PROVIDER(openai-compatible|anthropic) KCP_MODEL KCP_API_KEY KCP_BASE_URL
          KCP_MAX_STEPS KCP_MAX_SAME_ACTION KCP_MAX_TOKENS KCP_MAX_HEAL KCP_DEADLINE(秒) KCP_STATE_FILE
          KCP_STREAM(默认1=SSE流式,0=关闭)
          KCP_COMPACT_TOKENS(上下文治理:估算token超此值压缩早期历史;0=关闭)
          KCP_COMPACT_KEEP_ROUNDS(压缩保留最近几轮;默认8) KCP_CHECKPOINT_EVERY(checkpoint步频;默认3)
          KCP_MAX_OUTPUT_TOKENS(单次LLM输出上限透传;0=模型默认)
          KCP_EMBED_MODEL(留空=离线字符哈希嵌入;设置后用 OpenAI 兼容 /embeddings 语义检索)
          KCP_EMBED_BASE_URL KCP_EMBED_API_KEY(默认跟随 KCP_BASE_URL/KCP_API_KEY)
          KCP_RETRIEVE_K(默认5)
`

// Main 命令分发。返回退出码。
func Main(args []string) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" {
		fmt.Print(usage)
		return 0
	}
	root, err := wiki.Root()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	cfg := config.Load()

	switch args[0] {
	case "status":
		fmt.Println(wiki.StatusText(root))
		return 0
	case "lint":
		fmt.Print(wiki.ReportLint(root))
		return 0
	case "list":
		fmt.Println("compiler\nqa\nquery")
		return 0
	case "observe":
		if err := observe(root, args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println("观察已捕获到 raw/observations/")
		return 0
	case "update":
		return runUpdate(root, args[1:])
	case "index":
		fp, err := wiki.WriteIndex(root)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println("已重建 " + fp)
		return 0
	case "skeleton":
		return runSkeleton(root, args[1:])
	case "preflight":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "用法: kcp preflight <raw> [--source-page <页>]")
			return 1
		}
		sourcePage := ""
		for i := 2; i < len(args); i++ {
			if args[i] == "--source-page" && i+1 < len(args) {
				sourcePage = filepath.Join(root, args[i+1])
				i++
			}
		}
		report, issues, err := wiki.Preflight(filepath.Join(root, args[1]), sourcePage)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Print(report)
		for _, it := range issues {
			fmt.Println("  " + it)
		}
		if len(issues) > 0 {
			return 1
		}
		return 0
	case "check-sources":
		problems, err := wiki.CheckSourcesShrink(root)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if len(problems) == 0 {
			fmt.Println("check-sources: 无缩水,sources 完整。")
			return 0
		}
		fmt.Println("⚠️ 概念/实体页 sources 缩水(compiler 重写 frontmatter 常见 bug):")
		for _, p := range problems {
			fmt.Println("  " + p)
		}
		return 1
	case "search":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "用法: kcp search \"<查询>\"")
			return 1
		}
		return runSearch(root, cfg, strings.Join(args[1:], " "))
	case "eval":
		seeds, k := "", 0
		for i := 1; i < len(args); i++ {
			switch args[i] {
			case "--seeds":
				if i+1 < len(args) {
					seeds = args[i+1]
					i++
				}
			case "--k":
				if i+1 < len(args) {
					fmt.Sscanf(args[i+1], "%d", &k)
					i++
				}
			}
		}
		if k > 0 {
			cfg.RetrieveK = k
		}
		seedList, err := eval.LoadSeeds(root, seeds)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if _, err := eval.RunCompare(root, cfg, seedList); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	case "mcp":
		srv := mcp.New(root, eval.RetrievalOpts(root, cfg))
		if err := srv.Serve(os.Stdin, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "mcp: ", err)
			return 1
		}
		return 0
	case "compile":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "用法: kcp compile <raw文件>")
			return 1
		}
		return runRole(root, cfg, "compiler", compileInput(root, args[1]))
	case "query":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "用法: kcp query <问题>")
			return 1
		}
		return runRole(root, cfg, "query", strings.Join(args[1:], " "))
	default:
		// 兜底:把第一个参数当角色
		if args[0] == "compiler" || args[0] == "qa" || args[0] == "query" {
			input := ""
			if len(args) > 1 {
				input = strings.Join(args[1:], " ")
			}
			return runRole(root, cfg, args[0], input)
		}
		fmt.Fprintf(os.Stderr, "未知命令: %s\n%s", args[0], usage)
		return 1
	}
}

// compileInput 组装 compiler 的任务输入:raw 内容 + SCHEMA 要求。
func compileInput(root, rawFile string) string {
	fp := filepath.Join(root, rawFile)
	b, err := os.ReadFile(fp)
	if err != nil {
		return fmt.Sprintf("读取失败: %v", err)
	}
	content := string(b)
	if len(content) > 20000 {
		content = content[:20000] + "\n…(截断)"
	}
	return fmt.Sprintf("只编译下面这一个 raw 源:%s(禁止读取或编译其他 raw)。源摘要页文件名必须是 wiki/sources/%s.md(与 raw 主干同名,禁止改名/加后缀)。按 SCHEMA 生成源摘要页(论证链保留全部代码/表/示例);再用 search_wiki 检查哪些概念/实体已被 2+ 个源提及,满足条件的直接创建/更新 wiki/concepts/、wiki/entities/ 页面;禁止创建 synthesis 页。\n\n%s",
		rawFile, strings.TrimSuffix(filepath.Base(rawFile), ".md"), content)
}

// runSearch 混合检索诊断(零 LLM):词法+向量融合分、词法/语义子分、私有度徽标。
func runSearch(root string, cfg config.Config, query string) int {
	opts := eval.RetrievalOpts(root, cfg)
	idx := index.Load(root)
	var results []retrieval.Result
	if idx != nil {
		results = idx.Rank(query, opts)
		_ = idx.Save(root) // 留快照给下次(首次运行建索引的开销只付一次)
	} else {
		results = wiki.Retrieve(root, query, opts)
	}
	if len(results) == 0 {
		fmt.Println("未找到相关页面。")
		return 0
	}
	fmt.Printf("检索「%s」: %d 条(嵌入=%s, 缓存=%s, 索引=%s)\n", query, len(results), embedDesc(opts), retrieval.CachePath(root), indexDesc(idx))
	for _, r := range results {
		badge := wiki.EvidenceBadge(root, r.Path)
		fmt.Printf("- [[%s]] %s 融合%.2f 词法%.2f 语义%.2f: %s\n",
			r.Label, badge, r.Score, r.Lexical, r.Semantic, r.Summary)
	}
	return 0
}

func embedDesc(opts retrieval.Options) string {
	if opts.Embedder == nil {
		return "无向量层"
	}
	return "已启用"
}

// indexDesc 索引状态描述(nil=降级全扫)。
func indexDesc(idx *index.Index) string {
	if idx == nil {
		return "off(全扫)"
	}
	return fmt.Sprintf("on(%d 页)", len(idx.Pages))
}

// runRole 用自研运行时跑一个角色(provider + 工具 + M04 循环)。
func runRole(root string, cfg config.Config, role, input string) int {
	timeout := time.Duration(cfg.Deadline) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	p := newProvider(cfg)
	system := map[string]string{
		"compiler": agents.CompilerPrompt,
		"query":    agents.QueryPrompt,
		"qa":       agents.QAPrompt,
	}[role]
	// 注入当天日期,防模型幻觉编造 compiled/created/updated 日期。
	system += fmt.Sprintf("\n\n今天日期:%s。frontmatter 的 compiled/created/updated 一律用它,不要自己猜。",
		time.Now().Format("2006-01-02"))

	// 编译一个源要写 1 个 source 页 + 2-6 个概念/实体页,外加检索核对;默认 10 步不够(实测在调查阶段耗尽)。
	// 用户显式设 KCP_MAX_STEPS 时不覆盖。
	steps := cfg.MaxSteps
	if role == "compiler" && os.Getenv("KCP_MAX_STEPS") == "" {
		steps = 25
	}

	toolList, idx := tools.Build(root, terminalAsk, eval.RetrievalOpts(root, cfg))
	if idx != nil {
		defer idx.Save(root) // 进程退出前把索引增量留给下次(原子写)
	}

	rt := &agent.Runtime{
		Provider:        p,
		SystemPrompt:    system,
		Tools:           toolList,
		MaxSteps:        steps,
		MaxSameAction:   cfg.MaxSameAction,
		MaxTokens:       cfg.MaxTokens,
		MaxHeal:         cfg.MaxHeal,
		StateFile:       cfg.StateFile,
		CheckpointEvery: cfg.CheckpointEvery,
		Compaction: agent.CompactionPolicy{
			Enabled:    cfg.CompactTokens > 0,
			MaxTokens:  cfg.CompactTokens,
			KeepRounds: cfg.KeepRounds,
		},
	}
	streamed := false
	if cfg.Stream {
		// 结构化事件流渲染:文本打 stdout(流式),工具调用/返回打 stderr(逐步可见)。
		rt.OnEvent = func(ev agent.Event) {
			switch ev.Kind {
			case agent.EvText:
				fmt.Print(ev.Text)
				streamed = true
			case agent.EvToolCall:
				fmt.Fprintf(os.Stderr, "\n[step %d] → %s(%s)\n", ev.Step, ev.Tool, truncStr(ev.Args, 100))
			case agent.EvToolResult:
				fmt.Fprintf(os.Stderr, "[step %d] ← %s: %s\n", ev.Step, ev.Tool, truncStr(ev.Result, 180))
			case agent.EvCompact:
				fmt.Fprintf(os.Stderr, "[step %d] ⇥ 上下文压缩:折叠 %d 条早期消息\n", ev.Step, ev.Dropped)
			}
		}
	}
	out, err := rt.Run(ctx, input, "")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if streamed {
		fmt.Println() // 内容已实时输出,补换行
	} else {
		fmt.Println(out)
	}
	return 0
}

// runUpdate 确定性页面编辑:add-source / touch / add-link(无 LLM,对齐 wiki-update.py)。
func runUpdate(root string, args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "用法: kcp update <add-source|touch|add-link> <slug> [参数]")
		return 1
	}
	var msg string
	var err error
	switch args[0] {
	case "add-source":
		if len(args) < 3 {
			err = fmt.Errorf("用法: kcp update add-source <slug> <源>")
		} else {
			msg, err = wiki.AddSource(root, args[1], args[2])
		}
	case "touch":
		if len(args) < 2 {
			err = fmt.Errorf("用法: kcp update touch <slug>")
		} else {
			msg, err = wiki.Touch(root, args[1])
		}
	case "add-link":
		if len(args) < 3 {
			err = fmt.Errorf("用法: kcp update add-link <slug> <目标>")
		} else {
			msg, err = wiki.AddLink(root, args[1], args[2])
		}
	default:
		err = fmt.Errorf("未知 update 操作: %s(支持 add-source/touch/add-link)", args[0])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Println(msg)
	return 0
}

// runSkeleton 生成 source 页骨架;--write 直接写入 wiki/sources/。
func runSkeleton(root string, args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "用法: kcp skeleton <raw文件> [--tags a,b] [--origin external|self] [--write]")
		return 1
	}
	rawPath := args[0]
	var tags, origin string
	write := false
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--tags":
			if i+1 < len(args) {
				tags = args[i+1]
				i++
			}
		case "--origin":
			if i+1 < len(args) {
				origin = args[i+1]
				i++
			}
		case "--write":
			write = true
		}
	}
	body, err := wiki.Skeleton(root, rawPath, tags, origin)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if !write {
		fmt.Println(body)
		return 0
	}
	slug := strings.TrimSuffix(filepath.Base(rawPath), filepath.Ext(rawPath))
	fp := filepath.Join(root, "wiki", "sources", slug+".md")
	if err := os.WriteFile(fp, []byte(body), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	rel, _ := filepath.Rel(root, fp)
	fmt.Println("已写入 " + rel)
	return 0
}

// truncStr 截断长文本供 stderr 逐步渲染(工具参数/结果)。
func truncStr(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// terminalAsk 策展决策交互:终端时读用户裁决;非终端(管道/CI)返回非交互提示。
func terminalAsk(question string) string {
	fi, err := os.Stdin.Stat()
	if err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return "[非交互] 无终端用户;按 SCHEMA 纪律自行决策(单次提及不建页、2+ 源才建概念/实体页)。"
	}
	fmt.Fprintln(os.Stderr, "\n[策展决策] "+question)
	fmt.Fprint(os.Stderr, "裁决(回车=采纳模型建议;输入内容=覆盖建议): ")
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" || line == "y" || line == "yes" {
		return "用户裁决:采纳你的建议,继续。"
	}
	return "用户裁决: " + line
}

func newProvider(cfg config.Config) provider.Provider {
	if cfg.Provider == "anthropic" {
		p := provider.NewAnthropic(cfg.Model, cfg.APIKey, cfg.BaseURL)
		p.MaxTokens = cfg.MaxOutputTokens
		return p
	}
	p := provider.NewOpenAICompatible(cfg.Model, cfg.APIKey, cfg.BaseURL)
	p.MaxTokens = cfg.MaxOutputTokens
	return p
}

// observe 捕获观察:写 raw/observations/<ts>-<strategy>.md(frontmatter + 内容)。
func observe(root string, args []string) error {
	var strategy, title, content string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-s":
			if i+1 < len(args) {
				strategy = args[i+1]
				i++
			}
		case "-T":
			if i+1 < len(args) {
				title = args[i+1]
				i++
			}
		case "-c":
			if i+1 < len(args) {
				content = args[i+1]
				i++
			}
		}
	}
	if title == "" {
		return fmt.Errorf("标题必填(-T)")
	}
	dir := filepath.Join(root, "raw", "observations")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	ts := time.Now().Format("2006-01-02-150405")
	name := fmt.Sprintf("%s-%s.md", ts, title)
	md := fmt.Sprintf("---\n类型: %s\n策略: %s\n标题: %s\n时间: %s\n---\n\n%s\n", "observation", strategy, title, time.Now().Format(time.RFC3339), content)
	return os.WriteFile(filepath.Join(dir, name), []byte(md), 0o644)
}
