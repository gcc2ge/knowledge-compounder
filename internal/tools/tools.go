// Package tools 把知识库操作暴露成可调用工具(M06 工具系统)。
// 全部 Go 原生实现,不依赖 Python 子进程。写文件只允许 wiki/raw/examples 内。
package tools

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gcc2ge/knowledge-compounder/internal/index"
	"github.com/gcc2ge/knowledge-compounder/internal/provider"
	"github.com/gcc2ge/knowledge-compounder/internal/retrieval"
	"github.com/gcc2ge/knowledge-compounder/internal/wiki"
)

// Build 构造工具列表。root 为项目根;ask 为策展决策回调(向用户提问等裁决),nil 则 ask_user 返回非交互提示;
// opts 为检索选项(embedder/缓存/条数),空则 search_wiki 退回词法检索。
// 返回工具列表、内存索引(KCP_INDEX=off 时索引为 nil,工具降级走全扫旧路径)与读覆盖台账
// (分页读取的确定性记录,compiler 退出前查长源中间 gap——防「读头尾跳过中间」的浅页)。
// 调用方在进程退出前调 idx.Save(root) 把增量留给下次。
func Build(root string, ask func(string) string, vision func(string) string, opts retrieval.Options) ([]provider.ToolDef, *index.Index, *Coverage) {
	idx := index.Load(root)
	readsSinceWrite := map[string]int{} // 同文件连续分页读计数:落盘纪律的机械执行(见 read_file)
	cov := &Coverage{}                  // 读覆盖台账:gap 检测的确定性依据
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
				if idx != nil {
					return idx.FormatMentions(idx.Mentions(terms)) // 三档话术 + 子串/短词护栏(带页名判据)
				}
				// 降级:逐 term 全扫 source 页
				files, _ := filepath.Glob(filepath.Join(root, "wiki", "sources", "*.md"))
				var texts []struct{ slug, low string }
				for _, f := range files {
					if b, err := os.ReadFile(f); err == nil {
						texts = append(texts, struct{ slug, low string }{strings.TrimSuffix(filepath.Base(f), ".md"), strings.ToLower(string(b))})
					}
				}
				var results []index.MentionResult
				for _, t := range terms {
					var hits []string
					for _, te := range texts {
						if strings.Contains(te.low, strings.ToLower(t)) {
							hits = append(hits, te.slug)
						}
					}
					results = append(results, index.MentionResult{Term: t, Hits: hits})
				}
				return (&index.Index{}).FormatMentions(results) // 无索引:仅跳过页名子串检查
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
			Name: "read_file", Description: "读取项目内文件(相对项目根)。支持分页:offset(起始行,从 1 起)/limit(行数)返回带行号片段与总行数提示;不传参数读全文,超过 80KB 的大文件返回头部并要求分页——编译长源时逐页读完,禁止只凭开头",
			Parameters: obj(map[string]any{"path": strProp, "offset": intProp, "limit": intProp}),
			Func: func(args map[string]any) string {
				path := str(args, "path")
				// 图片文件:不能按文本读,给确定性指引走 describe_image(Tier 2 vision)
				if p, ok := safeJoin(root, path, "raw"); ok && isImageExt(p) {
					if fi, err := os.Stat(p); err == nil {
						return fmt.Sprintf("图片文件 %s(%d KB)。不能按文本读取;请用 describe_image 工具让视觉模型描述其内容,再把描述编译进页面。", path, fi.Size()>>10)
					}
				}
				offset, limit := intArg(args, "offset", 0), intArg(args, "limit", 0)
				out := readPaged(root, path, offset, limit)
				markRead(cov, root, path, offset, limit) // 读覆盖台账:gap 检测依据
				// 落盘纪律的机械执行:同文件连续分页读 ≥3 页未写任何文件时,
				// 在返回文本里插入强制提醒——实测便宜模型会连读 13 页,上下文压缩
				// 把早期内容折叠掉,读完已无料可写。纪律进工具返回,不靠 prompt 自觉。
				if offset > 0 || limit > 0 {
					readsSinceWrite[path]++
					if readsSinceWrite[path] >= 3 {
						out += fmt.Sprintf("\n\n⚠️ 你已连续读 %d 页未落盘。先停下:把已读段落的要点 write_file 进源页草稿,再继续读后面的页——否则上下文压缩会吃掉早期内容。", readsSinceWrite[path])
					}
				}
				return out
			},
		},
		{
			Name: "web_fetch", Description: "抓取 http(s) URL 并转为纯文本(去 HTML 标签,截 8KB)——验证外部主张、查作者/工具背景用;非文本或不可达时返回错误说明",
			Parameters: obj(map[string]any{"url": strProp}),
			Func:       func(args map[string]any) string { return webFetch(str(args, "url")) },
		},
		{
			Name: "web_search", Description: "搜索网页(零 key,DuckDuckGo HTML,返回 5 条标题/URL/摘要)——验证外部主张、查作者/工具背景用;要正文时再用 web_fetch 抓具体 URL",
			Parameters: obj(map[string]any{"query": strProp}),
			Func:       func(args map[string]any) string { return webSearch(str(args, "query")) },
		},
		{
			Name: "describe_image", Description: "让视觉模型描述 raw/ 里的一张图片(架构图/截图/抓图,传相对路径)。返回的描述文本可直接编译进页面;模型无视觉能力时返回错误说明",
			Parameters: obj(map[string]any{"path": strProp}),
			Func: func(args map[string]any) string {
				path := str(args, "path")
				if vision == nil {
					return "[非交互] 未配置视觉描述能力。按 SCHEMA 图片源无法自动编译:在 source 页注明「含图片 N 张,待人工补充描述」,或换多模态模型后重跑。"
				}
				fp, ok := safeJoin(root, path, "raw") // 只允许描述 raw/ 下图片(raw 不可变,不可写)
				if !ok {
					return "拒绝:describe_image 只允许描述 raw/ 下的图片,收到 " + path
				}
				if !isImageExt(fp) {
					return "不是图片文件(png/jpg/jpeg/gif/webp/bmp): " + path
				}
				return vision(fp)
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
				delete(readsSinceWrite, path) // 落盘即解除落盘提醒
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
	}, idx, cov
}

