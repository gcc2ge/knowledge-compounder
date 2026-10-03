// Package eval 评估集 + Agent-as-a-Judge + RAG-vs-编译复利对照实验(Phase 4)。
// 把「编译复利 > RAG 外挂」从信念变成数据:同一组种子问题,两套检索方案各自答题,
// 用 LLM 评审按 SCHEMA 质量维度打分,输出对照报告。种子见 eval/seeds/,报告写 eval/reports/。
package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/knowledge-compounder/kcp/internal/config"
	"github.com/knowledge-compounder/kcp/internal/provider"
	"github.com/knowledge-compounder/kcp/internal/retrieval"
	"github.com/knowledge-compounder/kcp/internal/wiki"
)

// Seed 一个评估用例:问题 + 期望要点。种子需按自己的知识库改写(eval/seeds/)。
type Seed struct {
	ID       string   `json:"id"`
	Question string   `json:"question"`
	Note     string   `json:"note"`
	Rubric   []string `json:"rubric,omitempty"`
}

// DefaultRubric SCHEMA 质量维度(来源性质 A/B/C 之外的评估面)。
var DefaultRubric = []string{"论证完整性", "连接价值", "矛盾标注", "综合密度", "可迁移性"}

// LoadSeeds 加载评估种子:path 指定单个文件;空则扫 root/eval/seeds/*.json。
func LoadSeeds(root, path string) ([]Seed, error) {
	if path == "" {
		matches, err := filepath.Glob(filepath.Join(root, "eval", "seeds", "*.json"))
		if err != nil || len(matches) == 0 {
			return nil, fmt.Errorf("未找到评估种子(%s/eval/seeds/*.json),请先准备种子", root)
		}
		sort.Strings(matches)
		var all []Seed
		for _, m := range matches {
			seeds, err := loadSeedFile(m)
			if err != nil {
				return nil, err
			}
			all = append(all, seeds...)
		}
		return all, nil
	}
	return loadSeedFile(path)
}

func loadSeedFile(path string) ([]Seed, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var seeds []Seed
	if err := json.Unmarshal(b, &seeds); err != nil {
		return nil, fmt.Errorf("种子 JSON 解析失败 %s: %w", path, err)
	}
	return seeds, nil
}

// ---- 语料 ----

// RawDocs 未编译原文语料(raw/、raw/books/、raw/observations/)——RAG 外挂的检索对象。
func RawDocs(root string) []retrieval.Doc {
	var docs []retrieval.Doc
	for _, dir := range []string{"raw", filepath.Join("raw", "books"), filepath.Join("raw", "observations")} {
		matches, _ := filepath.Glob(filepath.Join(root, dir, "*.md"))
		for _, m := range matches {
			text := wiki.Read(m)
			if text == "" {
				continue
			}
			rel, _ := filepath.Rel(root, m)
			docs = append(docs, retrieval.Doc{Path: m, Label: rel, Text: text})
		}
	}
	return docs
}

// WikiDocs 编译后知识资产(wiki/ 全部页面)——编译复利的检索对象。
func WikiDocs(root string) []retrieval.Doc {
	var docs []retrieval.Doc
	for _, p := range wiki.Pages(root) {
		text := wiki.Read(p)
		if text == "" {
			continue
		}
		rel, _ := filepath.Rel(root, p)
		docs = append(docs, retrieval.Doc{Path: p, Label: strings.TrimSuffix(rel, ".md"), Text: text})
	}
	return docs
}

// ---- 答题 ----

const answerPrompt = `你是评估用答题员。仅依据下面【资料】中的内容回答,不引入资料外知识。
要求:
- 综合资料中的信息作答,每条证据标注来源(如 [[资料1]])
- 与资料内容矛盾或不完整处如实说明
- 控制在 400 字内,直接输出答案,不要前缀。

问题: %s

【资料】
%s`

func buildContext(docsByPath map[string]string, res []retrieval.Result) string {
	var b strings.Builder
	for i, r := range res {
		text := docsByPath[r.Path]
		fmt.Fprintf(&b, "[资料%d] %s(命中 %.2f)\n", i+1, r.Label, r.Score)
		if len(text) > 1500 {
			text = text[:1500]
		}
		b.WriteString(text)
		b.WriteString("\n\n")
	}
	return b.String()
}

