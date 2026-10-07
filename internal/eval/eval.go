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

	"github.com/gcc2ge/knowledge-compounder/internal/config"
	"github.com/gcc2ge/knowledge-compounder/internal/provider"
	"github.com/gcc2ge/knowledge-compounder/internal/retrieval"
	"github.com/gcc2ge/knowledge-compounder/internal/wiki"
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

// TrajRubric 轨迹质量维度(过程评估,M10 Agent-as-a-Judge 轨迹判官)。
var TrajRubric = []string{"检索相关度", "证据覆盖", "证据忠实"}

// 评审 JSON 清洗正则:剥离首尾非 JSON 前缀/后缀(模型常包 md 代码块或解释文字)。
var (
	stripJSONPrefix = regexp.MustCompile(`^[^{]*`)
	stripJSONSuffix = regexp.MustCompile(`}[^}]*$`)
)

// TraceHit 一次检索命中的证据(M10 轨迹:检索到了什么)。
type TraceHit struct {
	Label string  `json:"label"` // 页面/文档标签(含路径)
	Score float64 `json:"score"` // 融合检索分
}

// Trace 单方案的检索轨迹:TopK 命中的证据列表。用于「对的答案、错的过程」检测——
// 答案文本可以漂亮,但若检索根本没命中关键页,过程就是坏的。
type Trace struct {
	Mode string     `json:"mode"` // "rag" | "wiki"
	Hits []TraceHit `json:"hits"`
}

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

