package eval

import (
	"reflect"
	"strings"
	"testing"
)

// 英文变体(判官实测输出)归一到规范中文维度。
func TestNormalizeDimKeys_EnglishVariants(t *testing.T) {
	in := map[string]map[string]int{
		"rag":  {"connection_value": 2, "synthesis_density": 3, "论证完整性": 4},
		"wiki": {"connection_value": 3, "comprehensive_density": 4, "论证完整性": 5},
	}
	got := normalizeDimKeys(in)
	want := map[string]map[string]int{
		"rag":  {"连接价值": 2, "综合密度": 3, "论证完整性": 4},
		"wiki": {"连接价值": 3, "综合密度": 4, "论证完整性": 5},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("英文变体应归一到中文维度,得 %v", got)
	}
}

// 规范中文键不应被改动。
func TestNormalizeDimKeys_CanonicalUntouched(t *testing.T) {
	in := map[string]map[string]int{"wiki": {"连接价值": 5, "矛盾标注": 2}}
	if !reflect.DeepEqual(normalizeDimKeys(in), in) {
		t.Fatal("规范中文键不应被改动")
	}
}

// 未收录的键原样保留(不吞数据,扩展别名表前的兜底)。
func TestNormalizeDimKeys_UnknownKept(t *testing.T) {
	in := map[string]map[string]int{"wiki": {"novel_dim": 3}}
	if !reflect.DeepEqual(normalizeDimKeys(in), in) {
		t.Fatal("未知键应原样保留")
	}
}

// 大小写不敏感:英文变体混合大小写也能命中别名。
func TestNormalizeDimKeys_CaseInsensitive(t *testing.T) {
	in := map[string]map[string]int{"wiki": {"Connection_Value": 4}}
	got := normalizeDimKeys(in)
	if got["wiki"]["连接价值"] != 4 {
		t.Fatalf("混合大小写英文变体应归一到中文,得 %v", got)
	}
}

// nil 输入安全(无分数时判官可能给空 scores)。
func TestNormalizeDimKeys_Nil(t *testing.T) {
	if normalizeDimKeys(nil) != nil {
		t.Fatal("nil 输入应原样返回")
	}
}

// 归一化闭环:跨轮同一维度键(key 漂移)归一后一致,Δ 才不虚高。
func TestNormalizeDimKeys_DeltaComparable(t *testing.T) {
	prev := map[string]map[string]int{"wiki": {"连接价值": 2, "综合密度": 2}}
	now := normalizeDimKeys(map[string]map[string]int{"wiki": {"connection_value": 4, "synthesis_density": 3}})
	sum := func(m map[string]map[string]int, mode string) int {
		total := 0
		for _, v := range m[mode] {
			total += v
		}
		return total
	}
	nowTotal, beforeTotal := sum(now, "wiki"), sum(prev, "wiki")
	// 归一化前:now 的 key 与 prev 完全对不上,beforeTotal=0,Δ 虚高为 7;
	// 归一化后:键对齐,beforeTotal=4,真实 Δ = 7-4 = 3。
	if beforeTotal != 4 {
		t.Fatalf("归一化后键应跨轮对齐,得 beforeTotal=%d", beforeTotal)
	}
	if nowTotal-beforeTotal != 3 {
		t.Fatalf("真实 Δ 应为 3,得 %d", nowTotal-beforeTotal)
	}
}

// parseTrajJSON 应归一轨迹维度键(中英混用兜底)。
func TestParseTrajJSON_NormalizesDims(t *testing.T) {
	raw := `{"scores":{"rag":{"retrieval_relevance":5,"证据覆盖":2,"evidence_fidelity":4},"wiki":{"检索相关度":4,"证据覆盖":5,"证据忠实":5}},"trajRationale":"B优"}`
	out, err := parseTrajJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if out.Scores["rag"]["检索相关度"] != 5 || out.Scores["rag"]["证据忠实"] != 4 {
		t.Fatalf("轨迹英文键应归一,得 %v", out.Scores["rag"])
	}
	if out.Scores["wiki"]["检索相关度"] != 4 || out.Scores["wiki"]["证据覆盖"] != 5 {
		t.Fatalf("规范键应保留,得 %v", out.Scores["wiki"])
	}
	if !strings.Contains(out.Rationale, "B优") {
		t.Fatalf("rationale 应保留,得 %q", out.Rationale)
	}
}

// 判官把 scores 压成扁平数字(实测 {"rag":3,"wiki":4})→ 解析必须拒绝,由重试吸收。
func TestParseJudgeJSON_RejectsFlatScores(t *testing.T) {
	raw := `{"scores":{"rag":3,"wiki":4},"verdict":"B更优","rationale":"扁平"}`
	if _, err := parseJudgeJSON(raw); err == nil {
		t.Fatal("扁平 scores 形状应解析失败(触发重试),而不是被静默接受")
	}
}

