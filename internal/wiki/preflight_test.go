package wiki

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 带全部 9 个 SCHEMA 节的 source 骨架,避免 preflight 的「缺节」检查误报。
const srcSkeleton = `# t

## 一句话结论
x

## 论证链
%s

## 关键细节
x

## 作者立场与定位
x

## 意外发现
x

## 疑点
x

## 术语
x

## 连接
x

## 引用
x
`

// raw 里 3 个逐字重复代码块 + 1 个唯一块;source 保留一个代表 + 唯一块 → 语义无损,不列为缺失。
func TestPreflightRepetitionCoverage(t *testing.T) {
	dir := t.TempDir()
	raw := filepath.Join(dir, "raw.md")
	src := filepath.Join(dir, "src.md")
	dup := "```go\nfunc run() {\n\tselect {}\n}\n```\n"
	uniq := "```go\nfunc unique() {\n\treturn 1\n}\n```\n"
	if err := os.WriteFile(raw, []byte(dup+dup+dup+uniq), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte(fmt.Sprintf(srcSkeleton, dup+uniq)), 0o644); err != nil {
		t.Fatal(err)
	}
	report, issues, err := Preflight(raw, src)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 0 {
		t.Fatalf("重复块以代表保留应视为无损,不得列缺失: %#v", issues)
	}
	if !strings.Contains(report, "覆盖核验") || !strings.Contains(report, "语义无损") {
		t.Fatalf("报告应亮出覆盖核验提示: %s", report)
	}
}

// source 连唯一块也丢了 → 硬失败。
func TestPreflightUniqueBlockMissing(t *testing.T) {
	dir := t.TempDir()
	raw := filepath.Join(dir, "raw.md")
	src := filepath.Join(dir, "src.md")
	dup := "```go\nfunc run() {}\n```\n"
	uniq := "```go\nfunc unique() {}\n```\n"
	if err := os.WriteFile(raw, []byte(dup+dup+uniq), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte(fmt.Sprintf(srcSkeleton, dup)), 0o644); err != nil {
		t.Fatal(err)
	}
	_, issues, err := Preflight(raw, src)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(issues, "\n"), "唯一代码块") {
		t.Fatalf("唯一块缺失必须报硬失败: %#v", issues)
	}
}

// source 覆盖所有块且块数足够 → 原逻辑不报缺失。
func TestPreflightEnoughBlocks(t *testing.T) {
	dir := t.TempDir()
	raw := filepath.Join(dir, "raw.md")
	src := filepath.Join(dir, "src.md")
	a := "```go\nfunc a() {}\n```\n"
	b := "```go\nfunc b() {}\n```\n"
	if err := os.WriteFile(raw, []byte(a+b), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte(fmt.Sprintf(srcSkeleton, a+b)), 0o644); err != nil {
		t.Fatal(err)
	}
	_, issues, err := Preflight(raw, src)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 0 {
		t.Fatalf("块数足够不应报缺失: %#v", issues)
	}
}

// 规范化:同一块只有空白差异(行首 tab/空格、空行)也视为同一块。
func TestDistinctBlocksNormalize(t *testing.T) {
	raw := "```go\nfunc a() {\n\treturn 1\n}\n```\n\n```go\nfunc a() {\n    return 1\n\n}\n```\n"
	set := distinctBlocks(raw)
	if len(set) != 1 {
		t.Fatalf("仅空白差异应归并为同一块,得 %d 个: %v", len(set), set)
	}
}

// 缩进围栏(嵌套在列表/引用里)也是合法代码块,必须被识别——否则覆盖判定把无损误报成丢失。
// 场景:raw 3 个重复块 + 1 唯一块;source 用缩进围栏保留代表 + 唯一块 → 应判语义无损。
func TestIndentedFenceDetected(t *testing.T) {
	dup := "```go\nfunc run() {\n\tselect {}\n}\n```\n"
	uniq := "```go\nfunc unique() {\n\treturn 1\n}\n```\n"
	raw := dup + dup + dup + uniq
	// deepseek 产物实测形态:代码块写在列表项里,围栏带 3 空格缩进
	src := "列表项:\n   ```go\n   func run() {\n   \tselect {}\n   }\n   ```\n" + uniq
	dir := t.TempDir()
	rawF := filepath.Join(dir, "raw.md")
	srcF := filepath.Join(dir, "src.md")
	if err := os.WriteFile(rawF, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(srcF, []byte(fmt.Sprintf(srcSkeleton, src)), 0o644); err != nil {
		t.Fatal(err)
	}
	report, issues, err := Preflight(rawF, srcF)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 0 {
		t.Fatalf("缩进围栏代表 + 唯一块应判无损: %#v", issues)
	}
	if !strings.Contains(report, "语义无损") {
		t.Fatalf("应亮出覆盖核验: %s", report)
	}
}
