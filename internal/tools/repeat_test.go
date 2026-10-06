package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 构造模板化文件:两个主题各含 8 行模板正文 + 4 行差异参数。
func templateFile(t *testing.T) (string, []string) {
	t.Helper()
	dir := t.TempDir()
	var lines []string
	t1 := []string{"核心机制段落正文模板行1", "核心机制段落正文模板行2", "核心机制段落正文模板行3", "核心机制段落正文模板行4", "核心机制段落正文模板行5", "核心机制段落正文模板行6", "核心机制段落正文模板行7", "核心机制段落正文模板行8"}
	t2 := []string{"实战结论段落正文模板行1", "实战结论段落正文模板行2", "实战结论段落正文模板行3", "实战结论段落正文模板行4", "实战结论段落正文模板行5", "实战结论段落正文模板行6", "实战结论段落正文模板行7", "实战结论段落正文模板行8"}
	lines = append(lines, t1...)
	lines = append(lines, "参数一 GOMAXPROCS=2 队列深度=16")
	lines = append(lines, "参数二 锁重试=101 超时=50ms")
	lines = append(lines, t2...)
	lines = append(lines, t1...) // 第 2 主题:模板逐字重复
	lines = append(lines, "参数三 GOMAXPROCS=4 队列深度=32")
	lines = append(lines, "参数四 锁重试=201 超时=100ms")
	lines = append(lines, t2...)
	p := filepath.Join(dir, "raw", "book.md")
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte(strings.Join(lines, "\n")), 0o644)
	return dir, lines
}

// 首次读:模板段原样返回(不省略),并完成台账登记。
func TestRepeatFirstOccurrenceKept(t *testing.T) {
	dir, lines := templateFile(t)
	rep := &RepeatDetector{}
	got := readPaged(dir, "raw/book.md", 1, 20, rep)
	for i := 0; i < 20; i++ {
		if !strings.Contains(got, lines[i]) {
			t.Fatalf("首次读应原样含第 %d 行内容,得:\n%s", i+1, got)
		}
	}
	if strings.Contains(got, "逐字重复") {
		t.Fatalf("首次读不应出现重复标记,得:\n%s", got)
	}
}

// 第二次读:模拟序贯阅读——先读第 1 页(第 1-18 行,含两段模板首次出现),
// 再读第 2 页(第 19-36 行,两段模板逐字重复)→ 模板段被紧凑标记替换,差异参数行保留。
func TestRepeatSecondReadCompacts(t *testing.T) {
	dir, _ := templateFile(t)
	rep := &RepeatDetector{}
	readPaged(dir, "raw/book.md", 1, 18, rep) // 登记第 1-18 行(首次出现)
	got := readPaged(dir, "raw/book.md", 19, 18, rep)
	for _, frag := range []string{"参数三", "参数四"} {
		if !strings.Contains(got, frag) {
			t.Fatalf("差异参数行应保留,得:\n%s", got)
		}
	}
	if !strings.Contains(got, "[第 19–26 行与已读内容逐字重复(模板段,首次见于第 1 行),省略]") {
		t.Fatalf("第二段核心机制模板应被标记替换,得:\n%s", got)
	}
	if !strings.Contains(got, "[第 29–36 行与已读内容逐字重复(模板段,首次见于第 11 行),省略]") {
		t.Fatalf("实战结论模板应被标记替换,得:\n%s", got)
	}
}

// 短重复(不足 minRepeatRun 行)不省略——防误杀惯用代码块/短语。
func TestRepeatShortRunNotSuppressed(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "raw", "s.md")
	os.MkdirAll(filepath.Dir(p), 0o755)
	// 前 4 行与第 9-12 行相同(仅 4 行短重复),其余不同
	lines := []string{"行1", "行2", "行3", "行4", "行5", "行6", "行7", "行8", "行1", "行2", "行3", "行4", "行尾"}
	os.WriteFile(p, []byte(strings.Join(lines, "\n")), 0o644)
	rep := &RepeatDetector{}
	readPaged(dir, "raw/s.md", 1, 8, rep)
	got := readPaged(dir, "raw/s.md", 9, 5, rep)
	for i := 8; i < 13; i++ {
		if !strings.Contains(got, lines[i]) {
			t.Fatalf("短重复应原样返回第 %d 行,得:\n%s", i+1, got)
		}
	}
	if strings.Contains(got, "逐字重复") {
		t.Fatalf("短重复不应触发省略,得:\n%s", got)
	}
}

// 全页重复:整页都是模板段时只留标记,续读提示仍指向正确的下一行。
func TestRepeatWholePageCompacted(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "raw", "w.md")
	os.MkdirAll(filepath.Dir(p), 0o755)
	blk := []string{"模板甲", "模板乙", "模板丙", "模板丁", "模板戊", "模板己", "模板庚", "模板辛", "模板壬", "模板癸"}
	var lines []string
	lines = append(lines, blk...)
	lines = append(lines, blk...)
	lines = append(lines, "收尾行1", "收尾行2") // 重复段之后仍有新内容,续读提示才有意义
	os.WriteFile(p, []byte(strings.Join(lines, "\n")), 0o644)
	rep := &RepeatDetector{}
	readPaged(dir, "raw/w.md", 1, 10, rep)
	got := readPaged(dir, "raw/w.md", 11, 10, rep)
	if !strings.Contains(got, "逐字重复") {
		t.Fatalf("全页重复页应只剩标记,得:\n%s", got)
	}
	if !strings.Contains(got, "继续读:offset=21") {
		t.Fatalf("续读提示应指向重复段之后,得:\n%s", got)
	}
}

// 空台账(nil)不检测:行为与旧版一致。
func TestRepeatNilDetector(t *testing.T) {
	dir, lines := templateFile(t)
	got := readPaged(dir, "raw/book.md", 21, 20, nil)
	for i := 20; i < len(lines); i++ {
		if !strings.Contains(got, lines[i]) {
			t.Fatalf("nil 检测器应原样返回第 %d 行,得:\n%s", i+1, got)
		}
	}
}