// RawDocs 未编译原文语料(raw/、raw/books/)——RAG 外挂的检索对象。
// 刻意排除 raw/observations/:失败回灌是编译复利独有的私有 edge,掺进 RAG 语料
// 会掩盖对照实验的真实差异(见 M10 评估设计)。
func RawDocs(root string) []retrieval.Doc {
	var docs []retrieval.Doc
	for _, dir := range []string{"raw", filepath.Join("raw", "books")} {
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

// Answer 用给定语料检索并让 LLM 答题。返回答案文本 + 检索轨迹(供轨迹判官)。
func Answer(ctx context.Context, p provider.Provider, docs []retrieval.Doc, q string, opts retrieval.Options, top int) (string, Trace, error) {
	opts.Top = top
	res := retrieval.Search(docs, q, opts)
	trace := Trace{Hits: make([]TraceHit, 0, len(res))}
	for _, r := range res {
		trace.Hits = append(trace.Hits, TraceHit{Label: r.Label, Score: r.Score})
	}
	if len(res) == 0 {
		return "(检索无命中)", trace, nil
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
		return "", trace, err
	}
	return strings.TrimSpace(msg.Content), trace, nil
}

// ---- 评审(Agent-as-a-Judge) ----

type judgeScores struct {
	Scores    map[string]map[string]int `json:"scores"`
	Verdict   string                    `json:"verdict"`
	Rationale string                    `json:"rationale"`
	NoiseSpan int                       `json:"-"` // 多判去噪:单次 wiki 总分极差 = 本种子判官噪声底
}

// dimAliases 判官输出维度键的别名归一。实测判官中英混用(connection_value /
// synthesis_density / comprehensive_density 等英文变体与规范中文并存),而跨轮
// 复利 Δ 按精确 key 匹配——键漂移会让 beforeTotal 少算、Δ 被系统性高估。
// 统一映射回规范维度后,基线快照和 Δ 才跨轮可比。
var dimAliases = map[string]string{
	"connection_value":      "连接价值",
	"connection":            "连接价值",
	"connections":           "连接价值",
	"synthesis_density":     "综合密度",
	"comprehensive_density": "综合密度",
	"comprehensiveness":     "综合密度",
	"synthesis":             "综合密度",
	"argument_completeness": "论证完整性",
	"argumentation":         "论证完整性",
	"argument":              "论证完整性",
	"argumentation_quality": "论证完整性",
	"completeness":          "论证完整性",
	"contradiction_marking": "矛盾标注",
	"contradiction":         "矛盾标注",
	"contradictions":        "矛盾标注",
	"contradiction_detection": "矛盾标注",
	"transferability":       "可迁移性",
	"migratability":         "可迁移性",
	"generalizability":      "可迁移性",
	"portability":           "可迁移性",
	"retrieval_relevance":   "检索相关度",
	"evidence_coverage":     "证据覆盖",
	"evidence_fidelity":     "证据忠实",
}

// normalizeDimKeys 把所有维度键归一到规范名(小写查别名;未收录的键原样保留,不吞数据)。
func normalizeDimKeys(scores map[string]map[string]int) map[string]map[string]int {
	if scores == nil {
		return scores
	}
	for mode, dims := range scores {
		norm := make(map[string]int, len(dims))
		for k, v := range dims {
			canon := dimAliases[strings.ToLower(k)]
			if canon == "" {
				canon = k
			}
			norm[canon] = v
		}
		scores[mode] = norm
	}
	return scores
}

// rubricDesc 维度 → 打分说明(seed 自定义维度无说明时用通用描述)。
var rubricDesc = map[string]string{
	"论证完整性": "证据链是否完整、每步有依据",
	"连接价值":  "是否建立跨知识连接并解释其意义",
	"矛盾标注":  "是否识别并显式呈现资料矛盾",
	"综合密度":  "是否经综合而非罗列碎片",
	"可迁移性":  "结论能否直接用于新场景/决策",
}

// buildJudgePrompt 按维度列表动态构造评审提示词(seed.Rubric 为空时用 DefaultRubric)。
func buildJudgePrompt(rubric []string) string {
	var dims strings.Builder
	for _, d := range rubric {
		desc := rubricDesc[d]
		if desc == "" {
			desc = "该维度上的表现"
		}
		fmt.Fprintf(&dims, "- %s: %s\n", d, desc)
	}
	return fmt.Sprintf(`你是知识库方案评审(Agent-as-a-Judge)。同一问题给了两套检索方案的答案:
- 方案A = RAG外挂(直接检索原文碎片,原文=未编译的原始资料)
- 方案B = 知识编译复利(检索编译后的知识资产:源摘要/概念/综合页,含意外发现、连接与意义、矛盾标注、置信度)
按以下 %d 个维度对两方案各打 1-5 分(整数):
%s
问题: %%s

方案A(RAG)答案:
%%s

方案B(编译复利)答案:
%%s

只输出 JSON(不要 markdown 围栏,不要其他文字),且 scores 的维度键必须原样使用上面列出的名称(不要翻译成英文或改写):
{"scores":{"rag":{...},"wiki":{...}},"verdict":"B更优|A更优|持平","rationale":"一句话理由"}`,
		len(rubric), dims.String())
}

// Judge 让 LLM 评审两套答案,返回各维度分数。维度取 seed.Rubric(空则 5 维默认)。
// Judge 让 LLM 评审两套答案,返回各维度分数。维度取 seed.Rubric(空则 5 维默认)。
// 解析/调用失败自动重试(最多 3 次):多判去噪把评审次数放大 N 倍,模型输出偏差
// (如把 scores 压成扁平数字 {"rag":3,"wiki":4})的命中率同比例上升,必须重试吸收。
func Judge(ctx context.Context, p provider.Provider, seed Seed, ragAns, wikiAns string) (judgeScores, error) {
	rubric := seed.Rubric
	if len(rubric) == 0 {
		rubric = DefaultRubric
	}
	prompt := fmt.Sprintf(buildJudgePrompt(rubric), seed.Question, ragAns, wikiAns)
	msgs := []provider.Message{
		{Role: "system", Content: "你是严格的评估评审,只输出 JSON。"},
		{Role: "user", Content: prompt},
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		msg, err := p.Chat(ctx, msgs, nil, nil)
		if err != nil {
			lastErr = err
			continue // 网络/超时瞬断,换一轮重试
		}
		out, perr := parseJudgeJSON(msg.Content)
		if perr == nil {
			return out, nil
		}
		lastErr = perr
	}
	return judgeScores{}, lastErr
}

// parseJudgeJSON 清洗并解析评审 JSON(容忍 markdown 围栏/前后缀),维度键归一化。
func parseJudgeJSON(text string) (judgeScores, error) {
	var out judgeScores
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	text = strings.TrimSpace(text)
	text = stripJSONPrefix.ReplaceAllString(text, "")
	text = stripJSONSuffix.ReplaceAllString(text, "}")
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return judgeScores{}, fmt.Errorf("评审 JSON 解析失败: %v\n原文: %.200s", err, text)
	}
	out.Scores = normalizeDimKeys(out.Scores)
	return out, nil
}

// ---- 轨迹评审(过程质量,M10) ----

type trajScores struct {
	Scores    map[string]map[string]int `json:"scores"`
	Rationale string                    `json:"trajRationale"`
	NoiseSpan int                       `json:"-"` // 多判去噪:轨迹 wiki 总分极差
}

const trajectoryJudgePrompt = `你是知识库检索轨迹评审(Agent-as-a-Judge)。同一问题,两套方案各自做了「检索 → 作答」:
- 方案A = RAG外挂:直接检索未编译原文碎片
- 方案B = 知识编译复利:检索编译后的知识资产(源摘要/概念/综合页)
下面分别给出两方案的检索轨迹(命中的资料,按相关度降序带分)与最终答案。
按 3 个维度对两方案各打 1-5 分(整数):
- 检索相关度:检索到的资料与问题的相关程度
- 证据覆盖:关键信息是否都在检索结果里,有无明显遗漏关键页/关键原文
- 证据忠实:最终答案是否严格依据检索到的证据,而非引入外部知识或编造

问题: %s

方案A 检索轨迹:
%s

方案B 检索轨迹:
%s

方案A 答案:
%s

方案B 答案:
%s

只输出 JSON(不要 markdown 围栏,不要其他文字):
{"scores":{"rag":{"检索相关度":1,"证据覆盖":1,"证据忠实":1},"wiki":{...}},"trajRationale":"一句话评语(哪方检索过程更好、差在哪)"}`

// JudgeTrajectory 评审两方案的检索轨迹(过程质量) + 答案。判「对的答案、错的过程」。
// 同样带重试(最多 3 次):轨迹判官与答案判官同暴露在模型输出偏差下。
func JudgeTrajectory(ctx context.Context, p provider.Provider, seed Seed, ragTrace, wikiTrace Trace, ragAns, wikiAns string) (trajScores, error) {
	prompt := fmt.Sprintf(trajectoryJudgePrompt, seed.Question,
		traceText("A", ragTrace), traceText("B", wikiTrace), ragAns, wikiAns)
	msgs := []provider.Message{
		{Role: "system", Content: "你是严格的评估评审,只输出 JSON。"},
		{Role: "user", Content: prompt},
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		msg, err := p.Chat(ctx, msgs, nil, nil)
		if err != nil {
			lastErr = err
			continue
		}
		out, perr := parseTrajJSON(msg.Content)
		if perr == nil {
			return out, nil
		}
		lastErr = perr
	}
	return trajScores{}, lastErr
}

// traceText 把检索轨迹渲染成给 judge 的文本:命中列表(label + score)。
func traceText(prefix string, t Trace) string {
	if len(t.Hits) == 0 {
		return fmt.Sprintf("%s: (检索无命中)", prefix)
	}
	var b strings.Builder
	for i, h := range t.Hits {
		fmt.Fprintf(&b, "  %d. %s (分 %.2f)\n", i+1, h.Label, h.Score)
	}
	return prefix + ":\n" + b.String()
}

// parseTrajJSON 清洗并解析轨迹评审 JSON(容忍 markdown 围栏/前后缀)。
func parseTrajJSON(text string) (trajScores, error) {
	var out trajScores
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	text = strings.TrimSpace(text)
	text = stripJSONPrefix.ReplaceAllString(text, "")
	text = stripJSONSuffix.ReplaceAllString(text, "}")
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return trajScores{}, fmt.Errorf("轨迹评审 JSON 解析失败: %v\n原文: %.200s", err, text)
	}
	out.Scores = normalizeDimKeys(out.Scores)
	return out, nil
}

