// Package cli kcp 命令行入口:状态/编译/查询/lint/观察/跑角色。
package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
  kcp update evidence <slug> <corroborate|contradict> <source> [claim]
                                      佐证/冲突回流既有概念页(机器重算 confidence)
  kcp index                           重建 wiki/index.md(从页面 frontmatter)
  kcp skeleton <raw文件> [--tags a,b] [--origin external|self] [--write]
                                      生成 source 页骨架
  kcp preflight <raw> [--source-page <页>]
                                      硬资产完整性核验(代码块/表/示例不丢)
  kcp check-sources                   检查概念/实体页 sources 是否被错误替换(git)
  kcp search "<查询>"                混合检索诊断(词法+向量,零 LLM,带私有度徽标)
  kcp eval [--seeds <文件>] [--k <n>] [--rebaseline] RAG-vs-编译复利对照实验(Agent-as-a-Judge 打分;--rebaseline 与上次基线比「wiki 复利 Δ」)
  kcp mcp                      MCP stdio server(支柱 B:wiki 暴露为 5 个工具,喂 coding/trading agents)
  kcp <role> "<输入>"           直接跑一个角色(compiler/qa/query)
  kcp list                      列出角色

环境变量: KCP_PROVIDER(openai-compatible|anthropic) KCP_MODEL KCP_API_KEY KCP_BASE_URL
          KCP_MAX_STEPS KCP_MAX_SAME_ACTION KCP_MAX_TOKENS KCP_MAX_HEAL KCP_DEADLINE(秒) KCP_STATE_FILE
          KCP_STREAM(默认1=SSE流式,0=关闭)
          KCP_COMPACT_TOKENS(上下文治理:估算token超此值压缩早期历史;默认80000,显式0=关闭)
          KCP_COMPACT_KEEP_ROUNDS(压缩保留最近几轮;默认8) KCP_CHECKPOINT_EVERY(checkpoint步频;默认3)
          KCP_MAX_OUTPUT_TOKENS(单次LLM输出上限透传;0=模型默认)
          KCP_EMBED_MODEL(留空=离线字符哈希嵌入;设置后用 OpenAI 兼容 /embeddings 语义检索)
          KCP_EMBED_BASE_URL KCP_EMBED_API_KEY(默认跟随 KCP_BASE_URL/KCP_API_KEY)
          KCP_RETRIEVE_K(默认5)
          KCP_EVAL_TIMEOUT(评估单次LLM调用超时秒;默认90) KCP_EVAL_JUDGE_PASSES(判官去噪轮数;默认3,0=单判)
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
		rebaseline := false
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
			case "--rebaseline":
				rebaseline = true
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
		if _, err := eval.RunCompare(root, cfg, seedList, rebaseline); err != nil {
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
		start := time.Now() // 本轮是否产出源页的判据起点
		code, toolCalls := runRole(root, cfg, "compiler", compileInput(root, args[1]), args[1])
		if code == 0 {
			postCompileQA(root, args[1], cfg) // Stop hook 语义:编译完自动核对,问题就地暴露
			// 收尾码反映编译成败:源页本轮未落盘(FinishGuard 推满仍无 write_file)→ 返回失败码,
			// 脚本/CI 可感知——实测端点配置错误时模型全程未参与,exit 0 会误导自动化。
			// 区分两态:真偷懒(零工作假成功)→ 失败;合法幂等重编译(源页已最新、模型有真实参与)→ 成功。
			slug := strings.TrimSuffix(filepath.Base(args[1]), ".md")
			sourcePage := filepath.Join(root, "wiki", "sources", slug+".md")
			if fi, err := os.Stat(sourcePage); err != nil || !fi.ModTime().After(start) {
				idempotent := idempotentRecompile(root, args[1], fi, sourcePage, toolCalls)
				if !idempotent {
					fmt.Fprintf(os.Stderr, "  ⚠️ compile 未产出源页 wiki/sources/%s.md,返回失败码。\n", slug)
					return 1
				}
				fmt.Fprintf(os.Stderr, "  ℹ️ 源页 %s.md 本轮未重写:raw 未变、页面已最新且模型有真实参与,判定为幂等重编译(成功)。\n", slug)
			}
		}
		return code
	case "query":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "用法: kcp query <问题>")
			return 1
		}
		code, _ := runRole(root, cfg, "query", strings.Join(args[1:], " "), "")
		return code
	default:
		// 兜底:把第一个参数当角色
		if args[0] == "compiler" || args[0] == "qa" || args[0] == "query" {
			input := ""
			if len(args) > 1 {
				input = strings.Join(args[1:], " ")
			}
			code, _ := runRole(root, cfg, args[0], input, "")
			return code
		}
		fmt.Fprintf(os.Stderr, "未知命令: %s\n%s", args[0], usage)
		return 1
	}
}

