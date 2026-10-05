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
	// 围栏允许前导空白:嵌套在列表/引用里的缩进围栏也是合法代码块(实测 deepseek 产物带 3 空格缩进,
	// 旧正则锚定列首会漏检成 source=0)。[ \t]* 吸收缩进;反引号不能出现在 raw string,故拼接。
	preFencedBlock = regexp.MustCompile(`(?ms)^[ \t]*` + "```" + `[^\n]*\n.*?^[ \t]*` + "```" + `\s*$`)
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
	// 代码块特殊核验:允许「模板重复」式语义压缩——raw 里逐字重复的块,source 只需保留一个代表。
	// 降级为覆盖提示而非缺失;唯一块缺失仍是硬失败(真实内容丢失)。
	// 实测:deepseek 对 168 节同一代码骨架选择「保留唯一骨架 + 复现规则」,旧逻辑误报 164→0。
	if raw.fenced > 0 && page.fenced < raw.fenced {
		rawBlocks, pageBlocks := distinctBlocks(string(rawB)), distinctBlocks(string(pageB))
		missing := 0
		for blk := range rawBlocks {
			if !pageBlocks[blk] {
				missing++
			}
		}
		if missing == 0 {
			fmt.Fprintf(&b, "  代码块覆盖核验: raw=%d, source=%d — 缺失 %d 个均为重复块(去重后唯一 %d 个已全部覆盖)→ 语义无损,不阻断\n",
				raw.fenced, page.fenced, raw.fenced-page.fenced, len(rawBlocks))
		} else {
			issues = append(issues, fmt.Sprintf("⚠️ 代码块: raw=%d, source=%d — %d 个唯一代码块未覆盖(真实内容丢失),必须重编", raw.fenced, page.fenced, missing))
		}
	} else {
		check("代码块", raw.fenced, page.fenced)
	}
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

// distinctBlocks 提取去重后的代码块集合(规范化:逐行 trim + 去空行)。
// 用于重复块覆盖判定:raw 里 N 个逐字重复的代码块,source 保留了至少一个代表即视为无损。
func distinctBlocks(text string) map[string]bool {
	set := map[string]bool{}
	for _, m := range preFencedBlock.FindAllString(text, -1) {
		set[normalizeBlock(m)] = true
	}
	return set
}

func normalizeBlock(b string) string {
	lines := strings.Split(b, "\n")
	out := make([]string, 0, len(lines))
	for _, ln := range lines {
		if t := strings.TrimSpace(ln); t != "" {
			out = append(out, t)
		}
	}
	return strings.Join(out, "\n")
}
