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