// compileInput 组装 compiler 的任务输入:路径 + 短预览 + 分页读取指示。
// 不再把全文塞进初始消息——旧做法 20KB 截断,长书必丢内容;全文经 read_file 工具结果
// 进入上下文一次(可被压缩折叠),与 Claude Code 的 Read 语义对齐。
func compileInput(root, rawFile string) string {
	fp := filepath.Join(root, rawFile)
	b, err := os.ReadFile(fp)
	if err != nil {
		return fmt.Sprintf("读取失败: %v", err)
	}
	lines := strings.Count(string(b), "\n") + 1
	preview := string(b)
	if len(preview) > 1500 {
		preview = preview[:1500] + "…(预览截断)"
	}
	return fmt.Sprintf(`只编译这一个 raw 源:%s(共 %d 行;禁止读取或编译其他 raw)。
第一步:用 read_file(path="%s") 读全文;返回要求分页时按提示 offset/limit 逐页读完——**禁止只凭下面的预览编译**。
源摘要页文件名必须是 wiki/sources/%s.md(与 raw 主干同名,禁止改名/加后缀)。按 SCHEMA 生成源摘要页(论证链保留全部代码/表/示例);再用 wiki_mentions 检查哪些概念/实体已被 2+ 个源提及(候选术语用完整词,勿用泛化短词;⚠️ 警示行出现时优先并入完整页面),满足条件的创建/更新 wiki/concepts/、wiki/entities/;禁止创建 synthesis 页。

=== 文件预览(仅供判断题材,前 1500 字节) ===
%s`, rawFile, lines, rawFile, strings.TrimSuffix(filepath.Base(rawFile), ".md"), preview)
}