// markRead 把一次 read_file 的覆盖区间记入台账(与 readPaged 同判据)。
// 全文读:≤80KB 记全覆盖;>80KB 只记返回的头部段。分页读:记 [offset, offset+limit-1]。
func markRead(cov *Coverage, root, path string, offset, limit int) {
	fp, ok := safeJoin(root, path, "wiki", "raw", "examples", "SCHEMA.md", "templates", "docs")
	if !ok {
		return
	}
	b, err := os.ReadFile(fp)
	if err != nil {
		return
	}
	total := strings.Count(string(b), "\n") + 1
	if offset <= 0 && limit <= 0 {
		if len(b) <= readWholeCap {
			cov.Mark(path, 1, total, total)
		} else {
			headLines := strings.Count(string(b[:readWholeCap]), "\n") + 1
			cov.Mark(path, 1, headLines, total)
		}
		return
	}
	if offset <= 0 {
		offset = 1
	}
	if limit <= 0 || limit > pageLineCap {
		limit = pageLineCap
	}
	cov.Mark(path, offset, offset+limit-1, total)
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

const (
	readWholeCap = 80 << 10 // 全读上限 80KB;超过强制分页,防大文件一次挤爆上下文
	pageLineCap  = 2500     // 单页行数上限(对齐 Claude Read 的 2000 行语义,防一页过巨)
	pageBytesCap = 120 << 10
)

// readPaged 读取文件;offset(1 起始行)/limit 给定时按 cat -n 格式返回片段并附总行数与续读提示。
// 语义对齐 Claude Code 的 Read:文件内容经工具结果进上下文(一次),而不是塞进初始消息反复重发。
// 单页受行数与字节双闸限制——实测一次性 4000 行请求单条工具结果 ~150KB,足以毒化小上下文模型。
func readPaged(root, path string, offset, limit int) string {
	fp, ok := safeJoin(root, path, "wiki", "raw", "examples", "SCHEMA.md", "templates", "docs")
	if !ok {
		return "拒绝:路径超出项目根或不在白名单目录。"
	}
	b, err := os.ReadFile(fp)
	if err != nil {
		return fmt.Sprintf("读取失败: %v", err)
	}
	if offset <= 0 && limit <= 0 {
		if len(b) <= readWholeCap {
			return string(b)
		}
		return fmt.Sprintf("%s\n…(文件 %d KB / %d 行,超过单次读取上限;请用 offset/limit 分页读取,如 offset=1 limit=2000,逐页读完)\n",
			b[:readWholeCap], len(b)>>10, strings.Count(string(b), "\n")+1)
	}
	lines := strings.Split(string(b), "\n")
	if offset <= 0 {
		offset = 1
	}
	if offset > len(lines) {
		return fmt.Sprintf("offset %d 超出总行数 %d。", offset, len(lines))
	}
	if limit <= 0 || limit > pageLineCap {
		limit = pageLineCap
	}
	end := len(lines)
	if offset+limit-1 < end {
		end = offset + limit - 1
	}
	// 字节闸:超出单页字节上限时提前截断该页
	var sb strings.Builder
	cut := end
	for i := offset; i <= end; i++ {
		sb.WriteString(lines[i-1])
		sb.WriteByte('\n')
		if sb.Len() > pageBytesCap {
			cut = i - 1
			sb.Reset()
			break
		}
	}
	if cut < end {
		end = cut
		sb.Reset()
		for i := offset; i <= end; i++ {
			fmt.Fprintf(&sb, "%6d\t%s\n", i, lines[i-1])
		}
		fmt.Fprintf(&sb, "(第 %d–%d 行触达单页字节上限,共 %d 行;继续读:offset=%d)", offset, end, len(lines), end+1)
		return sb.String()
	}
	sb.Reset()
	for i := offset; i <= end; i++ {
		fmt.Fprintf(&sb, "%6d\t%s\n", i, lines[i-1])
	}
	if end < len(lines) {
		fmt.Fprintf(&sb, "(第 %d–%d 行,共 %d 行;继续读:offset=%d)", offset, end, len(lines), end+1)
	} else {
		fmt.Fprintf(&sb, "(第 %d–%d 行,共 %d 行;已到文件末尾)", offset, end, len(lines))
	}
	return sb.String()
}

// ---- web_fetch:纯标准库 HTTP 抓取 → 粗粒度 HTML 转文本 ----

var httpClient = &http.Client{Timeout: 15 * time.Second}

func webFetch(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "拒绝:仅支持 http/https URL。"
	}
	resp, err := httpClient.Get(rawURL)
	if err != nil {
		return fmt.Sprintf("抓取失败: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Sprintf("HTTP %s(%s)", resp.Status, rawURL)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/") && !strings.Contains(ct, "json") && !strings.Contains(ct, "xml") {
		return fmt.Sprintf("非文本内容(Content-Type: %s),无法转纯文本。", ct)
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512<<10))
	text := htmlToText(string(body))
	const cap = 8 << 10
	if len(text) > cap {
		text = text[:cap] + "\n…(截断)"
	}
	return fmt.Sprintf("%s\n\n(来源: %s)", text, rawURL)
}

