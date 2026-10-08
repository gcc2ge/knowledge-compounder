package wiki

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/gcc2ge/knowledge-compounder/internal/retrieval"
)

// Retrieve 检索 wiki 全部页面(混合:词法 + 可选向量)。返回带私有度徽标的排序结果。
func Retrieve(root, query string, opts retrieval.Options) []retrieval.Result {
	var docs []retrieval.Doc
	for _, p := range Pages(root) {
		text := Read(p)
		if text == "" {
			continue
		}
		docs = append(docs, retrieval.Doc{Path: p, Label: filepath.Base(p), Text: text})
	}
	res := retrieval.Search(docs, query, opts)
	for i := range res {
		res[i].Label = strings.TrimSuffix(res[i].Label, ".md")
	}
	return res
}

// Evidence 证据私有度标注(SCHEMA synthesis「来源性质标注」的机械化):
//
//	A=公开知识(他人资料/概念/实体)  B=公开但经精选/综合(synthesis)  C=私有数据/经验(notes/self 源)
func Evidence(root, pagePath string) string {
	dir := filepath.Base(filepath.Dir(pagePath))
	switch dir {
	case "notes":
		return "C"
	case "synthesis":
		return "B"
	case "sources":
		if origin := frontmatterOrigin(pagePath); origin == "self" {
			return "C"
		}
		return "A"
	default:
		return "A"
	}
}

var badgeDesc = map[string]string{"A": "公开", "B": "精选综合", "C": "私有"}

// EvidenceBadge 返回带说明的徽标文字。
func EvidenceBadge(root, pagePath string) string {
	label := Evidence(root, pagePath)
	return "[" + label + "]" + badgeDesc[label]
}

func frontmatterOrigin(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	vals, _ := ParseFrontmatter(string(b))
	return FrontmatterString(vals, "origin")
}
