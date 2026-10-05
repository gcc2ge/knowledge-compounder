// 矛盾核对(切片 A):编译收尾把新源页关键声明与已有 wiki 页比对,显式判断 冲突/佐证/无涉。
// 此前只有 prompt 指令「有矛盾就标注」——弱模型隔离编译、不回头核对 wiki,矛盾静默丢失。
// 本台账 + check_contradictions 工具把「编译器有没有回头对照 wiki」从模型自觉变成确定性记录:
// FinishGuard 在模型想直接收尾时强制补一轮核对;postCompileQA 再独立重扫,验证结论是否落盘。
//
// 检索语义:不依赖 idx.Rank——索引对中文按整句 token 存储,改写表述的页面命中不了(实测空转)。
// 改用 CJK bigram 重叠:声明与页面共享的相邻字对越多越相关,对中文「同义不同词」鲁棒,
// 且 DF 过滤掉「实现/可以」这类泛化二元组。确定性、可离线、无嵌入依赖。
package tools

import (
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/gcc2ge/knowledge-compounder/internal/retrieval"
	"github.com/gcc2ge/knowledge-compounder/internal/wiki"
)

// ContradictionLog 记录源页→已扫相关页的映射(Key=源页 slug,去 .md)。
// 非并发安全:runtime 主循环串行执行工具,与 Coverage 同约定。
type ContradictionLog struct {
	scans map[string][]string
}

// Mark 记录一次成功核对:源页 slug 扫到相关页 labels(空=扫过但无相关页,仍视为核对过)。
func (l *ContradictionLog) Mark(slug string, labels []string) {
	if l.scans == nil {
		l.scans = map[string][]string{}
	}
	l.scans[slug] = labels
}

// Scanned 该源页是否已跑过矛盾核对。
func (l *ContradictionLog) Scanned(slug string) bool {
	_, ok := l.scans[slug]
	return ok
}

// Pages 返回源页扫到的相关页 label(空=扫过但无相关页)。
func (l *ContradictionLog) Pages(slug string) []string { return l.scans[slug] }

// ContradictionHit 一次命中的相关页 + 触发它的源页声明。
type ContradictionHit struct {
	Result retrieval.Result
	Claim  string
}

// ExtractClaims 从源页正文提取矛盾核对用的关键声明:
// 一句话结论段全文(≤200 字)+ 论证链段前段(≤300 字)。二者浓缩源页核心论点与支撑链;
// 关键细节/意外发现等不参与比对(细节冲突由 preflight 硬资产兜,论点冲突才是核对对象)。
func ExtractClaims(text string) []string {
	var conclusion, argument string
	section := ""
	for _, ln := range strings.Split(text, "\n") {
		t := strings.TrimSpace(ln)
		if t == "" || t == "---" {
			continue
		}
		if strings.HasPrefix(t, "## ") {
			section = t
			continue
		}
		if strings.HasPrefix(t, "# ") {
			continue
		}
		switch section {
		case "## 一句话结论":
			if len(conclusion) < 200 {
				conclusion = collapseWS(conclusion + " " + t)
			}
		case "## 论证链":
			if len(argument) < 300 {
				argument = collapseWS(argument + " " + t)
			}
		}
	}
	var out []string
	if conclusion != "" {
		out = append(out, conclusion)
	}
	if argument != "" {
		out = append(out, argument)
	}
	return out
}

// ScanContradictions 把源页关键声明与整个 wiki 页比对,返回最相近的去重相关页(排除本源自身)。
// 核心:声明特征二元组 × 页面二元组的重叠率;DF 高于 high 的泛化二元组不作区分特征。
// 不依赖运行期状态:postCompileQA 用同一函数独立重扫,防止模型跳过/糊弄。
func ScanContradictions(root, slug, text string, k int) []ContradictionHit {
	if root == "" || text == "" {
		return nil
	}
	if k <= 0 {
		k = 4
	}
	claims := ExtractClaims(text)
	if len(claims) == 0 {
		return nil
	}

	type pg struct{ rel, label, body string }
	var pages []pg
	for _, abs := range wiki.Pages(root) {
		label := strings.TrimSuffix(filepath.Base(abs), ".md")
		if label == slug { // 重编译场景,排除本源自身
			continue
		}
		body := wiki.Read(abs)
		if body == "" {
			continue
		}
		rel, err := filepath.Rel(root, abs)
		if err != nil {
			rel = abs
		}
		pages = append(pages, pg{rel, label, body})
	}
	if len(pages) == 0 {
		return nil
	}

	// 每页去重二元组集合 + 全局 DF。
	sets := make([]map[string]bool, len(pages))
	df := map[string]int{}
	for i, p := range pages {
		m := map[string]bool{}
		for _, b := range bigramsOf(p.body) {
			if !m[b] {
				m[b] = true
				df[b]++
			}
		}
		sets[i] = m
	}
	high := len(pages) / 5 // 泛化阈值:出现于 >20% 页的二元组不具区分力
	if high < 8 {
		high = 8
	}

	// 逐声明打分,页面取跨声明最高分。
	type bestT struct {
		score float64
		claim string
	}
	best := map[int]bestT{}
	for _, c := range claims {
		feats := distinctiveBigrams(bigramsOf(c), df, high)
		if len(feats) == 0 {
			continue
		}
		for i, m := range sets {
			hit := 0
			for _, b := range feats {
				if m[b] {
					hit++
				}
			}
			score := float64(hit) / float64(len(feats))
			if hit < 2 && score < 0.15 { // 至少 2 个特征或 15% 重叠才算相关
				continue
			}
			if cur, ok := best[i]; !ok || score > cur.score {
				best[i] = bestT{score, c}
			}
		}
	}
	if len(best) == 0 {
		return nil
	}

	type ranked struct {
		i     int
		score float64
		claim string
	}
	var rs []ranked
	for i, v := range best {
		rs = append(rs, ranked{i, v.score, v.claim})
	}
	sort.Slice(rs, func(a, b int) bool {
		if rs[a].score != rs[b].score {
			return rs[a].score > rs[b].score
		}
		return pages[rs[a].i].label < pages[rs[b].i].label
	})
	if len(rs) > k {
		rs = rs[:k]
	}

	out := make([]ContradictionHit, 0, len(rs))
	for _, r := range rs {
		p := pages[r.i]
		out = append(out, ContradictionHit{
			Result: retrieval.Result{
				Path:    p.rel,
				Label:   p.label,
				Score:   r.score,
				Summary: truncate(collapseWS(p.body), 120),
			},
			Claim: r.claim,
		})
	}
	return out
}

// bigramsOf 提取文本中字母/数字连串的相邻二元组(汉字即一字一码位,二元组=相邻字对)。
func bigramsOf(s string) []string {
	var run []rune
	var out []string
	flush := func() {
		for i := 0; i+1 < len(run); i++ {
			out = append(out, string(run[i:i+2]))
		}
		run = run[:0]
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			run = append(run, r)
		} else {
			flush()
		}
	}
	flush()
	return out
}

// distinctiveBigrams 去重 + 剔除高 DF 泛化二元组(「实现/可以」这类不具区分力)。
func distinctiveBigrams(raw []string, df map[string]int, high int) []string {
	seen := map[string]bool{}
	var out []string
	for _, b := range raw {
		if seen[b] || df[b] > high {
			continue
		}
		seen[b] = true
		out = append(out, b)
	}
	return out
}

// pageLabels 提取命中列表的 label,供台账记录与 QA 展示。
func pageLabels(hits []ContradictionHit) []string {
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.Result.Label
	}
	return out
}

// truncate 截断到 n 个 rune(避免切 UTF-8 中文字节)。
func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
