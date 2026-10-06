package wiki

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// conceptFixture 写一个概念页并返回其绝对路径。
func conceptFixture(t *testing.T, root, slug, fm, body string) string {
	t.Helper()
	p := filepath.Join(root, "wiki", "concepts", slug+".md")
	writeFixtureFile(t, p, "---\n"+fm+"\n---\n\n"+body)
	return p
}

func writeFixtureFile(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// 佐证 → 追加源 + confidence 升级(low → medium)+ 外部观点加佐证行。
func TestApplyEvidence_CorroborateUpgrades(t *testing.T) {
	root := t.TempDir()
	conceptFixture(t, root, "背压", "type: concept\nsources: [源A]\nconfidence: low\ncreated: 2026-10-01\nupdated: 2026-10-01",
		"# 背压\n\n## 外部观点\n- 源A 定义背压为队列满时反压\n\n## 张力与缺口\n- 待验证点\n")
	msg, err := ApplyEvidence(root, "背压", "corroborate", "源B", "两套实现验证了同一反压语义")
	if err != nil {
		t.Fatal(err)
	}
	vals, body := ParseFrontmatter(readFile(t, filepath.Join(root, "wiki", "concepts", "背压.md")))
	if got := FrontmatterList(vals, "sources"); len(got) != 2 || got[1] != "源B" {
		t.Fatalf("sources 应追加 源B,得 %v", got)
	}
	if got := FrontmatterString(vals, "confidence"); got != "medium" {
		t.Fatalf("confidence 应升级为 medium,得 %s", got)
	}
	if !strings.Contains(body, "- 被 [[源B]] 佐证: 两套实现验证了同一反压语义") {
		t.Fatalf("外部观点节应含佐证行,得 %s", body)
	}
	if !strings.Contains(msg, "confidence=medium") {
		t.Fatalf("返回信息应含 confidence 读数,得 %s", msg)
	}
}

// 已有冲突标注的概念页,即使源足够多,confidence 封顶 medium(证据打架不该标 high)。
func TestApplyEvidence_ConflictCapsConfidence(t *testing.T) {
	root := t.TempDir()
	conceptFixture(t, root, "背压", "type: concept\nsources: [源A, 源B, 源C, 源D]\nconfidence: high\ncreated: 2026-10-01\nupdated: 2026-10-01",
		"# 背压\n\n## 外部观点\n- 源A 定义\n\n## 张力与缺口\n- ⚠️ 冲突: [[源B]] — 两种分类并存\n")
	if _, err := ApplyEvidence(root, "背压", "corroborate", "源E", "追加佐证"); err != nil {
		t.Fatal(err)
	}
	vals, _ := ParseFrontmatter(readFile(t, filepath.Join(root, "wiki", "concepts", "背压.md")))
	if got := FrontmatterString(vals, "confidence"); got != "medium" {
		t.Fatalf("含冲突标注的页 confidence 应封顶 medium,得 %s", got)
	}
}

// 冲突 → 往「张力与缺口」落 lint 可识别标注。
func TestApplyEvidence_ContradictAddsTension(t *testing.T) {
	root := t.TempDir()
	conceptFixture(t, root, "背压", "type: concept\nsources: [源A]\nconfidence: low\ncreated: 2026-10-01\nupdated: 2026-10-01",
		"# 背压\n\n## 外部观点\n- 源A 定义\n\n## 张力与缺口\n- 待验证点\n")
	if _, err := ApplyEvidence(root, "背压", "contradict", "源B", "两种 taxonomy 并存"); err != nil {
		t.Fatal(err)
	}
	vals, body := ParseFrontmatter(readFile(t, filepath.Join(root, "wiki", "concepts", "背压.md")))
	if got := FrontmatterString(vals, "confidence"); got != "low" {
		t.Fatalf("conflict 不改 confidence,得 %s", got)
	}
	if !strings.Contains(body, "- ⚠️ 冲突: [[源B]] — 两种 taxonomy 并存") {
		t.Fatalf("张力与缺口节应含 ⚠️ 冲突 标注,得 %s", body)
	}
}

// 只作用于概念页:source 页拒绝。
func TestApplyEvidence_RejectsNonConcept(t *testing.T) {
	root := t.TempDir()
	writeFixtureFile(t, filepath.Join(root, "wiki", "sources", "源A.md"), "---\ntype: source\norigin: external\n---\n# 源A\n")
	if _, err := ApplyEvidence(root, "源A", "corroborate", "源B", ""); err == nil || !strings.Contains(err.Error(), "概念页") {
		t.Fatalf("非概念页应拒绝,得 %v", err)
	}
}

// 缺「张力与缺口」节的页面落冲突应报错,不能静默丢标注。
func TestApplyEvidence_ContradictNeedsTensionSection(t *testing.T) {
	root := t.TempDir()
	conceptFixture(t, root, "背压", "type: concept\nsources: [源A]\nconfidence: low\ncreated: 2026-10-01\nupdated: 2026-10-01",
		"# 背压\n\n## 外部观点\n- 源A 定义\n")
	if _, err := ApplyEvidence(root, "背压", "contradict", "源B", "冲突原因"); err == nil || !strings.Contains(err.Error(), "张力与缺口") {
		t.Fatalf("缺张力节应报错,得 %v", err)
	}
}

// 佐证重复调用幂等:同一源不重复追加。
func TestApplyEvidence_CorroborateIdempotent(t *testing.T) {
	root := t.TempDir()
	conceptFixture(t, root, "背压", "type: concept\nsources: [源A]\nconfidence: low\ncreated: 2026-10-01\nupdated: 2026-10-01",
		"# 背压\n\n## 外部观点\n- 源A 定义\n\n## 张力与缺口\n- 待验证点\n")
	if _, err := ApplyEvidence(root, "背压", "corroborate", "源A", ""); err != nil {
		t.Fatal(err)
	}
	vals, _ := ParseFrontmatter(readFile(t, filepath.Join(root, "wiki", "concepts", "背压.md")))
	if got := FrontmatterList(vals, "sources"); len(got) != 1 {
		t.Fatalf("同一源重复佐证不应追加,得 %v", got)
	}
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
