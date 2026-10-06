package tools

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gcc2ge/knowledge-compounder/internal/retrieval"
)

// 扫出相关概念页但未回流 → MissingEvidence 列出(FinishGuard 强制对象)。
func TestContradictionLog_MissingEvidenceFires(t *testing.T) {
	l := &ContradictionLog{}
	l.Mark("源A", []string{"背压", "分布式事务"})
	l.MarkConcepts("源A", []string{"背压", "分布式事务"})
	if !l.Scanned("源A") {
		t.Fatal("应视为已扫描")
	}
	missing := l.MissingEvidence("源A")
	if len(missing) != 2 {
		t.Fatalf("未裁决概念页应全部列出,得 %v", missing)
	}
}

// 全部裁决(corroborate/contradict/skip)→ MissingEvidence 为空,FinishGuard 放行。
func TestContradictionLog_AllApplied(t *testing.T) {
	l := &ContradictionLog{}
	l.Mark("源A", []string{"背压", "分布式事务", "MCP"})
	l.MarkConcepts("源A", []string{"背压", "MCP"}) // 分布式事务是 source/entity 页,不进回流候选
	l.Applied("源A", "背压")
	l.Applied("源A", "MCP")
	if got := l.MissingEvidence("源A"); len(got) != 0 {
		t.Fatalf("全裁决后应为空,得 %v", got)
	}
}

// 部分裁决 → 只列出未裁决的。
func TestContradictionLog_PartialApplied(t *testing.T) {
	l := &ContradictionLog{}
	l.MarkConcepts("源A", []string{"背压", "分布式事务"})
	l.Applied("源A", "背压")
	got := l.MissingEvidence("源A")
	if len(got) != 1 || got[0] != "分布式事务" {
		t.Fatalf("应只列未裁决页,得 %v", got)
	}
}

// Applied 幂等:重复裁决不重复登记。
func TestContradictionLog_AppliedIdempotent(t *testing.T) {
	l := &ContradictionLog{}
	l.MarkConcepts("源A", []string{"背压"})
	l.Applied("源A", "背压")
	l.Applied("源A", "背压")
	if got := l.MissingEvidence("源A"); len(got) != 0 {
		t.Fatalf("重复裁决应幂等,得 %v", got)
	}
}

// 未跑 check_contradictions → MissingEvidence 返回 nil(不重复触发,由 Scanned 闸先拦)。
func TestContradictionLog_NoScanNoMissing(t *testing.T) {
	l := &ContradictionLog{}
	if got := l.MissingEvidence("源A"); got != nil {
		t.Fatalf("未扫描不应报缺失,得 %v", got)
	}
}

// conceptLabels 只挑概念页(路径含 /concepts/)。
func TestConceptLabels_FiltersConcepts(t *testing.T) {
	hits := []ContradictionHit{
		{Result: retrieval.Result{Path: "wiki/sources/源A.md", Label: "源A"}},
		{Result: retrieval.Result{Path: "wiki/concepts/背压.md", Label: "背压"}},
		{Result: retrieval.Result{Path: "wiki/synthesis/x.md", Label: "x"}},
	}
	got := conceptLabels(hits)
	want := []string{"背压"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("应只取概念页,得 %v", got)
	}
}

// skip 是合法裁决:登记后 MissingEvidence 为空。
func TestContradictionLog_SkipCountsAsApplied(t *testing.T) {
	l := &ContradictionLog{}
	l.MarkConcepts("源A", []string{"背压"})
	l.Applied("源A", "背压") // skip 在工具层走同一 Applied
	if got := l.MissingEvidence("源A"); len(got) != 0 {
		t.Fatalf("skip 应算已裁决,得 %v", got)
	}
}

// 台账闭环集成:扫出概念页 背压 → 未裁决被强制 → 裁决后放行。
func TestEvidenceLoopIntegration(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "wiki/sources/源A.md", "# 源A\n\n## 一句话结论\n背压是队列满时的反压信号,需要生产者降速。\n\n## 论证链\n队列满时触发背压,抑制生产者速率。\n")
	writeFixture(t, root, "wiki/concepts/背压.md", "# 背压\n\n## 定义\n队列满时对生产者的反压信号。\n\n## 外部观点\n- 来源\n\n## 张力与缺口\n- 待验证\n")
	writeFixture(t, root, "wiki/sources/源B.md", "# 源B\n\n## 一句话结论\n分布式事务需两阶段提交。\n\n## 论证链\n跨服务原子性。\n")

	src := readFixture(t, root, "wiki/sources/源A.md")
	hits := ScanContradictions(root, "源A", src, 4)
	if len(hits) == 0 {
		t.Fatal("应能扫出相关页(概念页 背压)")
	}
	l := &ContradictionLog{}
	var conceptHits []ContradictionHit
	for _, h := range hits {
		if strings.Contains(h.Result.Path, "/concepts/") {
			conceptHits = append(conceptHits, h)
		}
	}
	l.Mark("源A", pageLabels(hits))
	l.MarkConcepts("源A", conceptLabels(conceptHits))
	if got := l.MissingEvidence("源A"); len(got) == 0 {
		t.Fatalf("扫出概念页 背压 但未裁决,应被强制,得 %v", got)
	}
	l.Applied("源A", "背压")
	if got := l.MissingEvidence("源A"); len(got) != 0 {
		t.Fatalf("裁决后应放行,得 %v", got)
	}
}

func writeFixture(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFixture(t *testing.T, root, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
