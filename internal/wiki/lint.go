package wiki

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
)

// CompactionMarker 系统历史压缩占位符的前缀。write_file 的完整 content 被运行时压缩成
// 「[系统已压缩省略 N 字符——你实际写入的是完整内容…]」占位以省上下文;弱模型会把这个
// 占位符当真实内容写回文件(实测 6 个概念页被整页覆盖成占位符)。任何 wiki 文件含此前缀
// = 被污染;write_file/edit_file 工具也用它拒绝把占位符当内容写入。
const CompactionMarker = "[系统已压缩省略"

// LintResult 一次 lint 报告。
type LintResult struct {
	Broken         map[string][]string // 页面 → 指向不存在页面的链接
	Orphans        []string            // 零入站链接的页面
	NoFrontmatter  []string            // 缺 YAML frontmatter 的页面(格式检查)
	Format         []string            // source 页格式问题(缺 origin/意外发现/疑点)
	Contradictions []string            // 含矛盾标注的页(人工审核项,非错误)
	ReviewAfter    []string            // review_after 已过的页面(过时检查)
	Uncompiled     []string            // 未编译的 raw 源
	SurpriseWeak   []string            // 「意外发现」节疑似无私有联想(纯复述原文)——私有 edge 缺失的提示
	NearDup        []string            // 疑似同义概念页对(高文本相似 + 主题重叠,建页命名漂移的信号)
	Corrupted      []string            // 被系统压缩占位符污染的页面(内容被覆盖成占位符,必须重建)
	Counts         struct{ Pages, BrokenLinks int }
}

// Lint 健康检查:断链 + 孤儿 + 缺 frontmatter + source 格式 + 矛盾标注 + 过时 + 未编译覆盖。
func Lint(root string) LintResult {
	res := LintResult{Broken: map[string][]string{}}
	pages := Pages(root)

	// 收集出站链接与入站引用
	inbound := map[string]int{} // slug → 入站数
	for _, p := range pages {
		for _, link := range ParseWikilinks(Read(p)) {
			if strings.HasPrefix(link, "raw/") {
				continue
			}
			inbound[strings.TrimSuffix(link, ".md")]++
		}
	}

	var conceptBodies []struct {
		slug string
		body string
	}
	for _, p := range pages {
		slug := filepath.Base(p)
		content := Read(p)
		if inbound[strings.TrimSuffix(slug, ".md")] == 0 {
			res.Orphans = append(res.Orphans, slug)
		}
		if first := strings.TrimSpace(strings.SplitN(content, "\n", 2)[0]); first != "---" {
			res.NoFrontmatter = append(res.NoFrontmatter, slug)
		}
		vals, body := ParseFrontmatter(content)
		// 疑似同义概念页检测复用的正文:主循环已读,不必 nearDuplicateConcepts 再读盘
		if filepath.Base(filepath.Dir(p)) == "concepts" {
			conceptBodies = append(conceptBodies, struct {
				slug string
				body string
			}{strings.TrimSuffix(slug, ".md"), body})
		}
		// 压缩占位符污染:内容被系统历史压缩占位符覆盖(弱模型把占位符当真实内容写入)。
		// 任何页面含此前缀即判损坏——概念页没有 preflight 硬资产闸门,这是唯一能抓住它的扫描。
		if strings.Contains(content, CompactionMarker) {
			res.Corrupted = append(res.Corrupted, slug)
		}
		// 矛盾标注 → 人工审核项(SCHEMA:只把 CONTRADICTION/矛盾声明列为审核)
		for _, kw := range []string{"CONTRADICTION", "⚠️ 矛盾", "⚠️ 冲突", "矛盾声明"} {
			if strings.Contains(content, kw) {
				res.Contradictions = append(res.Contradictions, slug)
				break
			}
		}
		// 过时检查:review_after 早于今天的页面列为人审项
		if v, ok := vals["review_after"].(string); ok && v != "" {
			if t, err := time.Parse("2006-01-02", v); err == nil && t.Before(time.Now()) {
				res.ReviewAfter = append(res.ReviewAfter, slug)
			}
		}
		// source 页格式:origin 必填、意外发现必填、疑点节非空
		if filepath.Base(filepath.Dir(p)) == "sources" {
			if len(FrontmatterList(vals, "origin")) == 0 {
				res.Format = append(res.Format, slug+": 缺 origin(external|self)")
			}
			if !strings.Contains(body, "## 意外发现") {
				res.Format = append(res.Format, slug+": 缺「意外发现」节")
			} else {
				// 意外发现必须含私有联想(SCHEMA 规则 10 的精华:原文说什么 + 这让我联想到/在场景意味着什么)。
				// 只查节存在会让弱模型用「原文说了X,与我预期不符」的复述糊弄过去——私有 edge 是复利载体,必须逼出。
				if sec := sectionText(body, "意外发现"); sec != "" && !containsInsightSignal(sec) {
					res.SurpriseWeak = append(res.SurpriseWeak, slug+": 「意外发现」无联想信号(意味着/联想到/启示/场景/验证了…),疑似纯复述——补一行「这让我想到/在你场景中意味着…」")
				}
			}
			if !strings.Contains(body, "## 疑点") {
				res.Format = append(res.Format, slug+": 缺「疑点」节")
			}
		}
		for _, link := range ParseWikilinks(content) {
			if !ResolveLink(root, link) {
				res.Broken[slug] = append(res.Broken[slug], link)
			}
		}
	}
	// 未编译 raw(机器口径与 status 一致:复用 Scan)
	res.Uncompiled = Scan(root).UncompiledFiles
	// 疑似同义概念页(命名漂移 → 图碎片化的确定性检测):概念页两两算字符 bigram Jaccard,
	// 相似度过高报「疑似同一概念的两个页面」——同义归一靠模型候选词 + 此检测兜底。
	res.NearDup = nearDuplicateConcepts(conceptBodies)
	sort.Strings(res.Orphans)
	sort.Strings(res.NoFrontmatter)
	sort.Strings(res.Format)
	sort.Strings(res.Contradictions)
	sort.Strings(res.ReviewAfter)
	sort.Strings(res.SurpriseWeak)
	sort.Strings(res.NearDup)
	sort.Strings(res.Corrupted)
	for _, v := range res.Broken {
		res.Counts.BrokenLinks += len(v)
	}
	res.Counts.Pages = len(pages)
	return res
}

