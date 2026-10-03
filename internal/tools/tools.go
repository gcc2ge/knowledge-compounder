// Package tools 把知识库操作暴露成可调用工具(M06 工具系统)。
// 全部 Go 原生实现,不依赖 Python 子进程。写文件只允许 wiki/raw/examples 内。
package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/knowledge-compounder/kcp/internal/provider"
	"github.com/knowledge-compounder/kcp/internal/wiki"
)

// Build 构造工具列表。root 为项目根;ask 为策展决策回调(向用户提问等裁决),nil 则 ask_user 返回非交互提示。
func Build(root string, ask func(string) string) []provider.ToolDef {
	return []provider.ToolDef{
		{
			Name: "wiki_status", Description: "查看知识库状态:页面计数 + 未编译 raw",
			Parameters: obj(map[string]any{}),
			Func:       func(map[string]any) string { return wiki.StatusText(root) },
		},
		{
			Name: "search_wiki", Description: "在知识库中检索相关页面",
			Parameters: obj(map[string]any{"query": strProp, "k": intProp}),
			Func: func(args map[string]any) string {
				query := str(args, "query")
				k := intArg(args, "k", 5)
				results := wiki.SearchPages(root, query, k)
				if len(results) == 0 {
					return "知识库中未找到匹配页面。"
				}
				var b strings.Builder
				for _, r := range results {
					fmt.Fprintf(&b, "- [[%s]] (命中 %d): %s\n", strings.TrimSuffix(r.Page, ".md"), r.Score, r.Summary)
				}
				return b.String()
			},
		},
		{
			Name: "get_page", Description: "读取知识库中的一个 wiki 页面(slug 或文件名)",
			Parameters: obj(map[string]any{"page": strProp}),
			Func: func(args map[string]any) string {
				page := str(args, "page")
				text, dir, ok := wiki.GetPage(root, page)
				if !ok {
					return fmt.Sprintf("[[%s]] 不存在。", strings.TrimSuffix(page, ".md"))
				}
				return fmt.Sprintf("## %s (%s/)\n\n%s", strings.TrimSuffix(page, ".md"), dir, text)
			},
		},
		{
			Name: "read_file", Description: "读取项目内文件(相对项目根)",
			Parameters: obj(map[string]any{"path": strProp}),
			Func: func(args map[string]any) string {
				return readSafe(root, str(args, "path"))
			},
		},
		{
			Name: "write_file", Description: "写入 wiki/raw/examples 内的文件(编译写页面用)",
			Parameters: obj(map[string]any{"path": strProp, "content": strProp}),
			Func: func(args map[string]any) string {
				path, content := str(args, "path"), str(args, "content")
				fp, ok := safeJoin(root, path, "wiki", "raw", "examples")
				if !ok {
					return fmt.Sprintf("拒绝:只允许写 wiki/ raw/ examples/,收到 %s", path)
				}
				if err := os.MkdirAll(filepath.Dir(fp), 0o755); err != nil {
					return fmt.Sprintf("写入失败: %v", err)
				}
				if err := os.WriteFile(fp, []byte(content), 0o644); err != nil {
					return fmt.Sprintf("写入失败: %v", err)
				}
				return fmt.Sprintf("已写入 %s", rel(root, fp))
			},
		},
		{
			Name: "run_lint", Description: "运行 lint 健康检查(断链/孤儿)",
			Parameters: obj(map[string]any{}),
			Func:       func(map[string]any) string { return wiki.ReportLint(root) },
		},
		{
			Name: "ask_user", Description: "策展决策点向用户提问并等待裁决(如:是否值得建概念页/是否 filed back)。人策展>自动,拿不准就调用。",
			Parameters: obj(map[string]any{"question": strProp}),
			Func: func(args map[string]any) string {
				q := str(args, "question")
				if ask == nil {
					return "[非交互] 无用户可问。按 SCHEMA 纪律自行决策:单次提及不建页、2+ 源才建概念/实体页。"
				}
				return ask(q)
			},
		},
	}
}

// ---- 工具辅助 ----

var linkRe = regexp.MustCompile(`\[\[([^\]|]+)(?:\|[^\]]+)?\]\]`)

func obj(props map[string]any) map[string]any {
	return map[string]any{"type": "object", "properties": props}
}

var (
	strProp = map[string]any{"type": "string"}
	intProp = map[string]any{"type": "integer"}
)

func str(args map[string]any, k string) string {
	if v, ok := args[k].(string); ok {
		return v
	}
	return ""
}

func intArg(args map[string]any, k string, def int) int {
	switch v := args[k].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// safeJoin 保证路径在允许子目录内,防路径穿越。
func safeJoin(root, path string, allowed ...string) (string, bool) {
	fp, err := filepath.Abs(filepath.Join(root, filepath.Clean(path)))
	if err != nil {
		return "", false
	}
	if !strings.HasPrefix(fp, filepath.Clean(root)+string(os.PathSeparator)) {
		return "", false
	}
	rel := strings.TrimPrefix(fp, filepath.Clean(root)+string(os.PathSeparator))
	top := strings.SplitN(rel, string(os.PathSeparator), 2)[0]
	for _, a := range allowed {
		if top == a {
			return fp, true
		}
	}
	return "", false
}

func readSafe(root, path string) string {
	fp, ok := safeJoin(root, path, "wiki", "raw", "examples", "SCHEMA.md", "templates", "docs")
	if !ok {
		return "拒绝:路径超出项目根或不在白名单目录。"
	}
	b, err := os.ReadFile(fp)
	if err != nil {
		return fmt.Sprintf("读取失败: %v", err)
	}
	return string(b)
}

func rel(root, fp string) string {
	r, _ := filepath.Rel(root, fp)
	return r
}