// ---- 判官去噪(多判取中) ----

// medianInt 整数切片中位数(偶数取中间两值均值,与 1-5 整数档兼容)。
func medianInt(vals []int) int {
	sort.Ints(vals)
	n := len(vals)
	if n%2 == 1 {
		return vals[n/2]
	}
	return (vals[n/2-1] + vals[n/2]) / 2
}

// aggregateJudge 把 N 轮独立评审聚合去噪:判官单次方差实测 ±3-5 分/种子,
// 大于单轮编译带来的 wiki 增强——单次 Δ 会被噪声主导。
// - 分数:每模式每维度取中位数
// - verdict:多数票(平票 → 持平)
// - rationale:取多数票那一轮的
// - NoiseSpan:单次 wiki 总分的极差 = 本种子判官噪声底,供 render 判断 Δ 是否落在噪声内
func aggregateJudge(runs []judgeScores) judgeScores {
	out := judgeScores{Scores: map[string]map[string]int{}}
	if len(runs) == 1 {
		out = runs[0]
		out.NoiseSpan = 0
		return out
	}
	votes := map[string]int{}
	wikiTotals := make([]int, 0, len(runs))
	for _, r := range runs {
		votes[r.Verdict]++
		total := 0
		for _, v := range r.Scores["wiki"] {
			total += v
		}
		wikiTotals = append(wikiTotals, total)
	}
	for mode := range runs[0].Scores {
		dims := map[string][]int{}
		for _, r := range runs {
			for d, v := range r.Scores[mode] {
				dims[d] = append(dims[d], v)
			}
		}
		m := map[string]int{}
		for d, vals := range dims {
			m[d] = medianInt(vals)
		}
		out.Scores[mode] = m
	}
	best, top := "持平", -1
	for v, c := range votes {
		if c > top && c > len(runs)/2 { // 严格多数才生效,否则回落持平(避免 map 顺序随机)
			best, top = v, c
		}
	}
	out.Verdict = best
	for _, r := range runs {
		if r.Verdict == best {
			out.Rationale = r.Rationale
			break
		}
	}
	out.NoiseSpan = maxInt(wikiTotals) - minInt(wikiTotals)
	return out
}

