package index

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gcc2ge/knowledge-compounder/internal/retrieval"
	"github.com/gcc2ge/knowledge-compounder/internal/wiki"
)

// tmpWiki 建临时库:两个 source 页(共享术语「goroutine 泄漏」)+ 一个概念页。
func tmpWiki(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, d := range []string{"wiki/sources", "wiki/concepts", "wiki/entities", "wiki/synthesis", "wiki/notes", "raw"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(rel, body string) {
		if err := os.WriteFile(filepath.Join(root, rel), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("raw/a.md", "# A")
	write("wiki/sources/a.md", "---\nsource_files:\n  - raw/a.md\norigin: external\ntype: source\n---\n# A\n\n## 一句话结论\ngoroutine 泄漏三形态。\n\n链接 [[b]] 与 [[goroutine泄漏]]。")
	write("wiki/sources/b.md", "---\nsource_files:\n  - raw/b.md\norigin: self\ntype: source\n---\n# B\n\n## 一句话结论\ncontext 取消断链导致 goroutine 泄漏。\n\n见 [[goroutine泄漏]]。")
	write("wiki/concepts/goroutine泄漏.md", "---\ntype: concept\n---\n# goroutine 泄漏\n\n## 定义\n协程永不退出。\n\n相关 [[a]] [[b]]。")
	return root
}

func TestMentions(t *testing.T) {
	idx := Build(tmpWiki(t))
	rs := idx.Mentions([]string{"goroutine 泄漏", "context", "不存在词"})
	if len(rs) != 3 {
		t.Fatalf("应有 3 个结果,得 %d", len(rs))
	}
	if len(rs[0].Hits) != 2 { // a、b 两个 source 都提及
		t.Errorf("「goroutine 泄漏」应 2 源提及,得 %v", rs[0].Hits)
	}
	if rs[1].Hits == nil || rs[1].Hits[0] != "b" {
		t.Errorf("「context」应仅 b 提及,得 %v", rs[1].Hits)
	}
	if len(rs[2].Hits) != 0 {
		t.Errorf("「不存在词」应 0 命中,得 %v", rs[2].Hits)
	}
	// mentions 只数 source 页:概念页 goroutine泄漏.md 不进计数(它自己也含这个词)
	if len(idx.Mentions([]string{"永不退出"})[0].Hits) != 0 {
		t.Errorf("概念页内容不应计入 mentions")
	}
}

func TestSync增量(t *testing.T) {
	root := tmpWiki(t)
	idx := Build(root)
	fp := filepath.Join(root, "wiki/sources/b.md")
	// 未变:全 Unchanged
	rp := idx.Sync(root)
	if rp.Unchanged != 3 || rp.Added+rp.Updated+rp.Removed != 0 {
		t.Fatalf("无变化时全 Unchanged,得 %+v", rp)
	}
	// 改 b:仅 1 Updated
	if err := os.WriteFile(fp, []byte("---\norigin: external\ntype: source\n---\n# B2\n\n改过了 worker pool。"), 0o644); err != nil {
		t.Fatal(err)
	}
	rp = idx.Sync(root)
	if rp.Updated != 1 || rp.Unchanged != 2 {
		t.Fatalf("改一页应 Updated=1,得 %+v", rp)
	}
	if len(idx.Mentions([]string{"worker pool"})[0].Hits) != 1 {
		t.Errorf("更新后新词应可查")
	}
	// 删 b:Removed=1,倒排摘除
	if err := os.Remove(fp); err != nil {
		t.Fatal(err)
	}
	rp = idx.Sync(root)
	if rp.Removed != 1 {
		t.Fatalf("删一页应 Removed=1,得 %+v", rp)
	}
	if len(idx.Mentions([]string{"goroutine 泄漏"})[0].Hits) != 1 {
		t.Errorf("删除后 mentions 应剩 1")
	}
}

func Test快照往返与增载(t *testing.T) {
	root := tmpWiki(t)
	idx := Build(root)
	if err := idx.Save(root); err != nil {
		t.Fatal(err)
	}
	idx2 := Load(root) // 走快照路径
	if len(idx2.Pages) != 3 {
		t.Fatalf("快照加载应 3 页,得 %d", len(idx2.Pages))
	}
	if _, _, ok := idx2.GetPage("goroutine泄漏"); !ok {
		t.Errorf("GetPage 应命中")
	}
	// KCP_INDEX=off → nil
	t.Setenv("KCP_INDEX", "off")
	if Load(root) != nil {
		t.Errorf("KCP_INDEX=off 应返回 nil")
	}
}

func Test快照瘦身与水合(t *testing.T) {
	root := tmpWiki(t)
	if err := Build(root).Save(root); err != nil {
		t.Fatal(err)
	}
	idx := Load(root)
	// 快照载入后 Text 未水合
	for _, m := range idx.Pages {
		if m.hydrated {
			t.Fatalf("载入即水合违背瘦身设计:%s", m.Path)
		}
	}
	// Rank/Mentions/GetPage 用时按需水合,结果与全量索引一致
	if r := idx.Rank("goroutine 泄漏", defaultOpts()); len(r) == 0 {
		t.Fatal("水合后 Rank 应有结果")
	}
	if len(idx.Mentions([]string{"goroutine 泄漏"})[0].Hits) != 2 {
		t.Errorf("水合后 Mentions 应 2 源命中")
	}
	if txt, _, ok := idx.GetPage("a"); !ok || !strings.Contains(txt, "泄漏") {
		t.Errorf("水合后 GetPage 应返回正文")
	}
	// 未水合页删除不脏倒排:删 b 后「goroutine 泄漏」剩 1 源
	if err := os.Remove(filepath.Join(root, "wiki/sources/b.md")); err != nil {
		t.Fatal(err)
	}
	idx.Sync(root)
	if len(idx.Mentions([]string{"goroutine 泄漏"})[0].Hits) != 1 {
		t.Errorf("未水合页删除后倒排应正确摘除")
	}
}

func Test图查询(t *testing.T) {
	idx := Build(tmpWiki(t))
	// 入站:goroutine泄漏 被 a、b 链入
	in, out := idx.Backlinks("goroutine泄漏")
	if len(in) != 2 {
		t.Errorf("goroutine泄漏 入站应 2,得 %v", in)
	}
	if len(out) != 2 { // 概念页出链 a、b
		t.Errorf("出站应 2,得 %v", out)
	}
	// 断链:a 链 [[b]](存在)无断;故意加断链页
	if bl := idx.BrokenLinks(); len(bl) != 0 {
		t.Logf("基线断链: %v(无额外页面时可为空)", bl)
	}
	// 孤儿:概念页被链入非孤儿;无入站的页面为孤儿
	orph := idx.OrphanSlugs()
	found := false
	for _, s := range orph {
		if s == "goroutine泄漏" {
			found = true
		}
	}
	if found {
		t.Errorf("被链入的页面不应是孤儿")
	}
}

func TestRank走索引(t *testing.T) {
	idx := Build(tmpWiki(t))
	res := idx.Rank("goroutine 泄漏", defaultOpts())
	if len(res) == 0 {
		t.Fatal("应有检索结果")
	}
	if res[0].Label != "a" && res[0].Label != "b" {
		t.Logf("首条=%s(顺序按分数,a/b 均合理)", res[0].Label)
	}
	if res[0].Summary == "" {
		t.Errorf("应带摘要")
	}
	// 无命中查询
	if r := idx.Rank("完全不相关的查询词xyzq", defaultOpts()); len(r) != 0 {
		t.Errorf("无命中应返回空,得 %d 条", len(r))
	}
}

func TestUpdateFile写入即索引(t *testing.T) {
	root := tmpWiki(t)
	idx := Build(root)
	fp := filepath.Join(root, "wiki/notes/n1.md")
	if err := os.WriteFile(fp, []byte("---\ntype: note\n---\n# 新笔记\n\nworker pool 背压。"), 0o644); err != nil {
		t.Fatal(err)
	}
	idx.UpdateFile(fp)
	if _, _, ok := idx.GetPage("n1"); !ok {
		t.Fatal("UpdateFile 后应立即可查")
	}
	if r := idx.Rank("背压", defaultOpts()); len(r) == 0 {
		t.Errorf("新页应可被检索")
	}
	// 非 wiki/ 路径不入索引
	idx.UpdateFile(filepath.Join(root, "README.md"))
	if _, _, ok := idx.GetPage("README"); ok {
		t.Errorf("README 不应入索引")
	}
}

func defaultOpts() retrieval.Options {
	return retrieval.Options{Top: 5}
}

// BenchmarkRankVs全扫 需 BENCH_ROOT 指向真实库(如 716 页的 strategy_wiki 副本),否则跳过。
// 对照:索引路径 Rank(候选水合) vs 旧路径 wiki.Retrieve(全库读+全 tokenize)。
func BenchmarkRankVs全扫(b *testing.B) {
	root := os.Getenv("BENCH_ROOT")
	if root == "" {
		b.Skip("设 BENCH_ROOT=<wiki 根> 启用")
	}
	idx := Build(root)
	b.Run("index.Rank", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if r := idx.Rank("熔断器 限流 分布式锁", defaultOpts()); len(r) == 0 {
				b.Fatal("空结果")
			}
		}
	})
	b.Run("wiki.Retrieve全扫", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if r := wiki.Retrieve(root, "熔断器 限流 分布式锁", defaultOpts()); len(r) == 0 {
				b.Fatal("空结果")
			}
		}
	})
}
