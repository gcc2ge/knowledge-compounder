// Package cli kcp 命令行入口:状态/编译/查询/lint/观察/跑角色。
package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/knowledge-compounder/kcp/internal/agent"
	"github.com/knowledge-compounder/kcp/internal/agents"
	"github.com/knowledge-compounder/kcp/internal/config"
	"github.com/knowledge-compounder/kcp/internal/provider"
	"github.com/knowledge-compounder/kcp/internal/tools"
	"github.com/knowledge-compounder/kcp/internal/wiki"
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
  kcp check-sources                   检查概念/实体页 sources 是否被错误替换(git)
  kcp <role> "<输入>"           直接跑一个角色(compiler/qa/query)
  kcp list                      列出角色

环境变量: KCP_PROVIDER(openai-compatible|anthropic) KCP_MODEL KCP_API_KEY KCP_BASE_URL
          KCP_MAX_STEPS KCP_MAX_SAME_ACTION
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
	return fmt.Sprintf("编译以下 raw 源,按 SCHEMA 生成 wiki/sources/ 源摘要页(论证链保留全部代码/表/示例),并检查交叉引用:\n\n%s", content)
}

// runRole 用自研运行时跑一个角色(provider + 工具 + M04 循环)。
func runRole(root string, cfg config.Config, role, input string) int {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	p := newProvider(cfg)
	system := map[string]string{
		"compiler": agents.CompilerPrompt,
		"query":    agents.QueryPrompt,
		"qa":       agents.QAPrompt,
	}[role]

	rt := &agent.Runtime{
		Provider:      p,
		SystemPrompt:  system,
		Tools:         tools.Build(root),
		MaxSteps:      cfg.MaxSteps,
		MaxSameAction: cfg.MaxSameAction,
	}
	out, err := rt.Run(ctx, input, "")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Println(out)
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

func newProvider(cfg config.Config) provider.Provider {
	if cfg.Provider == "anthropic" {
		return provider.NewAnthropic(cfg.Model, cfg.APIKey, cfg.BaseURL)
	}
	return provider.NewOpenAICompatible(cfg.Model, cfg.APIKey, cfg.BaseURL)
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
