// update:确定性页面编辑(无 LLM)。对齐 wiki-update.py:add-source/touch/add-link。
package wiki

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var pageDirs = []string{"sources", "concepts", "entities", "synthesis", "notes"}

// ResolveSlug 按 slug 在 wiki 子目录中定位页面文件。
func ResolveSlug(root, slug string) (string, bool) {
	slug = strings.TrimSuffix(slug, ".md")
	for _, sub := range pageDirs {
		p := filepath.Join(root, "wiki", sub, slug+".md")
		if _, err := os.Stat(p); err == nil {
			return p, true
		}
	}
	// 兜底:wiki/ 下任意深度
	matches, _ := filepath.Glob(filepath.Join(root, "wiki", "**", slug+".md"))
	for _, m := range matches {
		return m, true
	}
	return "", false
}

// AddSource 给页面 frontmatter sources 追加一个源,并更新 updated 日期。
// 幂等:已存在则不重复追加。保留原正文。
func AddSource(root, slug, source string) (string, error) {
	vals, body, fp, err := readPageBodyForEdit(root, slug)
	if err != nil {
		return "", err
	}
	sources := FrontmatterList(vals, "sources")
	if !contains(sources, source) {
		sources = append(sources, source)
	}
	vals["sources"] = sources
	vals["updated"] = today()
	if err := writePage(fp, vals, body); err != nil {
		return "", err
	}
	return fmt.Sprintf("已更新 %s: 追加 source '%s'", slug, source), nil
}

// Touch 更新页面 updated 日期为今天。保留原正文。
func Touch(root, slug string) (string, error) {
	vals, body, fp, err := readPageBodyForEdit(root, slug)
	if err != nil {
		return "", err
	}
	vals["updated"] = today()
	if err := writePage(fp, vals, body); err != nil {
		return "", err
	}
	return fmt.Sprintf("已 touch %s → %s", slug, today()), nil
}

// AddLink 往「相关」节追加一条 wikilink(幂等:已存在则跳过)。
func AddLink(root, slug, target string) (string, error) {
	vals, body, fp, err := readPageBodyForEdit(root, slug)
	if err != nil {
		return "", err
	}
	link := "[[" + strings.TrimSuffix(target, ".md") + "]]"
	if strings.Contains(body, link) {
		return fmt.Sprintf("%s 已有链接 %s,跳过", slug, link), nil
	}
	idx := strings.Index(body, "## 相关")
	if idx >= 0 {
		// 在「相关」节末尾插入(下一个 ## 前 / 文末)
		end := strings.Index(body[idx+len("## 相关"):], "\n## ")
		if end < 0 {
			body = body + "\n- " + link + "\n"
		} else {
			at := idx + len("## 相关") + end
			body = body[:at] + "\n- " + link + "\n" + body[at:]
		}
	} else {
		body = strings.TrimRight(body, "\n") + "\n\n## 相关\n- " + link + "\n"
	}
	vals["updated"] = today()
	if err := writePage(fp, vals, body); err != nil {
		return "", err
	}
	return fmt.Sprintf("已加链接 %s → %s", slug, link), nil
}

// Evidence 佐证/冲突回流(update_evidence 工具底座):给既有概念页追加证据。
// 佐证(corroborate)时机器重算 confidence——正向复利:每次编译都在物理上强化既有页,
// 而不只是往新源页「连接」写一行。冲突(contradict)时往「张力与缺口」落 lint 可识别标注。
// confidence 规则:独立源数 <2=low,2-3=medium,≥4=high;页面已有冲突标注时封顶 medium
// (证据打架的页面不该标 high,但也不降级——冲突另在「张力与缺口」显式呈现)。
func ApplyEvidence(root, slug, action, source, claim string) (string, error) {
	fp, ok := ResolveSlug(root, slug)
	if !ok {
		return "", fmt.Errorf("页面不存在: %s", slug)
	}
	if filepath.Base(filepath.Dir(fp)) != "concepts" {
		return "", fmt.Errorf("update_evidence 只作用于概念页(wiki/concepts/),%s 位于 %s 目录", slug, filepath.Base(filepath.Dir(fp)))
	}
	b, err := os.ReadFile(fp)
	if err != nil {
		return "", err
	}
	vals, body := ParseFrontmatter(string(b))
	sources := FrontmatterList(vals, "sources")
	switch action {
	case "corroborate":
		if source != "" && !contains(sources, source) {
			sources = append(sources, source)
		}
		n := len(sources)
		conf := "low"
		if n >= 2 {
			conf = "medium"
		}
		if n >= 4 {
			conf = "high"
		}
		if containsAny(body, "⚠️ 冲突", "⚠️ 矛盾", "矛盾声明", "CONTRADICTION") && conf == "high" {
			conf = "medium"
		}
		vals["sources"] = sources
		vals["confidence"] = conf
		line := "- 被 [[" + strings.TrimSuffix(source, ".md") + "]] 佐证"
		if claim != "" {
			line += ": " + claim
		}
		body, ok = appendToSection(body, "外部观点", line)
		if !ok {
			body = strings.TrimRight(body, "\n") + "\n\n## 外部观点\n" + line + "\n"
		}
	case "contradict":
		if strings.TrimSpace(claim) == "" {
			return "", fmt.Errorf("contradict 需要 claim(冲突原因)不能留空")
		}
		line := "- ⚠️ 冲突: [[" + strings.TrimSuffix(source, ".md") + "]] — " + claim
		body, ok = appendToSection(body, "张力与缺口", line)
		if !ok {
			return "", fmt.Errorf("概念页 %s 缺「张力与缺口」节,无法落冲突标注", slug)
		}
	default:
		return "", fmt.Errorf("action 只能是 corroborate|contradict,收到 %s", action)
	}
	vals["updated"] = today()
	if err := writePage(fp, vals, body); err != nil {
		return "", err
	}
	return fmt.Sprintf("已更新 %s: action=%s → confidence=%s, sources=%d", slug, action, FrontmatterString(vals, "confidence"), len(sources)), nil
}

// appendToSection 在正文指定节(## 标题)末尾追加一行;节不存在返回 (原body, false)。
// 插入点取「下一个 ## 」之前或文末,保证只动该节、不碰其他节。
func appendToSection(body, section, line string) (string, bool) {
	idx := strings.Index(body, "## "+section)
	if idx < 0 {
		return body, false
	}
	rest := body[idx+len("## "+section):]
	end := strings.Index(rest, "\n## ")
	if end < 0 {
		return strings.TrimRight(body, "\n") + "\n" + line + "\n", true
	}
	at := idx + len("## "+section) + end
	return body[:at] + "\n" + line + body[at:], true
}

func containsAny(s string, kws ...string) bool {
	for _, k := range kws {
		if strings.Contains(s, k) {
			return true
		}
	}
	return false
}

// readPageBodyForEdit 解析页面 frontmatter + 正文,返回 (vals, body, fp, err)。
// update 类操作必须保留原正文,否则会清空页面。
func readPageBodyForEdit(root, slug string) (map[string]any, string, string, error) {
	fp, ok := ResolveSlug(root, slug)
	if !ok {
		return nil, "", "", fmt.Errorf("页面不存在: %s", slug)
	}
	b, err := os.ReadFile(fp)
	if err != nil {
		return nil, "", "", err
	}
	vals, body := ParseFrontmatter(string(b))
	return vals, body, fp, nil
}

func today() string { return time.Now().Format("2006-01-02") }

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
