package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gcc2ge/knowledge-compounder/internal/retrieval"
	"github.com/gcc2ge/knowledge-compounder/internal/tools"
)

// 概念页用弱标记(分类口径差异)且无 lint 可识别标记 → 应报警归一化。
func TestConceptWeakMarkerProblems_Fires(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "concepts/背压.md", "## 张力与缺口\n- ⚠️ 分类口径差异: [[Go并发实战进阶节选]] — 两套 taxonomy 并存\n")
	hits := []tools.ContradictionHit{{Result: retrieval.Result{Path: "concepts/背压.md", Label: "背压"}}}
	got := conceptWeakMarkerProblems(dir, hits)
	if len(got) != 1 || got[0] != "背压" {
		t.Fatalf("弱标记应报警,得 %v", got)
	}
}

// 概念页已有 lint 可识别标记(⚠️ 冲突:) → 不报警(既有正常冲突页)。
func TestConceptWeakMarkerProblems_SkipWhenStdMarker(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "concepts/背压.md", "## 张力与缺口\n- ⚠️ 冲突: [[Go并发实战进阶节选]] — 两套 taxonomy 并存\n")
	hits := []tools.ContradictionHit{{Result: retrieval.Result{Path: "concepts/背压.md", Label: "背压"}}}
	if got := conceptWeakMarkerProblems(dir, hits); len(got) != 0 {
		t.Fatalf("已有标准标记不应报警,得 %v", got)
	}
}

// 概念页带标准标记但仍有用弱词描述的其他张力 → 不报警(页面已被 lint 计入,不重复)。
func TestConceptWeakMarkerProblems_SkipWhenAnyStdMarker(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "concepts/goroutine泄漏.md", "## 张力与缺口\n- ⚠️ 冲突: [[Go并发实战进阶节选]] — 两套 taxonomy 并存\n- ⚠️ 分类口径差异: [[context取消传播实战]] — 九成归因经验值\n")
	hits := []tools.ContradictionHit{{Result: retrieval.Result{Path: "concepts/goroutine泄漏.md", Label: "goroutine泄漏"}}}
	if got := conceptWeakMarkerProblems(dir, hits); len(got) != 0 {
		t.Fatalf("页面已有标准标记时不应报警,得 %v", got)
	}
}

// 非概念页(源页)的弱标记不在本检查范围 → 不报警(源页有独立的弱词检查)。
func TestConceptWeakMarkerProblems_IgnoresSourcePages(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "sources/x.md", "## 连接\n- ⚠️ 分类口径差异: [[y]] — 说明\n")
	hits := []tools.ContradictionHit{{Result: retrieval.Result{Path: "sources/x.md", Label: "x"}}}
	if got := conceptWeakMarkerProblems(dir, hits); len(got) != 0 {
		t.Fatalf("源页弱标记不应在本检查报警,得 %v", got)
	}
}

// 文件不存在/不可读 → 静默跳过,不 panic。
func TestConceptWeakMarkerProblems_MissingPage(t *testing.T) {
	dir := t.TempDir()
	hits := []tools.ContradictionHit{{Result: retrieval.Result{Path: "concepts/不存在.md", Label: "不存在"}}}
	if got := conceptWeakMarkerProblems(dir, hits); len(got) != 0 {
		t.Fatalf("缺失页应静默跳过,得 %v", got)
	}
}

func writeFixture(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
