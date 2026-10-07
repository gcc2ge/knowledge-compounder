package wiki

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 打分与过滤:title 精确(10) > slug(8) > tag(5) > summary(3);
// 自身、页内已链(含 [[slug|显示名]])被过滤。
func TestSuggestLinks_ScoresAndFilters(t *testing.T) {
	reg := []PageInfo{
		{Slug: "上下文工程", Title: "上下文工程", Type: "concept"},
		{Slug: "工具调用", Title: "工具调用与函数", Type: "concept"},
		{Slug: "无关页", Title: "无关页面", Type: "entity"},
		{Slug: "本页", Title: "本页", Type: "source"},
		{Slug: "已链页", Title: "已链页", Type: "concept"},
		{Slug: "显示名页", Title: "显示名页", Type: "concept"},
	}
	text := `已有链 [[上下文工程]] 与 [[显示名页|别名]]。工具调用是核心,本页是自身。`
	cands := SuggestLinks(text, "本页", reg, 5)
	// 期望:工具调用(slug 出现在文本,score 8)。上下文工程/显示名页 已链 → 过滤;无关页 不出现 → 无;本页 自身 → 过滤。
	if len(cands) != 1 {
		t.Fatalf("应只剩 1 个候选,得 %v", cands)
	}
	if cands[0].Slug != "工具调用" || cands[0].Score != 8 {
		t.Fatalf("候选应为 工具调用(score=8),得 %v", cands[0])
	}
}

// title 完整出现在文本 → score 10,压过 slug 匹配。
func TestSuggestLinks_TitleOutranksSlug(t *testing.T) {
	reg := []PageInfo{
		{Slug: "上下文工程", Title: "上下文工程", Type: "concept"},
		{Slug: "工具调用", Title: "工具调用与函数", Type: "concept"},
	}
	text := "上下文工程与工具调用都是候选。"
	cands := SuggestLinks(text, "本页", reg, 1)
	if len(cands) != 1 || cands[0].Slug != "上下文工程" {
		t.Fatalf("title 精确命中应排第一,得 %v", cands)
	}
}

// 任意长度实体名都能完整命中(5 字「上下文工程」mid-run 也能捕获)。
func TestSuggestLinks_LongEntityExact(t *testing.T) {
	reg := []PageInfo{{Slug: "上下文工程", Title: "上下文工程", Type: "concept"}}
	text := "本页深入讨论了上下文工程这个主题。"
	cands := SuggestLinks(text, "本页", reg, 3)
	if len(cands) != 1 || cands[0].Score != 10 {
		t.Fatalf("长实体名应完整命中 score=10,得 %v", cands)
	}
}

// maxN 截断。
func TestSuggestLinks_MaxN(t *testing.T) {
	reg := []PageInfo{
		{Slug: "上下文工程", Title: "上下文工程", Type: "concept"},
		{Slug: "工具调用", Title: "工具调用与函数", Type: "concept"},
	}
	text := "上下文工程与工具调用都是候选。"
	if cands := SuggestLinks(text, "本页", reg, 1); len(cands) != 1 {
		t.Fatalf("maxN=1 应截断,得 %v", cands)
	}
}

// BuildRegistry:读全 wiki 子目录,填 slug/title/type/tags/summary。
func TestBuildRegistry(t *testing.T) {
	root := t.TempDir()
	dirs := []string{"sources", "concepts", "entities"}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(root, "wiki", d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(root, "wiki", "concepts", "背压.md"),
		[]byte("---\ntype: concept\ntags: [系统设计]\n---\n# 背压\n队列满时反压消费端。"), 0o644)
	os.WriteFile(filepath.Join(root, "wiki", "entities", "packyapi.md"),
		[]byte("---\ntype: entity\n---\n# packyapi\n一个模型网关。"), 0o644)

	reg := BuildRegistry(root)
	if len(reg) != 2 {
		t.Fatalf("应建 2 条注册表,得 %d", len(reg))
	}
	var found bool
	for _, r := range reg {
		if r.Slug == "背压" {
			found = true
			if r.Type != "concept" || len(r.Tags) != 1 || r.Tags[0] != "系统设计" {
				t.Fatalf("背压 元数据错: %+v", r)
			}
			if !strings.Contains(r.Summary, "反压") {
				t.Fatalf("summary 应取正文首句,得 %q", r.Summary)
			}
		}
	}
	if !found {
		t.Fatal("注册表缺 背压")
	}
}

// SetRelinked / RelinkedDates 往返:只动 frontmatter,正文原样保留。
func TestSetRelinkedRoundtrip(t *testing.T) {
	root := t.TempDir()
	fp := filepath.Join(root, "wiki", "sources", "M01.md")
	os.MkdirAll(filepath.Dir(fp), 0o755)
	body := "---\ncompiled: 2026-10-01\n---\n# M01\n论证链内容不可丢。"
	os.WriteFile(fp, []byte(body), 0o644)

	if err := SetRelinked(fp, "2026-10-07"); err != nil {
		t.Fatal(err)
	}
	rel, comp := RelinkedDates(fp)
	if rel != "2026-10-07" || comp != "2026-10-01" {
		t.Fatalf("relinked/compiled 往返错: rel=%q comp=%q", rel, comp)
	}
	text := Read(fp)
	if !strings.Contains(text, "论证链内容不可丢") {
		t.Fatalf("正文被 SetRelinked 破坏: %q", text)
	}
}

// 无时间戳的页:relinked/compiled 都返回空串(调用方据此判定必进)。
func TestRelinkedDatesAbsent(t *testing.T) {
	root := t.TempDir()
	fp := filepath.Join(root, "wiki", "sources", "X.md")
	os.MkdirAll(filepath.Dir(fp), 0o755)
	os.WriteFile(fp, []byte("---\ntype: source\n---\n# X\n无时间戳。"), 0o644)
	rel, comp := RelinkedDates(fp)
	if rel != "" || comp != "" {
		t.Fatalf("无时间戳应返回空串,得 rel=%q comp=%q", rel, comp)
	}
}