// ReportLint 打印 lint 报告并落盘 wiki/health/<日期>.md(健康报告留档,供下次比较)。
func ReportLint(root string) string {
	res := Lint(root)
	var b strings.Builder
	b.WriteString(fmt.Sprintf("lint: %d 页,断链 %d,孤儿 %d,缺 frontmatter %d,格式 %d,矛盾标注 %d,过时 %d,未编译 %d,弱意外发现 %d,疑似同义页 %d,被污染 %d\n",
		res.Counts.Pages, res.Counts.BrokenLinks, len(res.Orphans), len(res.NoFrontmatter),
		len(res.Format), len(res.Contradictions), len(res.ReviewAfter), len(res.Uncompiled),
		len(res.SurpriseWeak), len(res.NearDup), len(res.Corrupted)))
	// 健康趋势(P2):与上次 lint 首行对比——复利是时间序列,健康必须有曲线。
	if prev, prevDate := prevHealth(root); prev != "" {
		b.WriteString("上次(" + prevDate + "): " + prev + "\n")
	}
	if res.Counts.BrokenLinks > 0 {
		b.WriteString("断链:\n")
		for slug, links := range res.Broken {
			b.WriteString(fmt.Sprintf("  %s → %s\n", slug, strings.Join(links, ", ")))
		}
	}
	if len(res.Orphans) > 0 {
		b.WriteString("孤儿页(零入站):\n  " + strings.Join(res.Orphans, ", ") + "\n")
	}
	if len(res.NoFrontmatter) > 0 {
		b.WriteString("缺 frontmatter(格式):\n  " + strings.Join(res.NoFrontmatter, ", ") + "\n")
	}
	if len(res.Format) > 0 {
		b.WriteString("source 页格式问题:\n  " + strings.Join(res.Format, "\n  ") + "\n")
	}
	if len(res.Contradictions) > 0 {
		b.WriteString("矛盾标注(人工审核):\n  " + strings.Join(res.Contradictions, ", ") + "\n")
	}
	if len(res.ReviewAfter) > 0 {
		b.WriteString("过时页(review_after 已过,人工复核):\n  " + strings.Join(res.ReviewAfter, ", ") + "\n")
	}
	if len(res.SurpriseWeak) > 0 {
		b.WriteString("弱意外发现(疑似无私有联想,补联想即私有 edge 增厚):\n  " + strings.Join(res.SurpriseWeak, "\n  ") + "\n")
	}
	if len(res.NearDup) > 0 {
		b.WriteString("疑似同义概念页(命名漂移,合并或互链):\n  " + strings.Join(res.NearDup, "\n  ") + "\n")
	}
	if len(res.Corrupted) > 0 {
		b.WriteString("被压缩占位符污染(内容被覆盖成占位符,必须重建):\n  " + strings.Join(res.Corrupted, ", ") + "\n")
	}
	if len(res.Uncompiled) > 0 {
		b.WriteString("未编译 raw:\n  " + strings.Join(res.Uncompiled, ", ") + "\n")
	}
	if res.Counts.BrokenLinks == 0 && len(res.Orphans) == 0 && len(res.NoFrontmatter) == 0 && len(res.Format) == 0 && len(res.ReviewAfter) == 0 && len(res.Corrupted) == 0 {
		b.WriteString("健康:无断链、无孤儿、无格式问题、无过时、无污染。\n")
	}
	// 落盘健康报告(供历史比较;失败静默)
	dir := filepath.Join(root, "wiki", "health")
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, time.Now().Format("2006-01-02")+".md"), []byte(b.String()), 0o644)
	return b.String()
}