// postCompileQA 编译后自动质检(对标 Claude Code 的 Stop hook):
// preflight 硬资产完整性 + sources 缩水检查 + lint 摘要;打 stderr,不污染 stdout。
func postCompileQA(root, rawFile string, cfg config.Config) {
	fmt.Fprintln(os.Stderr, "\n[自动质检] 编译后检查…")
	slug := strings.TrimSuffix(filepath.Base(rawFile), ".md")
	sourcePage := filepath.Join(root, "wiki", "sources", slug+".md")
	if report, issues, err := wiki.Preflight(filepath.Join(root, rawFile), sourcePage); err == nil {
		// 覆盖核验是提示不是缺失,单独亮出(重复块以代表保留的语义无损判定)
		for _, line := range strings.Split(report, "\n") {
			if strings.Contains(line, "覆盖核验") {
				fmt.Fprintln(os.Stderr, "  "+strings.TrimSpace(line))
			}
		}
		if len(issues) == 0 {
			fmt.Fprintln(os.Stderr, "  preflight: 硬资产完整(代码/表/示例无缺失)")
		} else {
			fmt.Fprintf(os.Stderr, "  ⚠️ preflight 缺失 %d 项(重编或人工补齐):\n", len(issues))
			for i, it := range issues {
				if i >= 8 {
					fmt.Fprintf(os.Stderr, "    …共 %d 项\n", len(issues))
					break
				}
				fmt.Fprintf(os.Stderr, "    - %s\n", it)
			}
		}
	}
	if problems, err := wiki.CheckSourcesShrink(root); err == nil && len(problems) > 0 {
		fmt.Fprintf(os.Stderr, "  ⚠️ 概念/实体页 sources 缩水 %d 页:\n", len(problems))
		for _, p := range problems {
			fmt.Fprintf(os.Stderr, "    - %s\n", p)
		}
	}
	// 矛盾核对落盘检查(切片 A):独立重扫(不依赖运行期日志,防模型跳过/糊弄),
	// 验证扫描出的相关页是否被链入「连接」或有矛盾标注——没落盘即报警。
	if b, err := os.ReadFile(sourcePage); err == nil {
		hits := tools.ScanContradictions(root, slug, string(b), 4)
		if len(hits) > 0 {
			text := string(b)
			labels := make([]string, len(hits))
			for i, h := range hits {
				labels[i] = h.Result.Label
			}
			linked := false
			for _, l := range labels {
				if strings.Contains(text, "[["+l+"]]") {
					linked = true
					break
				}
			}
			if !linked && !strings.Contains(text, "矛盾") && !strings.Contains(text, "冲突") {
				fmt.Fprintf(os.Stderr, "  ⚠️ 矛盾核对:本源与 %d 个已有页相关(%s),但「连接」未链出任何一页且无矛盾标注——核对结论未落盘,需人工补核对。\n", len(hits), strings.Join(labels, ", "))
			} else {
				fmt.Fprintln(os.Stderr, "  矛盾核对:相关页已链入「连接」或已标注 ✓")
			}
			// 标注归一化(切片 B 附属):冲突必须用 lint 可识别标记,且须传播到相关概念页「张力与缺口」。
			hasStdMarker := containsAny(text, "⚠️ 冲突", "⚠️ 矛盾", "矛盾声明", "CONTRADICTION")
			if (strings.Contains(text, "冲突") || strings.Contains(text, "口径不一致") || strings.Contains(text, "分类不一致") || strings.Contains(text, "分类口径差异") || strings.Contains(text, "口径差异")) && !hasStdMarker {
				fmt.Fprintln(os.Stderr, "  ⚠️ 矛盾标注:本源页出现「冲突/口径不一致/分类口径差异」字样但未用 lint 可识别标记(⚠️ 冲突:/⚠️ 矛盾:/<!-- CONTRADICTION -->),lint 矛盾计数将显示 0——请归一化标注。")
			}
			conflictLabels := conflictMarkedLabels(text)
			for _, h := range hits {
				if !hasStdMarker || !contains(conflictLabels, h.Result.Label) || !strings.Contains(h.Result.Path, "concepts") {
					continue
				}
				if cb, err := os.ReadFile(filepath.Join(root, h.Result.Path)); err == nil {
					if !containsAny(string(cb), "⚠️ 冲突", "⚠️ 矛盾", "矛盾声明", "CONTRADICTION") {
						fmt.Fprintf(os.Stderr, "  ⚠️ 冲突传播:本源页对概念页 [[%s]] 标了冲突,但该页「张力与缺口」未同步 ⚠️ 冲突——需补一条(两边保留,不消除冲突)。\n", h.Result.Label)
					}
				}
			}
			// 概念页张力节归一化检查:概念页记录真实冲突(分类口径差异/口径不一致)时也必须用 lint
			// 可识别标记——否则源页标记日后被改时,概念页的冲突会静默脱离 lint 矛盾计数。
			for _, label := range conceptWeakMarkerProblems(root, hits) {
				fmt.Fprintf(os.Stderr, "  ⚠️ 矛盾归一化:概念页 [[%s]]「张力与缺口」出现「分类口径差异/口径不一致」但未用 lint 可识别标记(⚠️ 冲突:/⚠️ 矛盾:/<!-- CONTRADICTION -->),lint 矛盾计数将显示 0——请归一化为 ⚠️ 冲突: 格式(两边保留,不消除冲突)。\n", label)
			}
		}
	}
	fmt.Fprintln(os.Stderr, "  lint:", lintSummary(root))
}

// conflictMarkedLabels 从文本里抽取出被 lint 可识别冲突标记的行所引用的 wikilink label。
var cliLinkRe = regexp.MustCompile(`\[\[([^\]|]+)(?:\|[^\]]+)?\]\]`)

func conflictMarkedLabels(text string) []string {
	var out []string
	for _, ln := range strings.Split(text, "\n") {
		if !strings.Contains(ln, "⚠️ 冲突") && !strings.Contains(ln, "⚠️ 矛盾") && !strings.Contains(ln, "矛盾声明") && !strings.Contains(ln, "CONTRADICTION") {
			continue
		}
		for _, m := range cliLinkRe.FindAllStringSubmatch(ln, -1) {
			out = append(out, m[1])
		}
	}
	return out
}

// conceptWeakMarkerProblems 扫描相关概念页:若其「张力与缺口」用非 lint 可识别标记(分类口径差异/
// 口径不一致等)记录真实冲突,返回问题页 label 列表——供 QA 报警归一化为 ⚠️ 冲突: 格式。
// 已带任一 lint 可识别标记的页面不报警(允许既有正常冲突页存在)。
func conceptWeakMarkerProblems(root string, hits []tools.ContradictionHit) []string {
	var out []string
	for _, h := range hits {
		if !strings.Contains(h.Result.Path, "concepts") {
			continue
		}
		if cb, err := os.ReadFile(filepath.Join(root, h.Result.Path)); err == nil {
			cp := string(cb)
			if (strings.Contains(cp, "分类口径差异") || strings.Contains(cp, "口径差异") || strings.Contains(cp, "分类不一致") || strings.Contains(cp, "口径不一致")) && !containsAny(cp, "⚠️ 冲突", "⚠️ 矛盾", "矛盾声明", "CONTRADICTION") {
				out = append(out, h.Result.Label)
			}
		}
	}
	return out
}

