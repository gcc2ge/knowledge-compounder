package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

// markRead 分页截断回归:请求 [1,limit] 但字节闸在中间截断时,只记实际返回行,
// 截断后的未读区间必须仍被 Gap 检出(旧实现按请求上限记录,漏记中间 gap)。
func TestMarkReadTruncatedKeepsGap(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "raw") // readPaged 白名单允许 raw/
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// 大文件:每行 ~1KB 内容,200 行 = ~200KB > pageBytesCap(120KB),必触字节截断。
	var big strings.Builder
	for i := 1; i <= 500; i++ {
		big.WriteString(strings.Repeat("内容数据块_", 200)) // 每行 ~1KB
		big.WriteString("\n")
	}
	fp := filepath.Join(dir, "big.md")
	if err := os.WriteFile(fp, []byte(big.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	c := &Coverage{}
	// 模拟 read_file:分页读前 200 行,但字节闸只渲染到某行(截断行 < 200)。
	// 注意 root 是 tempdir(项目根),path 是相对根的白名单路径 raw/big.md。
	_, lo, hi, total := readPagedDetail(root, "raw/big.md", 1, 200, nil)
	if lo <= 0 || hi <= 0 || total <= 0 {
		t.Fatalf("readPagedDetail 应返回有效区间,得 lo=%d hi=%d total=%d", lo, hi, total)
	}
	if hi >= 200 {
		t.Fatalf("期望字节截断 hi<200(内容每行超 120KB/200 行的字节上限),得 hi=%d", hi)
	}
	markRead(c, "big.md", lo, hi, total)

	// 请求 200 行但只实际读到 hi=38 → 后面 [39, total] 未读,必须检出 gap。
	// 注意 Gap 的上限是文件总行数 total(=501),不是请求上限 200。
	if lo2, hi2, ok := c.Gap("big.md"); !ok {
		t.Fatal("分页截断后未读部分应仍检出 gap")
	} else if lo2 != hi+1 || hi2 != total {
		t.Fatalf("gap 应为 %d-%d,得 %d-%d(hi=%d total=%d)", hi+1, total, lo2, hi2, hi, total)
	}
}
