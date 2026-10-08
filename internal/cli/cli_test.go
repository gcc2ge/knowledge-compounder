package cli

import (
	"os"
	"path/filepath"
	"testing"
	"time"

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

// ---- 幂等重编译判定(FinishGuard 收尾码区分子句) ----

// 完整 9 节源页骨架(preflight 节检查要求:一句话结论/论证链/关键细节/作者立场与定位/
// 意外发现/疑点/术语/连接/引用,缺任何一节都会阻断)。
func fullSourcePage(slug string) string {
	return "# " + slug + "\n\n" +
		"## 一句话结论\nok\n" +
		"## 论证链\nok\n" +
		"## 关键细节\nok\n" +
		"## 作者立场与定位\nok\n" +
		"## 意外发现\nok\n" +
		"## 疑点\nok\n" +
		"## 术语\nok\n" +
		"## 连接\nok\n" +
		"## 引用\nok\n"
}

// statSrc 读取源页文件信息;读取失败直接终止测试。
func statSrc(t *testing.T, srcP string) os.FileInfo {
	t.Helper()
	fi, err := os.Stat(srcP)
	if err != nil {
		t.Fatal(err)
	}
	return fi
}

// 基础 fixture:纯文本 raw(无硬资产,preflight 天然零问题)+ 已存在的完整源页。
// 默认时间:raw 比源页旧 2 小时(页面不陈旧),模拟"上一轮已编译、本轮重编译"。
func idempotentFixture(t *testing.T) (dir, rawRel, srcP string) {
	t.Helper()
	dir = t.TempDir()
	rawRel = "raw/x.md"
	srcP = filepath.Join(dir, "wiki", "sources", "x.md")
	writeFixture(t, dir, rawRel, "纯文本 raw,无代码块/表/示例,preflight 天然零问题。")
	writeFixture(t, dir, "wiki/sources/x.md", fullSourcePage("x"))
	now := time.Now()
	if err := os.Chtimes(filepath.Join(dir, rawRel), now.Add(-2*time.Hour), now.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(srcP, now.Add(-1*time.Hour), now.Add(-1*time.Hour)); err != nil {
		t.Fatal(err)
	}
	return dir, rawRel, srcP
}

// 源页已最新 + 模型有真实参与(≥1 工具调用)→ 合法幂等重编译,放行。
func TestIdempotentRecompile_AllowsFreshPage(t *testing.T) {
	dir, rawRel, srcP := idempotentFixture(t)
	fi := statSrc(t, srcP)
	if !idempotentRecompile(dir, rawRel, fi, srcP, 5) {
		t.Fatal("源页最新 + 模型有参与应判幂等成功")
	}
}

// 零工具调用(真偷懒/假成功)→ 不放行,仍判失败。
func TestIdempotentRecompile_RejectsZeroWork(t *testing.T) {
	dir, rawRel, srcP := idempotentFixture(t)
	fi := statSrc(t, srcP)
	if idempotentRecompile(dir, rawRel, fi, srcP, 0) {
		t.Fatal("零工具调用(假成功)不应判幂等")
	}
}

// raw 比源页新 = 源页陈旧 → 不放行,必须重写。
func TestIdempotentRecompile_RejectsStalePage(t *testing.T) {
	dir, rawRel, srcP := idempotentFixture(t)
	now := time.Now()
	// 翻转:raw 新、源页旧 → 页面陈旧
	if err := os.Chtimes(filepath.Join(dir, rawRel), now.Add(-30*time.Minute), now.Add(-30*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(srcP, now.Add(-3*time.Hour), now.Add(-3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	fi := statSrc(t, srcP)
	if idempotentRecompile(dir, rawRel, fi, srcP, 5) {
		t.Fatal("源页陈旧(比 raw 旧)不应判幂等")
	}
}

// 源页不存在 → 不放行(1:1 映射要求源页存在)。
func TestIdempotentRecompile_RejectsMissingPage(t *testing.T) {
	dir, rawRel, _ := idempotentFixture(t)
	if idempotentRecompile(dir, rawRel, nil, filepath.Join(dir, "wiki/sources/x.md"), 3) {
		t.Fatal("源页不存在不应判幂等")
	}
}

// preflight 硬资产缺失 → 不放行(页面有阻断性缺失)。
func TestIdempotentRecompile_RejectsMissingHardAssets(t *testing.T) {
	dir := t.TempDir()
	rawRel := "raw/x.md"
	writeFixture(t, dir, rawRel, "```go\npackage main\n```\n")
	writeFixture(t, dir, "wiki/sources/x.md", fullSourcePage("x"))
	now := time.Now()
	os.Chtimes(filepath.Join(dir, rawRel), now.Add(-2*time.Hour), now.Add(-2*time.Hour))
	srcP := filepath.Join(dir, "wiki", "sources", "x.md")
	os.Chtimes(srcP, now.Add(-1*time.Hour), now.Add(-1*time.Hour))
	fi := statSrc(t, srcP)
	if idempotentRecompile(dir, rawRel, fi, srcP, 5) {
		t.Fatal("源页硬资产缺失(preflight 阻断)不应判幂等")
	}
}