// sectionText 提取 body 中指定节的内容(不含「## 标题」行,到下一个 ## 前或文末)。
func sectionText(body, section string) string {
	idx := strings.Index(body, "## "+section)
	if idx < 0 {
		return ""
	}
	rest := body[idx+len("## "+section):]
	end := strings.Index(rest, "\n## ")
	if end < 0 {
		return strings.TrimSpace(rest)
	}
	return strings.TrimSpace(rest[:end])
}

// insightSignals 私有联想的信号词(SCHEMA 规则 10:原文说什么 + 这让我想到什么/在你场景意味着什么)。
// 纯复述原文(只转述事实)通常不含这些词——启发式提示,不是裁决。
var insightSignals = []string{"意味着", "联想到", "启示", "类比", "这说明", "对用户", "场景", "含义", "验证了", "提示了", "这说明", "所以", "由此", "值得", "想到"}

func containsInsightSignal(sec string) bool {
	for _, s := range insightSignals {
		if strings.Contains(sec, s) {
			return true
		}
	}
	return false
}

// nearDuplicateConcepts 疑似同义概念页检测:概念页两两算字符 bigram Jaccard,
// 相似度 ≥ 阈值报对。命名漂移(中英别称/近似名)会让同一概念裂成两页——这是图碎片化的确定性信号。
// O(n²) 仅对 concepts/ 子集;字符 bigram 对中文鲁棒(词切分不依赖词表)。
// 接收 Lint 主循环已解析的正文,避免重复读盘。
func nearDuplicateConcepts(concepts []struct {
	slug string
	body string
}) []string {
	const threshold = 0.5
	type grams struct {
		slug  string
		grams map[string]struct{}
	}
	vecs := make([]grams, 0, len(concepts))
	for _, c := range concepts {
		// 去掉 frontmatter 的哈希痕迹干扰:只对正文算相似(传入的 body 已剥离 frontmatter)
		vecs = append(vecs, grams{c.slug, charBigrams(c.body)})
	}
	var out []string
	for i := 0; i < len(vecs); i++ {
		for j := i + 1; j < len(vecs); j++ {
			s := jaccard(vecs[i].grams, vecs[j].grams)
			if s >= threshold {
				a := strings.TrimSuffix(vecs[i].slug, ".md")
				b := strings.TrimSuffix(vecs[j].slug, ".md")
				out = append(out, fmt.Sprintf("%s ~ %s(相似 %.2f)——疑似同一概念的两个页面,合并或互链", a, b, s))
			}
		}
	}
	return out
}

// charBigrams 非空白字符的相邻二元组集合(忽略大小写)。
func charBigrams(s string) map[string]struct{} {
	set := map[string]struct{}{}
	var prev rune
	for _, r := range strings.ToLower(s) {
		if unicode.IsSpace(r) {
			prev = 0
			continue
		}
		if prev != 0 {
			set[string(prev)+string(r)] = struct{}{}
		}
		prev = r
	}
	return set
}

// jaccard 两个集合的 Jaccard 相似度。
func jaccard(a, b map[string]struct{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	inter := 0
	for k := range a {
		if _, ok := b[k]; ok {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

// prevHealth 读 wiki/health/ 中日期早于今天的最近一份报告,返回其 lint 首行与日期。
// 文件名 YYYY-MM-DD.md 字典序即时间序;当天报告刚写,不与自己比。
func prevHealth(root string) (string, string) {
	files, _ := filepath.Glob(filepath.Join(root, "wiki", "health", "*.md"))
	if len(files) == 0 {
		return "", ""
	}
	sort.Strings(files)
	today := time.Now().Format("2006-01-02")
	for i := len(files) - 1; i >= 0; i-- {
		date := strings.TrimSuffix(filepath.Base(files[i]), ".md")
		if date < today {
			b, err := os.ReadFile(files[i])
			if err != nil {
				continue
			}
			for _, line := range strings.Split(string(b), "\n") {
				if strings.HasPrefix(line, "lint: ") {
					return strings.TrimSpace(line), date
				}
			}
		}
	}
	return "", ""
}
