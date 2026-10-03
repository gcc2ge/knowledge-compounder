// index:重建 wiki/index.md(coordinator 闭环)。对齐 SCHEMA 的 Index 格式。
package wiki

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// RebuildIndex 扫描 wiki 全部页,生成 index.md 内容。
func RebuildIndex(root string) string {
	var sources, concepts, entities, synths []string
	for _, p := range Pages(root) {
		dir := filepath.Base(filepath.Dir(p))
		content := Read(p)
		vals, body := ParseFrontmatter(content)
		slug := strings.TrimSuffix(filepath.Base(p), ".md")
		switch dir {
		case "sources":
			srcFile := ""
			sf := FrontmatterList(vals, "source_files")
			if len(sf) > 0 {
				srcFile = filepath.Base(sf[0])
			}
			tags := strings.Join(FrontmatterList(vals, "tags"), ",")
			date := FrontmatterString(vals, "compiled")
			one := firstLineOfSection(body, "## 一句话结论")
			sources = append(sources, fmt.Sprintf("| [[%s]] | %s | %s | %s | %s |",
				slug, srcFile, stripSourceSuffix(one), date, tags))
		case "concepts":
			conf := FrontmatterString(vals, "confidence")
			nSrc := len(FrontmatterList(vals, "sources"))
			def := firstLineOfSection(body, "## 定义")
			concepts = append(concepts, fmt.Sprintf("| [[%s]] | %s | %s | %d |", slug, def, conf, nSrc))
		case "entities":
			et := FrontmatterString(vals, "entity_type")
			overview := firstLineOfSection(body, "## 概述")
			entities = append(entities, fmt.Sprintf("| [[%s]] | %s | %s |", slug, et, overview))
		case "synthesis":
			q := FrontmatterString(vals, "question")
			created := FrontmatterString(vals, "created")
			synths = append(synths, fmt.Sprintf("| [[%s]] | %s | %s |", slug, q, created))
		}
	}
	// title 兜底已在 extractTitle 中实现(slug 兜底),表内直接用 slug 作为 [[wikilink]]

	bySlug := func(list []string) { sort.Strings(list) }
	bySlug(sources)
	bySlug(concepts)
	bySlug(entities)
	bySlug(synths)

	st := Scan(root)
	var b strings.Builder
	b.WriteString("# Wiki Index\n\n## 概要\n")
	b.WriteString(fmt.Sprintf("- 总页面数:%d\n- 源文件数:%d\n- 最后更新:%s\n",
		st.Total, st.Sources, time.Now().Format("2006-01-02")))
	b.WriteString("\n## Sources(按文件名排序)\n| 文件 | 源 | 核心收获 | 日期 | 标签 |\n|------|-----|------|------|------|\n")
	for _, r := range sources {
		b.WriteString(r + "\n")
	}
	b.WriteString("\n## Concepts\n| 文件 | 描述 | 置信度 | 源数量 |\n|------|------|--------|--------|\n")
	for _, r := range concepts {
		b.WriteString(r + "\n")
	}
	b.WriteString("\n## Entities\n| 文件 | 类型 | 描述 |\n|------|------|------|\n")
	for _, r := range entities {
		b.WriteString(r + "\n")
	}
	b.WriteString("\n## Synthesis\n| 文件 | 问题 | 日期 |\n|------|------|------|\n")
	for _, r := range synths {
		b.WriteString(r + "\n")
	}
	return b.String()
}

// WriteIndex 重建 wiki/index.md 并返回写路径。
func WriteIndex(root string) (string, error) {
	fp := filepath.Join(root, "wiki", "index.md")
	if err := os.WriteFile(fp, []byte(RebuildIndex(root)), 0o644); err != nil {
		return "", err
	}
	return fp, nil
}

// extractTitle 取正文第一个 # 标题;无则用 slug。

// firstLineOfSection 取节标题后的第一行非空内容(去 > 前缀,截断 120 字)。
func firstLineOfSection(body, heading string) string {
	idx := strings.Index(body, heading)
	if idx < 0 {
		return ""
	}
	rest := body[idx+len(heading):]
	if i := strings.Index(rest, "\n## "); i >= 0 {
		rest = rest[:i]
	}
	for _, line := range strings.Split(rest, "\n") {
		trim := strings.TrimSpace(strings.TrimPrefix(line, ">"))
		if trim != "" {
			r := []rune(trim)
			if len(r) > 120 {
				return string(r[:120]) + "…"
			}
			return trim
		}
	}
	return ""
}

// stripSourceSuffix 去「（来源：…）」尾缀(SCHEMA index 规则)。
func stripSourceSuffix(s string) string {
	if i := strings.Index(s, "（来源："); i >= 0 {
		return s[:i]
	}
	return s
}