var (
	scriptRe = regexp.MustCompile(`(?is)<(?:script|style|noscript)[^>]*>.*?</(?:script|style|noscript)>`) // RE2 无反向引用,开闭标签组就近匹配,足够剔除
	tagRe    = regexp.MustCompile(`(?s)<[^>]*>`)
	blankRe  = regexp.MustCompile(`[ \t]*\n[ \t\n]*`)
)

// htmlToText 去 script/style → 去标签 → 解码常见实体 → 压缩空白。够用即可,非完整解析器。
func htmlToText(s string) string {
	s = scriptRe.ReplaceAllString(s, " ")
	s = tagRe.ReplaceAllString(s, " ")
	s = strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`,
		"&#39;", "'", "&#x27;", "'", "&#34;", `"`, "&nbsp;", " ").Replace(s)
	return strings.TrimSpace(blankRe.ReplaceAllString(s, "\n"))
}

func rel(root, fp string) string {
	r, _ := filepath.Rel(root, fp)
	return r
}

// ---- web_search:零 key DuckDuckGo HTML 搜索(粗解析,够验证主张即可) ----

var (
	ddgTitleRe = regexp.MustCompile(`(?is)<a[^>]*class="result__a"[^>]*href="([^"]*)"[^>]*>(.*?)</a>`)
	ddgSnipRe  = regexp.MustCompile(`(?is)<a[^>]*class="result__snippet"[^>]*>(.*?)</a>`)
)

type ddgResult struct{ title, url, snippet string }

func webSearch(query string) string {
	if strings.TrimSpace(query) == "" {
		return "query 必填。"
	}
	u := "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(strings.TrimSpace(query))
	resp, err := httpClient.Get(u)
	if err != nil {
		return fmt.Sprintf("搜索失败: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Sprintf("搜索 HTTP %s", resp.Status)
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	results := parseDDGResults(string(body))
	if len(results) == 0 {
		return "未搜到结果(网络/反爬)。"
	}
	var b strings.Builder
	for i, r := range results {
		fmt.Fprintf(&b, "%d. %s\n   %s\n   %s\n", i+1, r.title, r.snippet, r.url)
	}
	return strings.TrimSpace(b.String())
}

func parseDDGResults(html string) []ddgResult {
	titleMatches := ddgTitleRe.FindAllStringSubmatch(html, -1)
	snips := ddgSnipRe.FindAllStringSubmatch(html, -1)
	var out []ddgResult
	for i, m := range titleMatches {
		if i >= 5 {
			break
		}
		r := ddgResult{
			title: collapseWS(htmlToText(m[2])),
			url:   decodeDDGURL(m[1]),
		}
		if i < len(snips) {
			r.snippet = collapseWS(htmlToText(snips[i][1]))
		}
		if r.title != "" {
			out = append(out, r)
		}
	}
	return out
}

// decodeDDGURL DDG 结果 href 是跳转 URL(//duckduckgo.com/l/?uddg=…),解出真实地址。
func decodeDDGURL(href string) string {
	if u, err := url.Parse(href); err == nil {
		if q := u.Query().Get("uddg"); q != "" {
			return q
		}
	}
	switch {
	case strings.HasPrefix(href, "//"):
		return "https:" + href
	case strings.HasPrefix(href, "/"):
		return "https://duckduckgo.com" + href
	}
	return href
}

// isImageExt 图片扩展名判据(read_file 指引走 describe_image 用)。
func isImageExt(path string) bool {
	switch strings.ToLower(filepathExt(path)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp":
		return true
	}
	return false
}

// collapseWS 折叠连续空白为单空格(HTML 高亮 <b> 剥掉后残留双空格)。
func collapseWS(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// filepathExt 取小写扩展名(含点)。
func filepathExt(path string) string {
	if i := strings.LastIndexByte(path, '.'); i >= 0 {
		return path[i:]
	}
	return ""
}
