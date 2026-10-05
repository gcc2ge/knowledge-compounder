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

	"github.com/gcc2ge/knowledge-compounder/internal/index"
	"github.com/gcc2ge/knowledge-compounder/internal/provider"
	"github.com/gcc2ge/knowledge-compounder/internal/retrieval"
	"github.com/gcc2ge/knowledge-compounder/internal/wiki"
)

// Build 构造工具列表。root 为项目根;ask 为策展决策回调(向用户提问等裁决),nil 则 ask_user 返回非交互提示;
// opts 为检索选项(embedder/缓存/条数),空则 search_wiki 退回词法检索。
// 返回工具列表与内存索引(KCP_INDEX=off 时索引为 nil,工具降级走全扫旧路径);
// 调用方在进程退出前调 idx.Save(root) 把增量留给下次。
func Build(root string, ask func(string) string, opts retrieval.Options) ([]provider.ToolDef, *index.Index) {
	idx := index.Load(root)
	return []provider.ToolDef{
		{
			Name: "wiki_status", Description: "查看知识库状态:页面计数 + 未编译 raw",
			Parameters: obj(map[string]any{}),
			Func:       func(map[string]any) string { return wiki.StatusText(root) },
		},
		{
			Name: "search_wiki", Description: "在知识库中检索相关页面(混合检索:词法 + 可选语义向量;结果带私有度徽标 [A]公开/[B]精选综合/[C]私有)",
			Parameters: obj(map[string]any{"query": strProp, "k": intProp}),
			Func: func(args map[string]any) string {
				query := str(args, "query")
				k := intArg(args, "k", 0)
				if k > 0 {
					opts.Top = k
				}
				var results []retrieval.Result
				if idx != nil {
					results = idx.Rank(query, opts)
				} else {
					results = wiki.Retrieve(root, query, opts)
				}
				if len(results) == 0 {
					return "知识库中未找到相关页面。"
				}
				var b strings.Builder
				for _, r := range results {
					badge := wiki.EvidenceBadge(root, r.Path)
					fmt.Fprintf(&b, "- [[%s]] %s 命中%.2f: %s\n", r.Label, badge, r.Score, r.Summary)
				}
				return b.String()
			},
		},
		{
			Name: "get_page", Description: "读取知识库中的一个 wiki 页面(slug 或文件名)",
			Parameters: obj(map[string]any{"page": strProp}),
			Func: func(args map[string]any) string {
				page := str(args, "page")
				var text, dir string
				var ok bool
				if idx != nil {
					text, dir, ok = idx.GetPage(page)
				} else {
					text, dir, ok = wiki.GetPage(root, page)
				}
				if !ok {
					return fmt.Sprintf("[[%s]] 不存在。", strings.TrimSuffix(page, ".md"))
				}
				return fmt.Sprintf("## %s (%s/)\n\n%s", strings.TrimSuffix(page, ".md"), dir, text)
			},
		},
		{
			Name: "wiki_mentions", Description: "统计术语被几个已编译源页提及——「2+ 源提及才建概念/实体页」纪律的确定性判据。支持批量:terms 传数组一次查多个候选(推荐);单数 term 也兼容。每项返回提及计数与建页结论",
			Parameters: obj(map[string]any{
				"term":  strProp,
				"terms": map[string]any{"type": "array", "items": strProp},
			}),
			Func: func(args map[string]any) string {
				var terms []string
				if arr, ok := args["terms"].([]any); ok {
					for _, v := range arr {
						if s, ok := v.(string); ok && s != "" {
							terms = append(terms, s)
						}
					}
				}
				if t := str(args, "term"); t != "" {
					terms = append(terms, t)
				}
				if len(terms) == 0 {
					return "term/terms 至少提供一个。"
				}
				var results []index.MentionResult
				if idx != nil {
					results = idx.Mentions(terms)
				} else {
					// 降级:逐 term 全扫 source 页
					files, _ := filepath.Glob(filepath.Join(root, "wiki", "sources", "*.md"))
					var texts []struct{ slug, low string }
					for _, f := range files {
						if b, err := os.ReadFile(f); err == nil {
							texts = append(texts, struct{ slug, low string }{strings.TrimSuffix(filepath.Base(f), ".md"), strings.ToLower(string(b))})
						}
					}
					for _, t := range terms {
						var hits []string
						for _, te := range texts {
							if strings.Contains(te.low, strings.ToLower(t)) {
								hits = append(hits, te.slug)
							}
						}
						results = append(results, index.MentionResult{Term: t, Hits: hits})
					}
				}
				var b strings.Builder
				for _, r := range results {
					switch n := len(r.Hits); n {
					case 0:
						fmt.Fprintf(&b, "「%s」未被任何源页提及。\n", r.Term)
					case 1:
						fmt.Fprintf(&b, "「%s」仅 1 个源提及(%s)——单次提及,进该源「术语」节,不建页。\n", r.Term, r.Hits[0])
					default:
						fmt.Fprintf(&b, "「%s」被 %d 个源提及(%s)——≥2,满足建页纪律,直接创建/更新对应页面。\n", r.Term, n, strings.Join(r.Hits, "、"))
					}
				}
				return strings.TrimSuffix(b.String(), "\n")
			},
		},
		{
			Name: "backlinks", Description: "查一个页面的双向链接:谁链向它(入站)/它链向谁(出站,断链单独标出)。知识网络的结构化导航",
			Parameters: obj(map[string]any{"page": strProp}),
			Func: func(args map[string]any) string {
				slug := strings.TrimSuffix(str(args, "page"), ".md")
				if slug == "" {
					return "page 必填。"
				}
				if idx != nil {
					in, out := idx.Backlinks(slug)
					var b strings.Builder
					fmt.Fprintf(&b, "[[%s]] 入站 %d 条:", slug, len(in))
					if len(in) == 0 {
						b.WriteString(" 无(孤儿页)")
					} else {
						b.WriteString(" " + strings.Join(in, "、"))
					}
					var okLinks, broken []string
					for _, o := range out {
						if idx.HasSlug(o) {
							okLinks = append(okLinks, o)
						} else {
							broken = append(broken, o)
						}
					}
					fmt.Fprintf(&b, "\n出站 %d 条: %s", len(out), strings.Join(okLinks, "、"))
					if len(broken) > 0 {
						fmt.Fprintf(&b, "\n断链: %s", strings.Join(broken, "、"))
					}
					return b.String()
				}
				// 降级:读文件解析
				text, _, ok := wiki.GetPage(root, slug)
				if !ok {
					return fmt.Sprintf("页面 [[%s]] 不存在。", slug)
				}
				links := wiki.ParseWikilinks(text)
				return fmt.Sprintf("[[%s]] 出站 %d 条: %s\n(入站查询需索引;当前为降级模式)", slug, len(links), strings.Join(links, "、"))
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
			Name: "write_file", Description: "写入 wiki/ examples 内的文件(编译写页面用);raw/ 不可变,禁止写入",
			Parameters: obj(map[string]any{"path": strProp, "content": strProp}),
			Func: func(args map[string]any) string {
				path, content := str(args, "path"), str(args, "content")
				fp, ok := safeJoin(root, path, "wiki", "examples")
				if !ok {
					return fmt.Sprintf("拒绝:只允许写 wiki/ examples/(raw/ 不可变,禁止写入),收到 %s", path)
				}
				if err := os.MkdirAll(filepath.Dir(fp), 0o755); err != nil {
					return fmt.Sprintf("写入失败: %v", err)
				}
				if err := os.WriteFile(fp, []byte(content), 0o644); err != nil {
					return fmt.Sprintf("写入失败: %v", err)
				}
				if idx != nil {
					idx.UpdateFile(fp) // 写入即索引:同会话后续检索立即可见
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
	}, idx
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
