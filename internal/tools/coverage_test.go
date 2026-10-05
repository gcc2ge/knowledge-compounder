package tools

import "testing"

// 读头 + 尾、跳过中间 → 中间 gap 被确定性检出(glm 的「读头尾写浅页」失败模式)。
func TestCoverageDetectsMiddleGap(t *testing.T) {
	c := &Coverage{}
	c.Mark("raw/book.md", 1, 650, 2681)     // 全文读返回的头部
	c.Mark("raw/book.md", 2001, 2681, 2681) // 尾页
	lo, hi, ok := c.Gap("raw/book.md")
	if !ok {
		t.Fatal("中间有未读区间应检出 gap")
	}
	if lo != 651 || hi != 2000 {
		t.Fatalf("gap 应为 651-2000,得 %d-%d", lo, hi)
	}
}

// 分段读全 → 无 gap。
func TestCoverageFullReadNoGap(t *testing.T) {
	c := &Coverage{}
	c.Mark("raw/book.md", 1, 1000, 2681)
	c.Mark("raw/book.md", 1001, 2000, 2681)
	c.Mark("raw/book.md", 2001, 2681, 2681)
	if _, _, ok := c.Gap("raw/book.md"); ok {
		t.Fatalf("全部读满不应有 gap")
	}
}

// 重叠区间并入,不应误报。
func TestCoverageOverlapMerge(t *testing.T) {
	c := &Coverage{}
	c.Mark("raw/a.md", 1, 500, 1000)
	c.Mark("raw/a.md", 300, 700, 1000) // 与 [1,500] 重叠
	if _, _, ok := c.Gap("raw/a.md"); !ok {
		t.Fatal("700 之后还没读,应有 gap")
	}
	lo, hi, ok := c.Gap("raw/a.md")
	if !ok || lo != 701 || hi != 1000 {
		t.Fatalf("gap 应为 701-1000,得 %d-%d ok=%v", lo, hi, ok)
	}
	// 读满 → 无 gap
	c.Mark("raw/a.md", 701, 1000, 1000)
	if _, _, ok := c.Gap("raw/a.md"); ok {
		t.Fatal("读满后不应有 gap")
	}
}

// 从未读过的文件 → 不阻断(交给存在性/质量判据)。
func TestCoverageUnknownFileNoBlock(t *testing.T) {
	c := &Coverage{}
	if _, _, ok := c.Gap("raw/never.md"); ok {
		t.Fatal("未知文件不应阻断")
	}
}

// 越界参数被裁剪,不 panic 不产生负区间:lo 负、hi 超总行数 → 裁剪成全覆盖。
func TestCoverageClamp(t *testing.T) {
	c := &Coverage{}
	c.Mark("raw/a.md", -5, 99999, 2681)
	if _, _, ok := c.Gap("raw/a.md"); ok {
		t.Fatal("越界裁剪成 [1,2681] 全覆盖后不应有 gap")
	}
}
