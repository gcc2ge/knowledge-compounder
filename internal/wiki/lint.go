package wiki

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// LintResult 一次 lint 报告。
type LintResult struct {
	Broken         map[string][]string // 页面 → 指向不存在页面的链接
	Orphans        []string            // 零入站链接的页面
	NoFrontmatter  []string            // 缺 YAML frontmatter 的页面(格式检查)
	Format         []string            // source 页格式问题(缺 origin/意外发现/疑点)
	Contradictions []string            // 含矛盾标注的页(人工审核项,非错误)
	ReviewAfter    []string            // review_after 已过的页面(过时检查)
	Uncompiled     []string            // 未编译的 raw 源
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
	sort.Strings(res.Orphans)
	sort.Strings(res.NoFrontmatter)
	sort.Strings(res.Format)
	sort.Strings(res.Contradictions)
	sort.Strings(res.ReviewAfter)
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
	b.WriteString(fmt.Sprintf("lint: %d 页,断链 %d,孤儿 %d,缺 frontmatter %d,格式 %d,矛盾标注 %d,过时 %d,未编译 %d\n",
		res.Counts.Pages, res.Counts.BrokenLinks, len(res.Orphans), len(res.NoFrontmatter),
		len(res.Format), len(res.Contradictions), len(res.ReviewAfter), len(res.Uncompiled)))
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
	if len(res.Uncompiled) > 0 {
		b.WriteString("未编译 raw:\n  " + strings.Join(res.Uncompiled, ", ") + "\n")
	}
	if res.Counts.BrokenLinks == 0 && len(res.Orphans) == 0 && len(res.NoFrontmatter) == 0 && len(res.Format) == 0 && len(res.ReviewAfter) == 0 {
		b.WriteString("健康:无断链、无孤儿、无格式问题、无过时。\n")
	}
	// 落盘健康报告(供历史比较;失败静默)
	dir := filepath.Join(root, "wiki", "health")
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, time.Now().Format("2006-01-02")+".md"), []byte(b.String()), 0o644)
	return b.String()
}
