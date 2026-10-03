package wiki

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// LintResult 一次 lint 报告。
type LintResult struct {
	Broken        map[string][]string // 页面 → 指向不存在页面的链接
	Orphans       []string            // 零入站链接的页面
	NoFrontmatter []string            // 缺 YAML frontmatter 的页面(格式检查)
	Counts        struct{ Pages, BrokenLinks int }
}

// Lint 健康检查(骨架):断链 + 孤儿。TODO 对齐 scripts/wiki-lint.py 全量(矛盾/格式/覆盖)。
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
		if inbound[strings.TrimSuffix(slug, ".md")] == 0 {
			res.Orphans = append(res.Orphans, slug)
		}
		if first := strings.TrimSpace(strings.SplitN(Read(p), "\n", 2)[0]); first != "---" {
			res.NoFrontmatter = append(res.NoFrontmatter, slug)
		}
		for _, link := range ParseWikilinks(Read(p)) {
			if !ResolveLink(root, link) {
				res.Broken[slug] = append(res.Broken[slug], link)
			}
		}
	}
	sort.Strings(res.Orphans)
	sort.Strings(res.NoFrontmatter)
	for _, v := range res.Broken {
		res.Counts.BrokenLinks += len(v)
	}
	res.Counts.Pages = len(pages)
	return res
}

// ReportLint 打印 lint 报告。
func ReportLint(root string) string {
	res := Lint(root)
	var b strings.Builder
	b.WriteString(fmt.Sprintf("lint: %d 页,断链 %d,孤儿 %d,缺 frontmatter %d\n", res.Counts.Pages, res.Counts.BrokenLinks, len(res.Orphans), len(res.NoFrontmatter)))
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
	if res.Counts.BrokenLinks == 0 && len(res.Orphans) == 0 && len(res.NoFrontmatter) == 0 {
		b.WriteString("健康:无断链、无孤儿、无格式问题。\n")
	}
	_ = os.MkdirAll(filepath.Join(root, "wiki", "health"), 0o755)
	return b.String()
}