// aggregateTraj 聚合 N 轮轨迹评审:分数取中位数,rationale 取 wiki 总分居中那一轮。
func aggregateTraj(runs []trajScores) trajScores {
	out := trajScores{Scores: map[string]map[string]int{}}
	if len(runs) == 1 {
		out = runs[0]
		out.NoiseSpan = 0
		return out
	}
	type runT struct {
		wikiTotal int
		r         trajScores
	}
	order := make([]runT, 0, len(runs))
	for _, r := range runs {
		total := 0
		for _, v := range r.Scores["wiki"] {
			total += v
		}
		order = append(order, runT{total, r})
	}
	sort.Slice(order, func(i, j int) bool { return order[i].wikiTotal < order[j].wikiTotal })
	for mode := range runs[0].Scores {
		dims := map[string][]int{}
		for _, r := range runs {
			for d, v := range r.Scores[mode] {
				dims[d] = append(dims[d], v)
			}
		}
		m := map[string]int{}
		for d, vals := range dims {
			m[d] = medianInt(vals)
		}
		out.Scores[mode] = m
	}
	mid := order[len(order)/2]
	out.Rationale = mid.r.Rationale
	out.NoiseSpan = order[len(order)-1].wikiTotal - order[0].wikiTotal
	return out
}

func maxInt(vals []int) int {
	m := vals[0]
	for _, v := range vals[1:] {
		if v > m {
			m = v
		}
	}
	return m
}