// Answer 用给定语料检索并让 LLM 答题。返回答案文本。
func Answer(ctx context.Context, p provider.Provider, docs []retrieval.Doc, q string, opts retrieval.Options, top int) (string, error) {
	opts.Top = top
	res := retrieval.Search(docs, q, opts)
	if len(res) == 0 {
		return "(检索无命中)", nil
	}
	byPath := make(map[string]string, len(docs))
	for _, d := range docs {
		byPath[d.Path] = d.Text
	}
	prompt := fmt.Sprintf(answerPrompt, q, buildContext(byPath, res))
	msg, err := p.Chat(ctx, []provider.Message{
		{Role: "system", Content: "你是严谨的评估答题员。"},
		{Role: "user", Content: prompt},
	}, nil, nil)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(msg.Content), nil
}

// ---- 评审(Agent-as-a-Judge) ----

type judgeScores struct {
	Scores   map[string]map[string]int `json:"scores"`
	Verdict  string                    `json:"verdict"`
	Rationale string                   `json:"rationale"`
}

const judgePrompt = `你是知识库方案评审(Agent-as-a-Judge)。同一问题给了两套检索方案的答案:
- 方案A = RAG外挂(直接检索原文碎片,原文=未编译的原始资料)
- 方案B = 知识编译复利(检索编译后的知识资产:源摘要/概念/综合页,含意外发现、连接与意义、矛盾标注、置信度)
按以下 5 个维度对两方案各打 1-5 分(整数):
- 论证完整性:证据链是否完整、每步有依据
- 连接价值:是否建立跨知识连接并解释其意义
- 矛盾标注:是否识别并显式呈现资料矛盾
- 综合密度:是否经综合而非罗列碎片
- 可迁移性:结论能否直接用于新场景/决策

问题: %s

方案A(RAG)答案:
%s

方案B(编译复利)答案:
%s

只输出 JSON(不要 markdown 围栏,不要其他文字):
{"scores":{"rag":{"论证完整性":1,"连接价值":1,"矛盾标注":1,"综合密度":1,"可迁移性":1},"wiki":{...}},"verdict":"B更优|A更优|持平","rationale":"一句话理由"}`

// Judge 让 LLM 评审两套答案,返回各维度分数。
func Judge(ctx context.Context, p provider.Provider, seed Seed, ragAns, wikiAns string) (judgeScores, error) {
	prompt := fmt.Sprintf(judgePrompt, seed.Question, ragAns, wikiAns)
	msg, err := p.Chat(ctx, []provider.Message{
		{Role: "system", Content: "你是严格的评估评审,只输出 JSON。"},
		{Role: "user", Content: prompt},
	}, nil, nil)
	if err != nil {
		return judgeScores{}, err
	}
	var out judgeScores
	text := strings.TrimSpace(msg.Content)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	text = strings.TrimSpace(text)
	text = regexp.MustCompile(`^[^{]*`).ReplaceAllString(text, "")
	text = regexp.MustCompile(`}[^}]*$`).ReplaceAllString(text, "}")
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return judgeScores{}, fmt.Errorf("评审 JSON 解析失败: %v\n原文: %.200s", err, msg.Content)
	}
	return out, nil
}

// ---- 报告 ----

// CaseResult 单条种子的完整结果。
type CaseResult struct {
	Seed    Seed
	RAGAns  string
	WikiAns string
	Judge   judgeScores
}

