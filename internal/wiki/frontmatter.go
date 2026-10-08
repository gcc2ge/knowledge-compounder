// frontmatter:wiki 页 YAML-like 头的轻量解析/序列化(对齐 wiki-update.py)。
package wiki

import (
	"os"
	"sort"
	"strings"
)

// HasFrontmatter 判断文本是否以 --- 开头(YAML frontmatter)。
func HasFrontmatter(text string) bool {
	return strings.HasPrefix(text, "---\n")
}

// ParseFrontmatter 提取 frontmatter 键值 + 正文。
// 兼容两种合法 YAML 列表风格:缩进(  - item)与内联([a, b])。
// 无 frontmatter 时返回空 map 与全文。
func ParseFrontmatter(text string) (map[string]any, string) {
	vals := map[string]any{}
	if !HasFrontmatter(text) {
		return vals, strings.TrimSpace(text)
	}
	rest := strings.TrimPrefix(text, "---\n")
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return vals, strings.TrimSpace(text)
	}
	head := rest[:idx]
	body := strings.TrimSpace(rest[idx+4:])

	var cur string
	var list []string
	flush := func() {
		if cur == "" {
			return
		}
		if len(list) > 0 {
			vals[cur] = list
		} else {
			vals[cur] = ""
		}
		cur, list = "", nil
	}
	for _, line := range strings.Split(head, "\n") {
		trim := strings.TrimSpace(line)
		switch {
		case trim == "":
			continue
		case strings.HasPrefix(trim, "- "):
			if cur != "" {
				list = append(list, strings.TrimSpace(strings.TrimPrefix(trim, "- ")))
			}
		default:
			flush()
			if i := strings.Index(trim, ":"); i > 0 {
				cur = strings.TrimSpace(trim[:i])
				val := strings.TrimSpace(trim[i+1:])
				if strings.HasPrefix(val, "[") && strings.HasSuffix(val, "]") {
					inner := strings.TrimSpace(val[1 : len(val)-1])
					var items []string
					if inner != "" {
						for _, it := range strings.Split(inner, ",") {
							items = append(items, strings.Trim(strings.TrimSpace(it), "\"'"))
						}
					}
					vals[cur] = items
					cur = ""
				} else if val != "" {
					vals[cur] = strings.Trim(val, "\"'")
					cur = ""
				}
			}
		}
	}
	flush()
	return vals, body
}

// FrontmatterList 取 frontmatter 中的列表键;标量也包成单元素列表;缺失返回 nil。
func FrontmatterList(vals map[string]any, key string) []string {
	v, ok := vals[key]
	if !ok {
		return nil
	}
	switch t := v.(type) {
	case []string:
		return t
	case string:
		if t != "" {
			return []string{t}
		}
	}
	return nil
}

// FrontmatterString 取 frontmatter 标量键;缺失返回 ""。
func FrontmatterString(vals map[string]any, key string) string {
	v, ok := vals[key]
	if !ok {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// SerializeFrontmatter 把键值序列化回 frontmatter(含 --- 包裹)。
// 列表用缩进风格;键按字典序排序保证确定性输出。
func SerializeFrontmatter(vals map[string]any) string {
	var b strings.Builder
	b.WriteString("---\n")
	keys := make([]string, 0, len(vals))
	for k := range vals {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		switch t := vals[k].(type) {
		case []string:
			if len(t) == 0 {
				b.WriteString(k + ": []\n")
				continue
			}
			b.WriteString(k + ":\n")
			for _, it := range t {
				b.WriteString("  - " + it + "\n")
			}
		case string:
			b.WriteString(k + ": " + t + "\n")
		}
	}
	b.WriteString("---")
	return b.String()
}

// writePage 按 frontmatter + 正文写回页面(正文去首尾空行)。
func writePage(fp string, vals map[string]any, body string) error {
	text := SerializeFrontmatter(vals) + "\n\n" + strings.TrimSpace(body) + "\n"
	return os.WriteFile(fp, []byte(text), 0o644)
}