func minInt(vals []int) int {
	m := vals[0]
	for _, v := range vals[1:] {
		if v < m {
			m = v
		}
	}
	return m
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// ---- 报告 ----

// CaseResult 单条种子的完整结果。
type CaseResult struct {
	Seed      Seed
	RAGAns    string
	WikiAns   string
	Judge     judgeScores
	RAGTrace  Trace // M10 轨迹:过程质量评审的输入
	WikiTrace Trace
	Traj      trajScores
}

// Baseline 上次评估的快照(供 --rebaseline 对比)。复利是时间序列:
// 「wiki 得分随时间 Δ」才是编译复利本身的曲线——同种子重跑,看 wiki 有没有越变越强。
type Baseline struct {
	Time  time.Time `json:"time"`
	Model string    `json:"model"`
	// seedID → mode(rag|wiki) → dim → score
	Seeds map[string]map[string]map[string]int `json:"seeds"`
}

// CollectBaseline 从本轮结果构造基线快照。
func CollectBaseline(cases []CaseResult, model string) Baseline {
	bl := Baseline{Time: time.Now(), Model: model, Seeds: map[string]map[string]map[string]int{}}
	for _, c := range cases {
		bl.Seeds[c.Seed.ID] = map[string]map[string]int{"rag": {}, "wiki": {}}
		for _, mode := range []string{"rag", "wiki"} {
			for dim, v := range c.Judge.Scores[mode] {
				bl.Seeds[c.Seed.ID][mode][dim] = v
			}
		}
	}
	return bl
}

// LoadBaseline 读最近一次评估快照;缺失/损坏返回 nil(首次评估无基线)。
func LoadBaseline(root string) *Baseline {
	b, err := os.ReadFile(filepath.Join(root, "eval", "reports", "latest.json"))
	if err != nil {
		return nil
	}
	var bl Baseline
	if err := json.Unmarshal(b, &bl); err != nil || len(bl.Seeds) == 0 {
		return nil
	}
	return &bl
}

// Save 原子写基线快照(最新一次评估的机器可读分数,供下次 --rebaseline 读)。
func (bl *Baseline) Save(root string) {
	dir := filepath.Join(root, "eval", "reports")
	_ = os.MkdirAll(dir, 0o755)
	b, err := json.MarshalIndent(bl, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(dir, "latest.json"), b, 0o644)
}

// RunCompare 跑完整对照实验:两方案各答题 → 评审 → 报告。
// rebaseline=true 时读上次基线,报告里输出「wiki 复利 Δ」——同种子重跑才测得复利曲线。
func RunCompare(root string, cfg config.Config, seeds []Seed, rebaseline bool) (string, error) {
	p := newProvider(cfg)
	if cfg.APIKey == "" && cfg.BaseURL == "" {
		return "", fmt.Errorf("未配置 KCP_API_KEY,无法运行 LLM 评估")
	}
	opts := RetrievalOpts(root, cfg)
	rawDocs, wikiDocs := RawDocs(root), WikiDocs(root)

	var cases []CaseResult
	for _, s := range seeds {
		// 逐调用超时:慢 provider 下 4 次 LLM 调用(答题×2+判官×2)挤不进单种子硬窗口
		// (实测首轮 gc2 在第 4 次调用跑爆 90s deadline)。每次调用独立 ctx,
		// 超时用 cfg.EvalTimeout(默认 90s/次),单次慢调用不拖累整个种子。
		newCtx := func() (context.Context, context.CancelFunc) {
			return context.WithTimeout(context.Background(), time.Duration(cfg.EvalTimeout)*time.Second)
		}
		ctx, cancel := newCtx()
		ragAns, ragTrace, err := Answer(ctx, p, rawDocs, s.Question, opts, cfg.RetrieveK)
		cancel()
		if err != nil {
			return "", fmt.Errorf("RAG 答题失败 [%s]: %w", s.ID, err)
		}
		ctx, cancel = newCtx()
		wikiAns, wikiTrace, err := Answer(ctx, p, wikiDocs, s.Question, opts, cfg.RetrieveK)
		cancel()
		if err != nil {
			return "", fmt.Errorf("WIKI 答题失败 [%s]: %w", s.ID, err)
		}
		// 判官去噪:同一对答案独立评审 cfg.JudgePasses 轮(默认 3),取中位数 + 多数票。
		// 单次评审实测方差 ±3-5 分/种子,直接拿单轮分数比 1-1 判涨跌会被噪声主导。
		passes := cfg.JudgePasses
		if passes < 1 {
			passes = 1 // 显式 0 = 退化为单判
		}
		judgeRuns := make([]judgeScores, 0, passes)
		for i := 0; i < passes; i++ {
			ctx, cancel = newCtx()
			j, err := Judge(ctx, p, s, ragAns, wikiAns)
			cancel()
			if err != nil {
				// 单轮评审失败不杀整种子:多判场景一轮偏差(网络/模型输出畸形)跳过,
				// 聚合用剩余有效轮次;全部失败才判种子失败。
				fmt.Printf("  ⚠️ [%s] 评审第 %d 轮失败,跳过: %v\n", s.ID, i+1, err)
				continue
			}
			judgeRuns = append(judgeRuns, j)
		}
		if len(judgeRuns) == 0 {
			return "", fmt.Errorf("评审失败 [%s]: 全部 %d 轮均失败", s.ID, passes)
		}
		j := aggregateJudge(judgeRuns)
		// M10 轨迹判官:评价检索过程质量,捕捉「对的答案、错的过程」。同样多判去噪。
		trajRuns := make([]trajScores, 0, passes)
		for i := 0; i < passes; i++ {
			ctx, cancel = newCtx()
			tj, err := JudgeTrajectory(ctx, p, s, ragTrace, wikiTrace, ragAns, wikiAns)
			cancel()
			if err != nil {
				fmt.Printf("  ⚠️ [%s] 轨迹评审第 %d 轮失败,跳过: %v\n", s.ID, i+1, err)
				continue
			}
			trajRuns = append(trajRuns, tj)
		}
		if len(trajRuns) == 0 {
			return "", fmt.Errorf("轨迹评审失败 [%s]: 全部 %d 轮均失败", s.ID, passes)
		}
		tj := aggregateTraj(trajRuns)
		ragTrace.Mode, wikiTrace.Mode = "rag", "wiki"
		cases = append(cases, CaseResult{Seed: s, RAGAns: ragAns, WikiAns: wikiAns, Judge: j,
			RAGTrace: ragTrace, WikiTrace: wikiTrace, Traj: tj})
		fmt.Printf("✓ %s: verdict=%s · 轨迹优=%s\n", s.ID, j.Verdict, trajWinner(tj))
	}
	report := render(cases, cfg.Model, prevBaseline(root, rebaseline))
	bl := CollectBaseline(cases, cfg.Model)
	bl.Save(root) // 写最新基线:下次 --rebaseline 有对比对象
	fp := writeReport(root, report)
	fmt.Println("报告已写入: " + fp)
	return report, nil
}

// prevBaseline rebaseline 模式才加载上次基线;普通评估跑无 Δ(基线仍会写,供下次)。
func prevBaseline(root string, rebaseline bool) *Baseline {
	if !rebaseline {
		return nil
	}
	return LoadBaseline(root)
}

func render(cases []CaseResult, model string, prev *Baseline) string {
	var b strings.Builder
	b.WriteString("# 对照实验报告: RAG 外挂 vs 知识编译复利\n\n")
	b.WriteString(fmt.Sprintf("时间: %s · 模型: %s · 种子数: %d\n\n", time.Now().Format("2006-01-02 15:04"), model, len(cases)))
	if prev != nil {
		b.WriteString(fmt.Sprintf("> **复利对比(--rebaseline)**: vs 上次(%s, %s)\n\n", prev.Time.Format("2006-01-02 15:04"), prev.Model))
	}

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

	// M10 轨迹质量汇总:检索过程维度(过程好 ≠ 答案好)。
	tavg := func(dim string) (float64, float64) {
		totalA, totalB, n := 0, 0, 0
		for _, c := range cases {
			if c.Traj.Scores == nil {
				continue
			}
			totalA += c.Traj.Scores["rag"][dim]
			totalB += c.Traj.Scores["wiki"][dim]
			n++
		}
		if n == 0 {
			return 0, 0
		}
		return float64(totalA) / float64(n), float64(totalB) / float64(n)
	}
	b.WriteString("\n| 轨迹维度 | RAG | 编译 | Δ |\n|---|---|---|---|\n")
	for _, dim := range TrajRubric {
		a, w := tavg(dim)
		b.WriteString(fmt.Sprintf("| %s | %.1f | %.1f | %+.1f |\n", dim, a, w, w-a))
	}

	for _, c := range cases {
		note := c.Seed.Note
		note = strings.TrimPrefix(note, "期望:")
		note = strings.TrimSpace(note)
		b.WriteString(fmt.Sprintf("\n---\n\n## %s %s\n\n> 期望: %s\n\n**RAG 答案**:\n\n%s\n\n**编译复利答案**:\n\n%s\n\n**评审**: verdict=%s · %s\n",
			c.Seed.ID, c.Seed.Question, note, c.RAGAns, c.WikiAns, c.Judge.Verdict, c.Judge.Rationale))
		// 复利 Δ:wiki 侧总分 vs 上次(同种子)——wiki 是否越变越强是复利曲线的直接读数。
		if prev != nil {
			if p, ok := prev.Seeds[c.Seed.ID]; ok {
				nowTotal, beforeTotal := 0, 0
				for dim, v := range c.Judge.Scores["wiki"] {
					nowTotal += v
					if pv, ok2 := p["wiki"][dim]; ok2 {
						beforeTotal += pv
					}
				}
				delta := nowTotal - beforeTotal
				// 判官噪声底:本种子多判 wiki 总分极差。|Δ| 在噪声内时不断言涨跌——
				// 单判噪声 ±3-5 分,一次 rejudge 抖动不该被读成 wiki 变强/变弱。
				floor := c.Judge.NoiseSpan
				if floor < 0 {
					floor = 0
				}
				marker := ""
				if absInt(delta) <= floor {
					marker = fmt.Sprintf(" (±%d 判官噪声内,不判涨跌)", floor)
				} else if floor > 0 {
					marker = fmt.Sprintf(" (超出噪声底 ±%d)", floor)
				}
				b.WriteString(fmt.Sprintf("\n**复利 Δ(wiki vs 上次)**: %+d%s\n", delta, marker))
			}
		}
		scores := map[string]map[string]int{"RAG": c.Judge.Scores["rag"], "WIKI": c.Judge.Scores["wiki"]}
		dimList := DefaultRubric
		if len(c.Seed.Rubric) > 0 {
			dimList = c.Seed.Rubric
		}
		for _, m := range []string{"RAG", "WIKI"} {
			b.WriteString(fmt.Sprintf("%s: ", m))
			for _, dim := range dimList {
				if v, ok := scores[m][dim]; ok {
					b.WriteString(fmt.Sprintf("%s=%d ", dim, v))
				}
			}
			b.WriteString("\n")
		}
		// M10 检索轨迹 + 过程质量评审
		b.WriteString("\n**检索轨迹**:\n\n")
		for _, t := range []struct {
			name string
			tr   Trace
		}{{"RAG", c.RAGTrace}, {"编译复利", c.WikiTrace}} {
			b.WriteString(fmt.Sprintf("- %s 命中 %d 条:", t.name, len(t.tr.Hits)))
			for i, h := range t.tr.Hits {
				if i >= 8 {
					b.WriteString(" …")
					break
				}
				b.WriteString(fmt.Sprintf(" [%s %.2f]", h.Label, h.Score))
			}
			b.WriteString("\n")
		}
		if c.Traj.Scores != nil {
			b.WriteString("轨迹评审: ")
			for _, m := range []string{"RAG", "WIKI"} {
				b.WriteString(fmt.Sprintf("%s: ", m))
				for _, dim := range TrajRubric {
					if v, ok := c.Traj.Scores[strings.ToLower(m)][dim]; ok {
						b.WriteString(fmt.Sprintf("%s=%d ", dim, v))
					}
				}
				b.WriteString(" ")
			}
			b.WriteString("\n")
			if c.Traj.Rationale != "" {
				b.WriteString(fmt.Sprintf("轨迹评语: %s\n", c.Traj.Rationale))
			}
		}
	}
	return b.String()
}

// trajWinner 按轨迹维度总分判过程优胜方(用于命令行 ✓ 输出)。
func trajWinner(t trajScores) string {
	sumA, sumB := 0, 0
	if t.Scores == nil {
		return "无轨迹分"
	}
	for _, d := range TrajRubric {
		sumA += t.Scores["rag"][d]
		sumB += t.Scores["wiki"][d]
	}
	switch {
	case sumB > sumA:
		return "B"
	case sumA > sumB:
		return "A"
	default:
		return "持平"
	}
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
