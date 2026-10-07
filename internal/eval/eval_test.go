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