// 标准形状(含英文变体键)解析并归一成功。
func TestParseJudgeJSON_StandardShape(t *testing.T) {
	raw := `{"scores":{"rag":{"论证完整性":3,"connection_value":2},"wiki":{"论证完整性":4,"connection_value":3}},"verdict":"B更优","rationale":"ok"}`
	out, err := parseJudgeJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if out.Scores["wiki"]["连接价值"] != 3 || out.Scores["rag"]["论证完整性"] != 3 {
		t.Fatalf("应解析成功并归一,得 %v", out.Scores)
	}
	if out.Verdict != "B更优" {
		t.Fatalf("verdict 应保留,得 %s", out.Verdict)
	}
}

// ---- 判官去噪 ----

// 中位数:奇数取中间值,偶数取中间两值均值。
func TestMedianInt(t *testing.T) {
	if got := medianInt([]int{3, 5, 4}); got != 4 {
		t.Fatalf("奇数取中间值,得 %d", got)
	}
	if got := medianInt([]int{3, 4}); got != 3 { // (3+4)/2=3
		t.Fatalf("偶数取均值,得 %d", got)
	}
	if got := medianInt([]int{5, 5, 5}); got != 5 {
		t.Fatalf("全等应稳定,得 %d", got)
	}
}

// 多判聚合:分数取各维度中位数、verdict 取多数票、rationale 取多数票那轮、噪声底=极差。
func TestAggregateJudge_MedianAndMajority(t *testing.T) {
	runs := []judgeScores{
		{Scores: map[string]map[string]int{"wiki": {"论证完整性": 3, "连接价值": 2}}, Verdict: "B更优", Rationale: "r1"},
		{Scores: map[string]map[string]int{"wiki": {"论证完整性": 5, "连接价值": 4}}, Verdict: "B更优", Rationale: "r2"},
		{Scores: map[string]map[string]int{"wiki": {"论证完整性": 4, "连接价值": 3}}, Verdict: "A更优", Rationale: "r3"},
	}
	out := aggregateJudge(runs)
	if out.Scores["wiki"]["论证完整性"] != 4 || out.Scores["wiki"]["连接价值"] != 3 {
		t.Fatalf("分数应取中位数,得 %v", out.Scores["wiki"])
	}
	if out.Verdict != "B更优" {
		t.Fatalf("verdict 应取多数票 B更优,得 %s", out.Verdict)
	}
	if out.Rationale != "r1" {
		t.Fatalf("rationale 应取多数票那轮,得 %q", out.Rationale)
	}
	// wiki 总分:r1=5, r2=9, r3=7 → 极差 9-5=4
	if out.NoiseSpan != 4 {
		t.Fatalf("噪声底应为 4,得 %d", out.NoiseSpan)
	}
}

// 平票 → 持平(多数票兜底)。
func TestAggregateJudge_TieBecomesEven(t *testing.T) {
	runs := []judgeScores{
		{Verdict: "B更优"},
		{Verdict: "A更优"},
	}
	out := aggregateJudge(runs)
	if out.Verdict != "持平" {
		t.Fatalf("平票应回落为持平,得 %s", out.Verdict)
	}
}

// 单判(去噪关闭)直接透传,噪声底为 0。
func TestAggregateJudge_SinglePassThrough(t *testing.T) {
	one := judgeScores{Scores: map[string]map[string]int{"wiki": {"论证完整性": 4}}, Verdict: "B更优", Rationale: "r"}
	out := aggregateJudge([]judgeScores{one})
	if out.NoiseSpan != 0 || out.Verdict != "B更优" || out.Rationale != "r" {
		t.Fatalf("单判应透传,得 %+v", out)
	}
}

// 轨迹聚合:分数取中位数,NoiseSpan 按 wiki 总分极差,不同模式独立聚合。
func TestAggregateTraj(t *testing.T) {
	runs := []trajScores{
		{Scores: map[string]map[string]int{"rag": {"检索相关度": 2}, "wiki": {"检索相关度": 3, "证据覆盖": 2}}, Rationale: "t1"},
		{Scores: map[string]map[string]int{"rag": {"检索相关度": 4}, "wiki": {"检索相关度": 5, "证据覆盖": 4}}, Rationale: "t2"},
		{Scores: map[string]map[string]int{"rag": {"检索相关度": 3}, "wiki": {"检索相关度": 4, "证据覆盖": 3}}, Rationale: "t3"},
	}
	out := aggregateTraj(runs)
	if out.Scores["rag"]["检索相关度"] != 3 || out.Scores["wiki"]["证据覆盖"] != 3 {
		t.Fatalf("分数应取中位数,得 rag=%v wiki=%v", out.Scores["rag"], out.Scores["wiki"])
	}
	// wiki 总分:t1=5, t2=9, t3=7 → 极差 4
	if out.NoiseSpan != 4 {
		t.Fatalf("轨迹噪声底应为 4,得 %d", out.NoiseSpan)
	}
	if out.Rationale != "t3" {
		t.Fatalf("rationale 应取 wiki 总分居中那轮(t3=7),得 %q", out.Rationale)
	}
}
