// Package retrieval 混合检索:BM25-lite 词法 + 可选向量余弦,rerank 合并得分(Agentic RAG 的检索层)。
// 词法恒可用(离线);向量层配 KCP_EMBED_MODEL 后叠加语义相似。rerank 即两路归一化后的线性融合。
package retrieval

import (
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/knowledge-compounder/kcp/internal/embed"
)

// Doc 检索语料中的一份文档。
type Doc struct {
	Path  string // 磁盘路径(缓存键)
	Label string // 展示名(页面名/文件名)
	Text  string
}

// Result 单条检索结果。
type Result struct {
	Path    string
	Label   string
	Score   float64 // 0-1 融合分
	Lexical float64 // 词法子分
	Semantic float64 // 向量子分(-1 表示未启用)
	Summary string
}

// Options 检索选项。
type Options struct {
	Embedder embed.Embedder // 空 = 纯词法
	Cache    *embed.Cache   // 向量缓存,可空
	Top      int            // 返回条数(0 取默认 5)
}

// Search 对语料检索 query,返回前 Top 条。
func Search(docs []Doc, query string, opts Options) []Result {
	if opts.Top <= 0 {
		opts.Top = 5
	}
	if len(docs) == 0 {
		return nil
	}

	terms := tokenize(query)
	if len(terms) == 0 {
		return nil
	}

	// 词法分:TF×IDF(BM25-lite)。df 统计 + 逐文档打分一次过。
	df := map[string]int{}
	wordCounts := make([][]int, len(docs))
	for i, d := range docs {
		wordCounts[i] = termCounts(d.Text)
		seen := map[string]bool{}
		for _, t := range terms {
			if wordCounts[i][hashSlot(t)%4096] > 0 && !seen[t] {
				seen[t] = true
				df[t]++
			}
		}
	}
	n := float64(len(docs))
	lex := make([]float64, len(docs))
	for i, d := range docs {
		var s float64
		head := d.Text
		if len(head) > 300 {
			head = head[:300]
		}
		for _, t := range terms {
			cnt := wordCounts[i][hashSlot(t)%4096]
			if cnt == 0 {
				continue
			}
			// 标题/开头命中翻倍:前 300 字符的计数视为命中权重
			headCnt := strings.Count(strings.ToLower(head), t)
			cnt += headCnt
			dfv := df[t]
			idf := math.Log(1 + (n-float64(dfv)+0.5)/(float64(dfv)+0.5))
			s += idf * float64(cnt) / (float64(cnt) + 1.2)
		}
		lex[i] = s
	}

	// 语义分(可选):查询向量 × 文档向量
	useVec := opts.Embedder != nil
	sem := make([]float64, len(docs))
	if useVec {
		vecs := corpusVectors(docs, opts.Embedder, opts.Cache)
		qvecs, err := opts.Embedder.Embed([]string{query})
		if err == nil && len(qvecs) == 1 {
			for i := range docs {
				sem[i] = embed.Cosine(qvecs[0], vecs[i])
			}
		} else {
			useVec = false
		}
	}

	// 归一化 + 融合(词法 0.55 / 语义 0.45)
	maxLex := 0.0
	for _, x := range lex {
		if x > maxLex {
			maxLex = x
		}
	}
	out := make([]Result, 0, len(docs))
	for i, d := range docs {
		if lex[i] == 0 && (!useVec || sem[i] == 0) {
			continue
		}
		lexN := 0.0
		if maxLex > 0 {
			lexN = lex[i] / maxLex
		}
		score := lexN
		semN := 0.0
		if useVec {
			if sem[i] < 0 {
				sem[i] = 0
			}
			semN = sem[i]
			score = 0.55*lexN + 0.45*semN
		}
		out = append(out, Result{
			Path:     d.Path,
			Label:    d.Label,
			Score:    score,
			Lexical:  lexN,
			Semantic: semN,
			Summary:  summaryOf(d.Text),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if len(out) > opts.Top {
		out = out[:opts.Top]
	}
	return out
}

// corpusVectors 计算/读取语料向量:命中缓存直接读,否则批量嵌入并回写。
func corpusVectors(docs []Doc, emb embed.Embedder, cache *embed.Cache) [][]float32 {
	vecs := make([][]float32, len(docs))
	var pending []int
	for i, d := range docs {
		if fi, err := os.Stat(d.Path); err == nil && cache != nil {
			if v, ok := cache.Get(embed.Key(d.Path, fi)); ok {
				vecs[i] = v
				continue
			}
		}
		pending = append(pending, i)
	}
	const batch = 16
	for start := 0; start < len(pending); start += batch {
		end := start + batch
		if end > len(pending) {
			end = len(pending)
		}
		idx := pending[start:end]
		texts := make([]string, len(idx))
		for j, i := range idx {
			texts[j] = docs[i].Text
		}
		b, err := emb.Embed(texts)
		if err != nil {
			return vecs // 失败则返回已算部分
		}
		for j, i := range idx {
			vecs[i] = b[j]
			if cache != nil {
				if fi, err := os.Stat(docs[i].Path); err == nil {
					cache.Set(embed.Key(docs[i].Path, fi), b[j])
				}
			}
		}
	}
	if cache != nil {
		cache.Save()
	}
	return vecs
}

// ---- 词法辅助 ----

var stopwords = map[string]bool{
	"的": true, "了": true, "是": true, "在": true, "有": true, "和": true, "与": true,
	"及": true, "或": true, "这": true, "那": true, "一个": true, "什么": true, "怎么": true,
	"为什么": true, "如果": true, "可以": true, "应该": true, "如何": true, "the": true,
	"a": true, "an": true, "of": true, "to": true, "in": true, "and": true, "is": true,
}

func tokenize(q string) []string {
	var out []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			t := strings.ToLower(cur.String())
			if !stopwords[t] {
				out = append(out, t)
			}
			cur.Reset()
		}
	}
	for _, r := range q {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r > 127 {
			cur.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return out
}

// hashSlot 把 token 映射到计数槽(词法 IDF 统计用,不用真实词表)。
func hashSlot(t string) int {
	var h uint32 = 5381
	for _, r := range t {
		h = h*33 + uint32(r)
	}
	return int(h)
}

func termCounts(text string) []int {
	m := make([]int, 4096)
	text = strings.ToLower(text)
	// 按 token 切分计数(含中文连续串)
	words := tokenize(text)
	for _, w := range words {
		m[hashSlot(w)%4096]++
	}
	return m
}

var summaryRe = regexp.MustCompile(`(?m)^## (?:一句话结论|定义|概述|问题)\s*\n([^\n]+)`)

func summaryOf(text string) string {
	if m := summaryRe.FindStringSubmatch(text); m != nil {
		return strings.TrimSpace(m[1])
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "---") && !strings.HasPrefix(line, "#") {
			return line
		}
	}
	return ""
}

// CachePath 返回项目根下的向量缓存路径。
func CachePath(root string) string {
	return filepath.Join(root, ".kcp-embed-cache.json")
}
