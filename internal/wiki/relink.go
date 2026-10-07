// relink:确定性链接候选层(对齐 strategy_wiki scripts/wiki-relink.py 的意图)。
// relink 的「找候选」第一拍不用 LLM——脚本把「这页可能该链谁」从注册表里筛出来,
// LLM 只负责第二拍(判断这条连接是否相关、写关联意义)。零 API 成本,本地毫秒级。
package wiki

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// PageInfo 注册表条目:wiki 一页的 slug/title/tags/type + 摘要首句(供 substring 匹配)。
type PageInfo struct {
	Slug    string
	Title   string
	Type    string
	Tags    []string
	Summary string
}

// Candidate 候选链接(score 降序,score 高 = 更可能是本页该链的页)。
type Candidate struct {
	Slug  string
	Title string
	Type  string
	Score int
}

var (
	reWikilink = regexp.MustCompile(`\[\[[^\]]+\]\]`)
	// 目录 → SCHEMA 单数类型(sources→source 等),供候选展示与注册表 Type 字段。
	dirType = map[string]string{
		"sources": "source", "concepts": "concept", "entities": "entity",
		"synthesis": "synthesis", "notes": "note",
	}
)

// BuildRegistry 扫描全部 wiki 页(含 notes)建注册表:每页 slug/title/type/tags + 摘要首句。
// 本地文件读取,毫秒级;一次性构建,relink-all 复用于所有目标页。
func BuildRegistry(root string) []PageInfo {
	var out []PageInfo
	for _, p := range Pages(root) {
		slug := strings.TrimSuffix(filepath.Base(p), ".md")
		vals, body := ParseFrontmatter(Read(p))
		title := FrontmatterString(vals, "title")
		if title == "" {
			title = slug
		}
		info := PageInfo{
			Slug:  slug,
			Title: title,
			Type:  dirType[filepath.Base(filepath.Dir(p))],
			Tags:  FrontmatterList(vals, "tags"),
		}
		if info.Type == "" {
			info.Type = filepath.Base(filepath.Dir(p))
		}
		if s := firstContentLine(body); s != "" {
			info.Summary = s
		}
		out = append(out, info)
	}
	return out
}

// firstContentLine 返回正文第一个非空、非标题行(源页通常是「一句话结论」首句,匹配价值高)。
func firstContentLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if t != "" && !strings.HasPrefix(t, "#") {
			return t
		}
	}
	return ""
}

// SuggestLinks 反向匹配:对注册表每一页,判断其 title/slug/tag/summary 是否在本页文本出现。
// 相比「提取术语再逐个匹配」:任意长度的实体名完整命中,不会产生海量碎词候选。
// 打分对齐 wiki-relink.py 思路:title 出现=10 / slug 出现=8 / tag 出现=5 / summary 出现=3。
// 过滤:自身、页内已链的页(不重复建议)。
func SuggestLinks(text, selfSlug string, reg []PageInfo, maxN int) []Candidate {
	cleanLower := strings.ToLower(reWikilink.ReplaceAllString(text, ""))
	var cands []Candidate
	for _, page := range reg {
		if page.Slug == "" || page.Slug == selfSlug {
			continue
		}
		if linkedInText(text, page.Slug) {
			continue
		}
		title := strings.ToLower(page.Title)
		score := 0
		switch {
		case title != "" && strings.Contains(cleanLower, title):
			score = 10
		case strings.Contains(cleanLower, strings.ToLower(page.Slug)):
			score = 8
		default:
			for _, tag := range page.Tags {
				if tag != "" && strings.Contains(cleanLower, strings.ToLower(tag)) {
					score = 5
					break
				}
			}
			if score == 0 && page.Summary != "" && strings.Contains(cleanLower, strings.ToLower(page.Summary)) {
				score = 3
			}
		}
		if score > 0 {
			cands = append(cands, Candidate{Slug: page.Slug, Title: page.Title, Type: page.Type, Score: score})
		}
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].Score != cands[j].Score {
			return cands[i].Score > cands[j].Score
		}
		return cands[i].Slug < cands[j].Slug
	})
	if len(cands) > maxN {
		cands = cands[:maxN]
	}
	return cands
}

// linkedInText 判断某 slug 是否已以 [[wikilink]] 形态出现在文本中(含 [[slug|显示名]] 形式)。
func linkedInText(text, slug string) bool {
	if strings.Contains(text, "[["+slug) {
		return true
	}
	for _, m := range reWikilink.FindAllString(text, -1) {
		inner := strings.TrimPrefix(strings.TrimSuffix(m, "]]"), "[[")
		if i := strings.Index(inner, "|"); i >= 0 {
			inner = inner[:i]
		}
		if strings.TrimSpace(inner) == slug {
			return true
		}
	}
	return false
}

// RelinkedDates 取源页 frontmatter 的 relinked / compiled 日期(缺失返回 "")。
// 增量判据:relinked >= compiled 且两者都有值 → 该页自上次编译后已回访过,未变,可跳过。
func RelinkedDates(fp string) (relinked, compiled string) {
	vals, _ := ParseFrontmatter(Read(fp))
	return FrontmatterString(vals, "relinked"), FrontmatterString(vals, "compiled")
}

// SetRelinked 把 relinked 日期写回源页 frontmatter(只动头,不动正文)。
func SetRelinked(fp, date string) error {
	text := Read(fp)
	if text == "" {
		return fmt.Errorf("页面不存在或为空: %s", fp)
	}
	vals, body := ParseFrontmatter(text)
	vals["relinked"] = date
	return writePage(fp, vals, body)
}