// contains 字符串切片包含判断(小工具,避免引入外部依赖)。
func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// containsAny 任一关键字命中。
func containsAny(s string, kws ...string) bool {
	for _, k := range kws {
		if strings.Contains(s, k) {
			return true
		}
	}
	return false
}

// lintSummary 单行 lint 概要(取报告首行)。
func lintSummary(root string) string {
	for _, line := range strings.Split(wiki.ReportLint(root), "\n") {
		if strings.HasPrefix(line, "lint:") {
			return strings.TrimSpace(line)
		}
	}
	return "见 kcp lint"
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

// runRole 用自研运行时跑一个角色(provider + 工具 + M04 循环)。rawFile 为 compile 场景的源路径(步数自适应用)。
func runRole(root string, cfg config.Config, role, input, rawFile string) (int, int) {
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
	// 用户显式设 KCP_MAX_STEPS 时不覆盖;长源(>100KB)分页读 + 分批落盘需要更多步,按大小自适应。
	steps := cfg.MaxSteps
	if role == "compiler" && os.Getenv("KCP_MAX_STEPS") == "" {
		steps = 25
		if rawFile != "" {
			if fi, err := os.Stat(filepath.Join(root, rawFile)); err == nil && fi.Size() > 100<<10 {
				steps = 35
			}
		}
	}

	// describe_image 的视觉回调:type-assert provider 到 VisionProvider;非多模态则提示(Tier 2 vision)。
	visionFn := func(imagePath string) string {
		vp, ok := p.(provider.VisionProvider)
		if !ok {
			return fmt.Sprintf("当前 provider(%s) 不支持视觉描述。", cfg.Provider)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		desc, err := vp.Vision(ctx, imagePath, "这是一张知识库 raw 中的图片。请完整描述其内容(文字、结构、要点);若是图表/架构图,描述节点与关系。描述将编译进 wiki 页面。")
		if err != nil {
			return fmt.Sprintf("视觉描述失败: %v", err)
		}
		return desc
	}
	toolList, idx, readCov, contradictionLog := tools.Build(root, terminalAsk, visionFn, eval.RetrievalOpts(root, cfg))
	if idx != nil {
		defer idx.Save(root) // 进程退出前把索引增量留给下次(原子写)
	}

	// StateFile 按 raw slug 隔离(compile 场景):连续编译多个源若共用同一个 KCP_STATE_FILE,
	// runtime 会无条件 loadCheckpoint() 把上一次的消息历史带进这一次——模型可能误以为该源已编译
	// 或被困在旧任务。隔离后每个源有自己的 checkpoint,互不污染(P5)。
	if role == "compiler" && rawFile != "" && cfg.StateFile != "" {
		slug := strings.TrimSuffix(filepath.Base(rawFile), ".md")
		dir := filepath.Join(root, ".kcp", "state")
		_ = os.MkdirAll(dir, 0o755)
		cfg.StateFile = filepath.Join(dir, slug+".json")
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
	// 落盘兜底(切片 B):compiler 给出最终答复但目标源页未落盘时,强制注入续跑要求 write_file。
	// 实测 glm-4.5-air 对长源反复「读 2-3 页→输出总结→放弃工具循环」,事后报告不够,要堵在退出前。
	if role == "compiler" && rawFile != "" {
		slug := strings.TrimSuffix(filepath.Base(rawFile), ".md")
		sourcePage := filepath.Join(root, "wiki", "sources", slug+".md")
		// 两级判据:①源页不存在→强制首写;②源页存在但 preflight 有阻断性缺失→强制补全。
		// 实测 glm 会「读头尾→写空心页(9 节齐全但 marker/代码/表全缺)」糊弄过存在性判据,故叠加质量闸门。
		// preflight 的重复块覆盖第三态保证:deepseek 式语义压缩通过,glm 式整块缺失被拦。
		rt.FinishGuard = func(provider.Message) string {
			if _, err := os.Stat(sourcePage); err != nil {
				return fmt.Sprintf("⚠️ 编译未完成:目标 wiki/sources/%s.md 还不存在。你现在必须调用 write_file 落盘源页草稿(至少 frontmatter + 一句话结论 + 论证链骨架),然后可继续完善;只输出总结文本不算完成。", slug)
			}
			if _, issues, err := wiki.Preflight(filepath.Join(root, rawFile), sourcePage); err == nil && len(issues) > 0 {
				brief := issues[0]
				if len(issues) > 1 {
					brief += fmt.Sprintf("(…共 %d 项)", len(issues))
				}
				return fmt.Sprintf("⚠️ 源页 preflight 未通过:%s。继续完善 wiki/sources/%s.md——按 SCHEMA 补齐缺失硬资产与节,全部通过后再收尾。", brief, slug)
			}
			// ③ 读覆盖 gap:>80KB 长源中间有未读区间 = 内容没读完,拒收浅页。
			// 实测 glm 只读头+尾就写页,存在性与硬资产都拦不住,只有台账能确定性地抓住。
			if lo, hi, ok := readCov.Gap(rawFile); ok {
				return fmt.Sprintf("⚠️ raw %s 还有 %d-%d 行未读(中间跳读了)。先用 read_file offset=%d 补读该段,再据它完善源页 %s——跳过内容写出的页会被拒收。", rawFile, lo, hi, lo, slug)
			}
			// ④ 矛盾核对:编译器没回头对照 wiki 就收尾 → 强制补一轮(check_contradictions)。
			// 此前只有 prompt 指令「有矛盾就标注」,弱模型隔离编译不回头核对,矛盾静默丢失;
			// 台账把「有没有对照 wiki」变成确定性记录,收尾时必跑,跑过才能放行。
			if !contradictionLog.Scanned(slug) {
				return fmt.Sprintf("⚠️ 你还没运行 check_contradictions(source=\"%s\") 对照已有页面。调用它,根据返回的相关页逐个判断 冲突/佐证/无涉,把结论写进「连接」节;有冲突的在源页显式标注(矛盾/冲突)。", slug)
			}
			// ⑤ 证据回流(evidenceLog):check_contradictions 扫出的相关概念页必须被
			// update_evidence 逐一裁决(corroborate/contradict/skip)——正向复利与负向张力
			// 一样确定性执行,不能只口头判断。漏裁决的页被列名强制,模型不能装没看见。
			if missing := contradictionLog.MissingEvidence(slug); len(missing) > 0 {
				return fmt.Sprintf("⚠️ check_contradictions 扫出 %d 个相关概念页但尚未回流裁决: %s。对每一页调用 update_evidence(slug=<概念页>, source=\"%s\")——佐证→corroborate(追加源+机器升 confidence) / 冲突→contradict(落 ⚠️ 冲突) / 无关→skip。既有页必须被物理强化,不能只口头判断。", len(missing), strings.Join(missing, "、"), slug)
			}
			return "" // 源页存在、硬资产无缺失、长源已读全覆盖、概念页已全裁决,视为完成
		}
		rt.MaxFinishPushes = 2 // 最多续跑 2 轮,仍不达标则退出(交由 postCompileQA 如实报告)
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
			case agent.EvFinishGuard:
				fmt.Fprintf(os.Stderr, "\n[step %d] ⇐ 落盘兜底:强制续跑,要求 write_file 落盘\n", ev.Step)
			}
		}
	}
	out, err := rt.Run(ctx, input, "")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1, 0
	}
	if streamed {
		fmt.Println() // 内容已实时输出,补换行
	} else {
		fmt.Println(out)
	}
	return 0, rt.ToolCalls
}

