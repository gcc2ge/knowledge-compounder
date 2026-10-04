// preflight:raw 硬资产盘点 + 编译产物完整性对比(SCHEMA「论证链完整性核验」的机械化)。
// 对齐 compiler-preflight.py:机械盘点代码块/表/示例,不看论证质量——硬资产不能丢。
package wiki

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

var (
	preFencedBlock = regexp.MustCompile("(?ms)^```[^\\n]*\\n.*?^```\\s*$")
	preTable       = regexp.MustCompile(`(?m)^\|.+\|\s*\n\|(?:\s*:?-{3,}:?\s*\|)+`)
	preHTMLTable   = regexp.MustCompile(`(?i)<table\b`)
	preMath        = regexp.MustCompile(`(?s)\$\$.+?\$\$|\\\[.+?\\\]`)
	preExample     = regexp.MustCompile(`(?im)^(?:Example|示例|案例|输入|输出|Case)\b`)
	preSection     = regexp.MustCompile(`(?m)^##\s+(.+?)\s*$`)
)

// Preflight 对比 raw 与 source 页的硬资产,返回报告与问题清单。
// sourcePage 为空时只盘点 raw。
func Preflight(rawPath, sourcePage string) (string, []string, error) {
	rawB, err := os.ReadFile(rawPath)
	if err != nil {
		return "", nil, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "raw: %s\n", rawPath)
	raw := inventoryText(string(rawB))
	fmt.Fprintf(&b, "  代码块 %d | markdown 表 %d | html 表 %d | math %d | 编号示例 %d | %d 行\n",
		raw.fenced, raw.tables, raw.htmlTables, raw.math, raw.examples, raw.lines)

	var issues []string
	if sourcePage == "" {
		return b.String(), nil, nil
	}
	pageB, err := os.ReadFile(sourcePage)
	if err != nil {
		return "", nil, err
	}
	page := inventoryText(string(pageB))
	fmt.Fprintf(&b, "source: %s\n  代码块 %d | markdown 表 %d | html 表 %d | math %d | 编号示例 %d | %d 行\n",
		sourcePage, page.fenced, page.tables, page.htmlTables, page.math, page.examples, page.lines)

	check := func(name string, rawN, pageN int) {
		if rawN > 0 && pageN < rawN {
			issues = append(issues, fmt.Sprintf("⚠️ %s: raw=%d, source=%d — 硬资产可能丢失,请逐条核验", name, rawN, pageN))
		}
	}
	check("代码块", raw.fenced, page.fenced)
	check("markdown 表", raw.tables, page.tables)
	check("html 表", raw.htmlTables, page.htmlTables)
	check("数学表达式", raw.math, page.math)
	check("编号示例", raw.examples, page.examples)

	pageText := string(pageB)
	for _, s := range []string{"一句话结论", "论证链", "关键细节", "作者立场与定位", "意外发现", "疑点", "术语", "连接", "引用"} {
		if !sectionHas(pageText, s) {
			issues = append(issues, "❌ source 缺「"+s+"」节")
		}
	}
	return b.String(), issues, nil
}

type inv struct{ fenced, tables, htmlTables, math, examples, lines int }

func inventoryText(text string) inv {
	return inv{
		fenced:     len(preFencedBlock.FindAllString(text, -1)),
		tables:     len(preTable.FindAllString(text, -1)),
		htmlTables: len(preHTMLTable.FindAllString(text, -1)),
		math:       len(preMath.FindAllString(text, -1)),
		examples:   len(preExample.FindAllString(text, -1)),
		lines:      strings.Count(text, "\n") + 1,
	}
}

func sectionHas(text, heading string) bool {
	for _, s := range preSection.FindAllStringSubmatch(text, -1) {
		if strings.TrimSpace(s[1]) == heading {
			return true
		}
	}
	return false
}
