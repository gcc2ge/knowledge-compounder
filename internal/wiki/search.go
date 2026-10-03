package wiki

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

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
