package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 提取关键声明:一句话结论 + 论证链前段;关键细节等不参与核对。
func TestExtractClaims(t *testing.T) {
	text := `---
source_files: [raw/a.md]
---
# 示例源

## 一句话结论
幂等性设计保证   消息  不重复处理。  多余空白折叠。

## 论证链
第一步,唯一索引兜底。
第二步,本地消息表。

## 关键细节
这里不该进声明。

## 连接
- → [[其他]]
`
	claims := ExtractClaims(text)
	if len(claims) != 2 {
		t.Fatalf("应提取 2 条声明(结论+论证链),得 %d: %v", len(claims), claims)
	}
	c0, c1 := claims[0], claims[1]
	// collapseWS 把连续空白折成单空格(保持词间空格的语义)
	if !strings.Contains(c0, "幂等性设计保证 消息 不重复处理") {
		t.Fatalf("结论声明应为折叠后的整句,得 %q", c0)
	}
	if !strings.Contains(c1, "唯一索引兜底") || !strings.Contains(c1, "本地消息表") {
		t.Fatalf("论证链声明应含两步内容,得 %q", c1)
	}
	if strings.Contains(c0+c1, "不该进声明") || strings.Contains(c0+c1, "关键细节") {
		t.Fatal("关键细节内容不应进入比对声明")
	}
}

// 空输入 → 不扫,不 panic(降级)。
func TestScanContradictionsEmpty(t *testing.T) {
	if hits := ScanContradictions("", "x", "## 一句话结论\nfoo", 4); hits != nil {
		t.Fatalf("空 root 应返回 nil,得 %v", hits)
	}
	if hits := ScanContradictions("/tmp", "x", "", 4); hits != nil {
		t.Fatalf("空文本应返回 nil,得 %v", hits)
	}
}

// 临时 wiki:新建源页声明应与既有概念页/旧源页比对命中,且排除本源自身(重编译场景)。
func TestScanContradictionsFindsRelated(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("wiki/concepts/幂等性.md", `---
type: concept
---
# 幂等性

## 定义
幂等性保证重复请求不产生重复副作用,是消息系统与支付的核心保障。
`)
	write("wiki/sources/旧源.md", `# 旧源

## 一句话结论
数据库唯一索引是幂等实现的关键手段。
`)
	// 新源已存在(wiki/sources/新源.md),ScanContradictions 必须排除它自身
	write("wiki/sources/新源.md", `# 新源

## 一句话结论
幂等性靠唯一索引与本地消息表实现。

## 论证链
第一,幂等设计避免重复扣款。
`)
	b, err := os.ReadFile(filepath.Join(root, "wiki", "sources", "新源.md"))
	if err != nil {
		t.Fatal(err)
	}
	hits := ScanContradictions(root, "新源", string(b), 4)
	if len(hits) == 0 {
		t.Fatal("应找到与本源相关的既有页面(中文 bigram 重叠)")
	}
	labels := make([]string, len(hits))
	for i, h := range hits {
		labels[i] = h.Result.Label
		if h.Result.Label == "新源" {
			t.Fatalf("不应返回本源自身,得 %v", labels)
		}
		if h.Claim == "" {
			t.Fatalf("命中应带触发声明,得 %v", h)
		}
		if h.Result.Score <= 0 {
			t.Fatalf("命中应带正相关分,得 %v", h.Result)
		}
	}
	found := false
	for _, l := range labels {
		if l == "幂等性" || l == "旧源" {
			found = true
		}
	}
	if !found {
		t.Fatalf("应命中「幂等性」或「旧源」,得 %v", labels)
	}
}

// 与 wiki 完全无关的声明 → 无相关页(合法「无涉」结果)。
func TestScanContradictionsNoRelated(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "wiki", "concepts", "幂等性.md")
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte("幂等性保证重复请求不产生重复副作用。"), 0o644)
	text := `## 一句话结论
量子计算与蛋白质折叠预测的交叉研究。
`
	if hits := ScanContradictions(root, "新源", text, 4); hits != nil {
		t.Fatalf("无关声明应返回空,得 %v", hits)
	}
}

// 台账:Mark/Scanned/Pages 基本语义(FinishGuard 放行依据)。
func TestContradictionLogBasic(t *testing.T) {
	l := &ContradictionLog{}
	if l.Scanned("a") {
		t.Fatal("未标记不应视为已核对")
	}
	l.Mark("a", []string{"幂等性", "旧源"})
	if !l.Scanned("a") {
		t.Fatal("标记后应视为已核对")
	}
	if got := l.Pages("a"); len(got) != 2 || got[0] != "幂等性" {
		t.Fatalf("Pages 应返回扫描结果,得 %v", got)
	}
	// 无相关页也是合法核对结果
	l.Mark("b", nil)
	if !l.Scanned("b") {
		t.Fatal("扫到空结果仍视为核对过(无涉是合法结论)")
	}
}
