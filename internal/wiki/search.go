package wiki

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Result 检索结果:页面路径 + 命中分 + 摘要首行。
type Result struct {
	Page    string
	Score   int
	Summary string
}

var summaryRe = regexp.MustCompile(`(?m)^## (?:一句话结论|定义|概述|问题)\s*\n([^\n]+)`)

// SearchPages 关键词检索:标题命中权重 3,正文命中计数。
// 骨架为 grep 级;TODO 升级 embedding + rerank(Agentic RAG)。
func SearchPages(root, query string, k int) []Result {
	terms := []string{}
	for _, t := range strings.Fields(strings.ToLower(query)) {
		terms = append(terms, t)
	}
	var results []Result
	for _, p := range Pages(root) {
		text := Read(p)
		if text == "" {
			continue
		}
		stem := strings.ToLower(filepath.Base(p))
		score := 0
		for _, t := range terms {
			if strings.Contains(stem, t) {
				score += 3
			}
			score += strings.Count(strings.ToLower(text), t)
		}
		if score == 0 {
			continue
		}
		summary := ""
		if m := summaryRe.FindStringSubmatch(text); m != nil {
			summary = strings.TrimSpace(m[1])
		}
		results = append(results, Result{Page: filepath.Base(p), Score: score, Summary: summary})
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Score > results[j].Score })
	if k > 0 && len(results) > k {
		results = results[:k]
	}
	return results
}

// GetPage 按 slug 或文件名读页面,返回 (内容, 子目录, 是否存在)。
func GetPage(root, slug string) (string, string, bool) {
	slug = strings.TrimSuffix(slug, ".md")
	for _, d := range Subdirs {
		p := filepath.Join(root, "wiki", d, slug+".md")
		if text := Read(p); text != "" {
			return text, d, true
		}
	}
	return "", "", false
}

var wikilinkRe = regexp.MustCompile(`\[\[([^\]|]+)(?:\|[^\]]+)?\]\]`)

// ParseWikilinks 解析页面里的 [[wikilinks]]。
func ParseWikilinks(text string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range wikilinkRe.FindAllStringSubmatch(text, -1) {
		l := m[1]
		if !seen[l] {
			seen[l] = true
			out = append(out, l)
		}
	}
	return out
}

// ResolveLink 判断 wikilink 是否指向存在的页面。
func ResolveLink(root, link string) bool {
	link = strings.TrimSuffix(link, ".md")
	if link == "" {
		return false
	}
	// 支持 [[raw/xxx]] 内部引用与跨子目录
	if strings.HasPrefix(link, "raw/") {
		return fileExists(filepath.Join(root, link+".md"))
	}
	for _, d := range Subdirs {
		if fileExists(filepath.Join(root, "wiki", d, link+".md")) {
			return true
		}
	}
	return false
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
