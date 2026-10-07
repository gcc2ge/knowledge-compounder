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
func Build(root string, ask func(string) string, vision func(string) string, opts retrieval.Options) ([]provider.ToolDef, *index.Index, *Coverage, *ContradictionLog) {
	idx := index.Load(root)
	readsSinceWrite := map[string]int{} // 同文件连续分页读计数:落盘纪律的机械执行(见 read_file)
	cov := &Coverage{}                  // 读覆盖台账:gap 检测的确定性依据
	rep := &RepeatDetector{}            // 重复段台账:模板化长源选择性深读的确定性依据(切片 B)
	cLog := &ContradictionLog{}         // 矛盾核对台账:收尾是否回头对照 wiki 的确定性记录
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
				out := readPaged(root, path, offset, limit, rep)
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
			Name:        "check_contradictions",
			Description: "编译收尾必做核对:读取你刚编译的源页(source=源页 slug,去 .md),把它的关键声明(一句话结论+论证链)与整个 wiki 已有页面比对,返回相关页——逐个判断 冲突/佐证/无涉:冲突用 lint 可识别的标注(⚠️ 冲突:/⚠️ 矛盾:/<!-- CONTRADICTION -->)显式落盘,并把冲突写进相关概念页「张力与缺口」;佐证/相关链进「连接」并写关联意义。直接收尾不跑本工具会被 FinishGuard 强制补做",
			Parameters:  obj(map[string]any{"source": strProp, "k": intProp}),
			Func: func(args map[string]any) string {
				slug := strings.TrimSuffix(str(args, "source"), ".md")
				if slug == "" {
					return "source 必填(你刚编译的源页 slug,去 .md)。"
				}
				fp := filepath.Join(root, "wiki", "sources", slug+".md")
				b, err := os.ReadFile(fp)
				if err != nil {
					return fmt.Sprintf("源页 wiki/sources/%s.md 还不存在——先用 write_file 落盘源页(至少 frontmatter+一句话结论+论证链),再调用本工具核对。", slug)
				}
				hits := ScanContradictions(root, slug, string(b), intArg(args, "k", 0))
				cLog.Mark(slug, pageLabels(hits))
				cLog.MarkConcepts(slug, conceptLabels(hits)) // evidenceLog:扫出的概念页需回流裁决
				var sb strings.Builder
				fmt.Fprintf(&sb, "## 矛盾核对:源页 × 已有 wiki 页(%d 个相关)\n\n", len(hits))
				if len(hits) == 0 {
					sb.WriteString("未找到与本源明显相关的已有页面——这是合法结果,可省略矛盾标注;概念/实体的建页纪律照旧(wiki_mentions 核对)。\n")
				} else {
					sb.WriteString("逐个判断 冲突/佐证/无涉,并按下面落盘要求写进「连接」:\n\n")
					for i, h := range hits {
						badge := wiki.EvidenceBadge(root, h.Result.Path)
						fmt.Fprintf(&sb, "%d. [[%s]] %s 相关%.2f\n   相关声明: %s\n   摘要: %s\n",
							i+1, h.Result.Label, badge, h.Result.Score,
							truncate(h.Claim, 90), truncate(h.Result.Summary, 140))
					}
					sb.WriteString("\n落盘要求(标注格式固定,改动会被 postCompileQA 报警):\n")
					sb.WriteString("- 冲突(论点直接对立:同一概念两种分类 / 结论相反 / 口径不一致):两边都保留。本源页「连接」写 `- ⚠️ 冲突: [[相关页]] — 冲突原因`;同时把冲突写进相关概念页「张力与缺口」一条 `- ⚠️ 冲突: 本源 [[本源页]] — 冲突原因`。冲突不消除,只标注。\n")
					sb.WriteString("- 佐证/相关:本源页「连接」写 `- → [[相关页]] — 佐证:关联意义`,并写清这页关联对用户意味着什么。\n")
					sb.WriteString("- 无涉:不用链,建页纪律照旧。\n")
					sb.WriteString("冲突≠佐证:仅在论点层面直接对立才算冲突(如两种 taxonomy 并存、同一断言两个版本);单纯语义/细节互补是佐证。\n")
				}
				return sb.String()
			},
		},
		{
			Name: "update_evidence", Description: "编译收尾的确定性回流:把本次编译结论写回既有概念页——正向复利(既有页必须被物理强化,不能只在新源页「连接」写一行)。slug=概念页,action=corroborate(佐证:追加源+机器重算 confidence+「外部观点」加佐证行)|contradict(冲突:「张力与缺口」落 ⚠️ 冲突 标注)|skip(无涉:仅登记已裁决,不写文件),source=源页 slug,claim=一句话原因/佐证内容。仅限 wiki/concepts/ 页面;compiler 在 check_contradictions 之后对每个相关概念页逐一调用——FinishGuard 会校验每个相关概念页都被裁决(corroborate/contradict/skip),漏掉会强制续跑",
			Parameters: obj(map[string]any{
				"slug": strProp, "action": strProp, "source": strProp, "claim": strProp,
			}),
			Func: func(args map[string]any) string {
				slug := strings.TrimSuffix(str(args, "slug"), ".md")
				source := strings.TrimSuffix(str(args, "source"), ".md")
				action, claim := str(args, "action"), str(args, "claim")
				if slug == "" || action == "" {
					return "slug 与 action(corroborate|contradict|skip)必填。"
				}
				if action == "skip" {
					cLog.Applied(source, slug) // 无涉裁决:只登记,不写文件
					return "已记录: 概念页 [[" + slug + "]] 判定无涉,不回流。"
				}
				msg, err := wiki.ApplyEvidence(root, slug, action, source, claim)
				if err != nil {
					return "失败: " + err.Error()
				}
				cLog.Applied(source, slug) // evidenceLog:该概念页已回流裁决
				if idx != nil {
					if p, ok := wiki.ResolveSlug(root, slug); ok {
						idx.UpdateFile(p) // 写即索引:同会话后续检索立即可见
					}
				}
				return msg
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
			Name: "write_file", Description: "写入 wiki/ examples 内的文件(编译写页面用)。⚠️ 覆盖语义:整页替换,不是追加——写新页/整页重写时用,重写前必须先 read_file 读回当前内容;增量补写/追加一律用 edit_file。raw/ 不可变,禁止写入",
			Parameters: obj(map[string]any{"path": strProp, "content": strProp}),
			Func: func(args map[string]any) string {
				path, content := str(args, "path"), str(args, "content")
				if path == "" {
					return "拒绝:未收到 path 参数(工具参数解析失败)。write_file 必须以 JSON 传 path(如 wiki/sources/xxx.md)与 content;content 内的换行要转义为 \\n、引号要转义为 \\\"。"
				}
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
				if tr, _ := args["content_truncated"].(bool); tr {
					// 参数被模型输出截断(无完整收尾):写下的内容必然不全。必须明说,
					// 否则模型以为整页已落盘,靠 FinishGuard 的 preflight 兜底才发现缺节、
					// 反复读-write 测试。且截断时模型多尝试「整页覆盖重试」→ 必然再次截断。
					// 正确路径是 read_file 读回 + edit_file 逐块补写,不靠整页重写。
					return fmt.Sprintf("已写入 %s ⚠️ 注意:本次 content 参数疑似被模型输出截断(未闭合),只写入了前 %d 行,页面不完整。不要再整页 write_file 覆盖(会再次截断);请 read_file 读回当前内容,再用 edit_file 逐块补写,每块 <3KB。", rel(root, fp), strings.Count(content, "\n")+1)
				}
				return fmt.Sprintf("已写入 %s", rel(root, fp))
			},
		},
		{
			Name: "edit_file", Description: "修改 wiki/ examples 内文件的内容(增量补写用;write_file 是整页覆盖,不用于追加)。old_string=当前文件里的唯一原文锚点(必须与 read_file 读回的原文逐字一致,含缩进/换行),new_string=替换后的内容。精确替换一次:old_string 找不到或出现多次时不改并返回说明。**写长页面纪律(防输出截断)**:先 write_file 写第一块骨架,再 read_file 读回确认,然后 edit_file 逐块填充——old_string 取**要补写的那一节当前结尾**的唯一原文锚点(不是无脑文件尾),new_string=锚点原文+新增块,每块 <3KB;**已存在的节标题(## 意外发现 等)绝不重复输出,每节只写一次**;绝不要整页 write_file 覆盖重试,超长 content 会被模型输出截断、只写一半。raw/ 不可变,禁止编辑",
			Parameters: obj(map[string]any{"path": strProp, "old_string": strProp, "new_string": strProp}),
			Func: func(args map[string]any) string {
				path, old, new := str(args, "path"), str(args, "old_string"), str(args, "new_string")
				// 截断判断放最前:repair 失败(截断到无收尾引号)会解析出空参数,此时缺字段
				// 是截断的果不是因——先报截断,模型才知道要分块,而不是对着假缺字段空转。
				if tr, _ := args["truncated"].(bool); tr {
					return "拒绝:本次 edit_file 参数疑似被模型输出截断(对象未以 } 收尾),未做任何修改。new_string 请保持 <3KB,分块编辑(每块一个 edit_file 调用)。"
				}
				if path == "" || old == "" {
					// 形状感知的拒绝:区分「只传了新内容」vs「全缺」,给可执行的字段顺序。
					// 弱模型偶尔把 new_string 放最前、或干脆只发 new_string——此时 path/old_string
					// 必然拿不到(短字段在长内容之后或根本没发),无法定位替换位置。
					if str(args, "new_string") != "" {
						return "拒绝:你这次只传了 new_string,丢了 path 与 old_string——无法定位要替换的位置。edit_file 三参必填,JSON 键顺序固定为 path → old_string → new_string(new_string 是新增内容,放最后);对象必须以 } 收尾。请按此顺序重发。"
					}
					return "拒绝:path 与 old_string 必填。old_string 必须是当前文件里的唯一原文锚点(先 read_file 读回确认),new_string 为替换内容。"
				}
				fp, ok := safeJoin(root, path, "wiki", "examples")
				if !ok {
					return fmt.Sprintf("拒绝:只允许编辑 wiki/ examples/(raw/ 不可变,禁止写入),收到 %s", path)
				}
				b, err := os.ReadFile(fp)
				if err != nil {
					return fmt.Sprintf("编辑失败:%s 不存在——先 write_file 建页(首块),再 edit_file 补写。", rel(root, fp))
				}
				text := string(b)
				switch n := strings.Count(text, old); {
				case n == 0:
					return fmt.Sprintf("编辑失败:old_string 在 %s 中未找到。锚点必须与文件当前内容逐字一致(含缩进/换行)——先 read_file 读回确认再编辑。", rel(root, fp))
				case n > 1:
					return fmt.Sprintf("编辑失败:old_string 在 %s 中出现 %d 次,锚点不唯一。请用更长/更靠文件尾的锚点,或包含上文数行使之一一唯一。", rel(root, fp), n)
				}
				text = strings.Replace(text, old, new, 1)
				if err := os.WriteFile(fp, []byte(text), 0o644); err != nil {
					return fmt.Sprintf("编辑失败: %v", err)
				}
				delete(readsSinceWrite, path)
				if idx != nil {
					idx.UpdateFile(fp) // 写即索引:同会话后续检索立即可见
				}
				return fmt.Sprintf("已编辑 %s:替换 1 处(%d 字符→%d 字符)。若继续补写:read_file 读回目标节,用该节当前结尾的唯一原文作锚点插入(已存在的节标题绝不重复输出)。", rel(root, fp), len(old), len(new))
			},
		},
		{
			Name: "run_lint", Description: "运行 lint 健康检查(断链/孤儿)",
			Parameters: obj(map[string]any{}),
			Func:       func(map[string]any) string { return wiki.ReportLint(root) },
		},
		{
			Name: "filed_back", Description: "query 角色把有持久价值的跨页综合答案落盘为 wiki/synthesis/ 永久页(query-as-contribution)。question=触发问题,title=页面名(中文简短,不含 /),summary=一句话摘要(可空),analysis=正文分析,evidence=证据与张力(每条标私有度 A/B/C),unresolved=未决问题(不能留空),conclusion=结论,sources=支撑源 slug 列表(逗号分隔,须 ≥2 个已存在页——单页答案不值得落盘)",
			Parameters: obj(map[string]any{
				"question": strProp, "title": strProp, "summary": strProp,
				"analysis": strProp, "evidence": strProp, "unresolved": strProp,
				"conclusion": strProp, "sources": strProp,
			}),
			Func: func(args map[string]any) string { return filedBack(root, args, idx) },
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
	}, idx, cov, cLog
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

// filedBack 把 query 的综合答案确定性落盘为 wiki/synthesis/ 永久页(模板校验 + 幂等防覆盖)。
// query-as-contribution 的机械执行:跨页综合(≥2 源)才允许落盘,未决问题不能留空,
// 已存在同名页拒绝覆盖——保证 synthesis 是收敛的知识产品而不是答案垃圾场。
func filedBack(root string, args map[string]any, idx *index.Index) string {
	question := str(args, "question")
	title := strings.TrimSpace(str(args, "title"))
	if question == "" || title == "" {
		return "question 与 title 必填。"
	}
	if strings.ContainsAny(title, `/\<>:"|?*`) {
		return "title 含非法路径字符(/\\<>:\"|?*),请用简短中文文件名。"
	}
	var sources []string
	for _, s := range strings.Split(str(args, "sources"), ",") {
		s = strings.TrimSpace(strings.TrimSuffix(s, ".md"))
		if s != "" {
			sources = append(sources, s)
		}
	}
	if len(sources) < 2 {
		return "sources 须 ≥2 个支撑页(单页答案不值得落盘为 synthesis),当前 " + strconv.Itoa(len(sources)) + " 个。"
	}
	for _, s := range sources {
		if _, ok := wiki.ResolveSlug(root, s); !ok {
			return "支撑页 [[" + s + "]] 不存在——先核实页面再落盘。"
		}
	}
	unresolved := strings.TrimSpace(str(args, "unresolved"))
	if unresolved == "" {
		return "unresolved(未决问题)不能留空——按 SCHEMA,未收敛的分析必须显式写出未解决之处,不替你脑补结论。"
	}
	fp := filepath.Join(root, "wiki", "synthesis", title+".md")
	if _, err := os.Stat(fp); err == nil {
		return "已存在 wiki/synthesis/" + title + ".md——不要覆盖,给 title 加区分前缀后重试。"
	}
	var sb strings.Builder
	sb.WriteString("---\ntype: synthesis\ntrigger: query\ncreated: " + time.Now().Format("2006-01-02") + "\nquestion: \"" + question + "\"\n---\n\n")
	sb.WriteString("# " + title + "\n\n## 一句话摘要\n" + str(args, "summary") + "\n\n## 问题\n" + question + "\n\n## 分析\n" + str(args, "analysis") + "\n\n## 证据与张力\n" + str(args, "evidence") + "\n\n## 未决问题\n" + unresolved + "\n\n## 结论\n" + str(args, "conclusion") + "\n\n## 来源\n")
	for _, s := range sources {
		sb.WriteString("- [[" + s + "]]\n")
	}
	if err := os.MkdirAll(filepath.Dir(fp), 0o755); err != nil {
		return "写入失败: " + err.Error()
	}
	if err := os.WriteFile(fp, []byte(sb.String()), 0o644); err != nil {
		return "写入失败: " + err.Error()
	}
	if idx != nil {
		idx.UpdateFile(fp) // 写即索引:同会话后续检索立即可见
	}
	return "已 filed back → wiki/synthesis/" + title + ".md(query-as-contribution 落盘成功)"
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
// 切片 B(选择性深读):rep 非空时,本页与已读内容逐字重复的模板段被紧凑标记替换
// (内容首次读入已在上下文,跳过不丢信息);Coverage 仍由 markRead 记整段,不影响 gap 判据。
func readPaged(root, path string, offset, limit int, rep *RepeatDetector) string {
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

	// 重复段检测(切片 B):把 [offset,end] 切成输出段,重复段用标记省略。
	segs := []outSeg{{offset, end, 0}}
	if rep != nil {
		segs = rep.compact(path, lines, offset, end)
	}

	// 字节闸 + 组装:普通行逐行写,重复段写紧凑标记;超单页字节上限提前截断。
	var sb strings.Builder
	cut := end
	atCap := false
	for _, s := range segs {
		if s.first > 0 {
			if sb.Len() > 0 && !strings.HasSuffix(sb.String(), "\n") {
				sb.WriteByte('\n')
			}
			sb.WriteString(fmt.Sprintf("[第 %d–%d 行与已读内容逐字重复(模板段,首次见于第 %d 行),省略]\n", s.lo, s.hi, s.first))
			if sb.Len() > pageBytesCap {
				atCap = true
				cut = s.lo - 1
				break
			}
			continue
		}
		for i := s.lo; i <= s.hi; i++ {
			fmt.Fprintf(&sb, "%6d\t%s\n", i, lines[i-1])
			if sb.Len() > pageBytesCap {
				cut = i - 1
				atCap = true
				break
			}
		}
		if atCap {
			break
		}
	}

	tail := fmt.Sprintf("(第 %d–%d 行,共 %d 行;已到文件末尾)", offset, end, len(lines))
	if end < len(lines) {
		tail = fmt.Sprintf("(第 %d–%d 行,共 %d 行;继续读:offset=%d)", offset, end, len(lines), end+1)
	}
	if atCap {
		tail = fmt.Sprintf("(第 %d–%d 行触达单页字节上限,共 %d 行;继续读:offset=%d)", offset, cut, len(lines), cut+1)
	}
	sb.WriteString(tail)
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