// RunCompare 跑完整对照实验:两方案各答题 → 评审 → 报告。
func RunCompare(root string, cfg config.Config, seeds []Seed) (string, error) {
	p := newProvider(cfg)
	if cfg.APIKey == "" && cfg.BaseURL == "" {
		return "", fmt.Errorf("未配置 KCP_API_KEY,无法运行 LLM 评估")
	}
	opts := RetrievalOpts(root, cfg)
	rawDocs, wikiDocs := RawDocs(root), WikiDocs(root)

	var cases []CaseResult
	for _, s := range seeds {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		ragAns, err := Answer(ctx, p, rawDocs, s.Question, opts, cfg.RetrieveK)
		if err != nil {
			cancel()
			return "", fmt.Errorf("RAG 答题失败 [%s]: %w", s.ID, err)
		}
		wikiAns, err := Answer(ctx, p, wikiDocs, s.Question, opts, cfg.RetrieveK)
		if err != nil {
			cancel()
			return "", fmt.Errorf("WIKI 答题失败 [%s]: %w", s.ID, err)
		}
		j, err := Judge(ctx, p, s, ragAns, wikiAns)
		cancel()
		if err != nil {
			return "", fmt.Errorf("评审失败 [%s]: %w", s.ID, err)
		}
		cases = append(cases, CaseResult{Seed: s, RAGAns: ragAns, WikiAns: wikiAns, Judge: j})
		fmt.Printf("✓ %s: verdict=%s\n", s.ID, j.Verdict)
	}
	report := render(cases, cfg.Model)
	fp := writeReport(root, report)
	fmt.Println("报告已写入: " + fp)
	return report, nil
}

func render(cases []CaseResult, model string) string {
	var b strings.Builder
	b.WriteString("# 对照实验报告: RAG 外挂 vs 知识编译复利\n\n")
	b.WriteString(fmt.Sprintf("时间: %s · 模型: %s · 种子数: %d\n\n", time.Now().Format("2006-01-02 15:04"), model, len(cases)))

	avg := func(mode string, dim string) float64 {
		total, n := 0, 0
		for _, c := range cases {
			if v, ok := c.Judge.Scores[mode][dim]; ok {
				total += v
				n++
			}
		}
		if n == 0 {
			return 0
		}
		return float64(total) / float64(n)
	}

	b.WriteString("| 维度 | RAG | 编译 | Δ |\n|---|---|---|---|\n")
	var ragTotal, wikiTotal float64
	for _, dim := range DefaultRubric {
		r, w := avg("rag", dim), avg("wiki", dim)
		ragTotal += r
		wikiTotal += w
		b.WriteString(fmt.Sprintf("| %s | %.1f | %.1f | %+.1f |\n", dim, r, w, w-r))
	}
	if len(cases) > 0 {
		ndim := len(DefaultRubric)
		b.WriteString(fmt.Sprintf("| **平均** | **%.1f** | **%.1f** | **%+.1f** |\n", ragTotal/float64(ndim), wikiTotal/float64(ndim), (wikiTotal-ragTotal)/float64(ndim)))
	}

	verdictCount := map[string]int{}
	for _, c := range cases {
		verdictCount[c.Judge.Verdict]++
	}
	b.WriteString(fmt.Sprintf("\n评审倾向: %s\n", verdictStr(verdictCount)))

	for _, c := range cases {
		note := c.Seed.Note
		note = strings.TrimPrefix(note, "期望:")
		note = strings.TrimSpace(note)
		b.WriteString(fmt.Sprintf("\n---\n\n## %s %s\n\n> 期望: %s\n\n**RAG 答案**:\n\n%s\n\n**编译复利答案**:\n\n%s\n\n**评审**: verdict=%s · %s\n",
			c.Seed.ID, c.Seed.Question, note, c.RAGAns, c.WikiAns, c.Judge.Verdict, c.Judge.Rationale))
		scores := map[string]map[string]int{"RAG": c.Judge.Scores["rag"], "WIKI": c.Judge.Scores["wiki"]}
		for _, m := range []string{"RAG", "WIKI"} {
			b.WriteString(fmt.Sprintf("%s: ", m))
			for _, dim := range DefaultRubric {
				if v, ok := scores[m][dim]; ok {
					b.WriteString(fmt.Sprintf("%s=%d ", dim, v))
				}
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

func verdictStr(m map[string]int) string {
	var parts []string
	for _, v := range []string{"B更优", "A更优", "持平"} {
		if n := m[v]; n > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", v, n))
		}
	}
	if len(parts) == 0 {
		return "无"
	}
	return strings.Join(parts, " / ")
}

func writeReport(root, content string) string {
	dir := filepath.Join(root, "eval", "reports")
	_ = os.MkdirAll(dir, 0o755)
	fp := filepath.Join(dir, time.Now().Format("2006-01-02-150405")+"-compare.md")
	_ = os.WriteFile(fp, []byte(content), 0o644)
	return fp
}