// idempotentRecompile 判定「合法幂等重编译」:源页本轮未重写但可放行——
// ①源页存在;②raw 自源页编译后未变(页面不陈旧);③模型本轮有真实参与(≥1 次工具调用,
// 排除零工作假成功);④源页 preflight 无阻断性缺失(硬资产完整)。
// 真偷懒(零工作但声称完成)不满足③ → 仍判失败。
func idempotentRecompile(root, rawRel string, fi os.FileInfo, sourcePage string, toolCalls int) bool {
	if fi == nil || toolCalls == 0 {
		return false // 源页不存在或模型零参与 → 不是幂等,是失败
	}
	// raw 自源页编译后未变(源页不陈旧)
	rawFi, err := os.Stat(filepath.Join(root, rawRel))
	if err != nil || rawFi.ModTime().After(fi.ModTime()) {
		return false // raw 比源页新 = 页面陈旧,必须重写
	}
	// preflight 无阻断性缺失
	if _, issues, err := wiki.Preflight(filepath.Join(root, rawRel), sourcePage); err != nil || len(issues) > 0 {
		return false
	}
	return true
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
	case "evidence":
		if len(args) < 4 {
			err = fmt.Errorf("用法: kcp update evidence <slug> <corroborate|contradict> <source> [claim]")
		} else {
			msg, err = wiki.ApplyEvidence(root, args[1], args[2], args[3], strings.Join(args[4:], " "))
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
