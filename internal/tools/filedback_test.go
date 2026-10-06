package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// filedBack 底座 fixture:两个已存在的源页作支撑。
func filedBackFixture(t *testing.T) (root string) {
	t.Helper()
	root = t.TempDir()
	for _, s := range []string{"源A", "源B"} {
		p := filepath.Join(root, "wiki", "sources", s+".md")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("---\ntype: source\norigin: external\n---\n# "+s+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func fbArgs(sources, unresolved string) map[string]any {
	return map[string]any{
		"question": "Kafka 积压怎么治", "title": "Kafka积压治理综合",
		"summary": "跨页综合", "analysis": "分析正文", "evidence": "证据 [A]",
		"unresolved": unresolved, "conclusion": "结论", "sources": sources,
	}
}

// 单源答案不值得落盘 → 拒绝(跨页综合的机械判据)。
func TestFiledBack_RejectsSingleSource(t *testing.T) {
	root := filedBackFixture(t)
	if got := filedBack(root, fbArgs("源A", "未决"), nil); !strings.Contains(got, "支撑页") {
		t.Fatalf("单源应拒绝,得 %s", got)
	}
}

// 未决问题留空 → 拒绝(不替你脑补结论)。
func TestFiledBack_RejectsEmptyUnresolved(t *testing.T) {
	root := filedBackFixture(t)
	if got := filedBack(root, fbArgs("源A, 源B", ""), nil); !strings.Contains(got, "unresolved") {
		t.Fatalf("未决留空应拒绝,得 %s", got)
	}
}

// 支撑页不存在 → 拒绝。
func TestFiledBack_RejectsMissingSource(t *testing.T) {
	root := filedBackFixture(t)
	if got := filedBack(root, fbArgs("源A, 不存在页", "未决"), nil); !strings.Contains(got, "不存在") {
		t.Fatalf("支撑页不存在应拒绝,得 %s", got)
	}
}

// 标题含路径字符 → 拒绝(防路径穿越/斜杠文件名)。
func TestFiledBack_RejectsBadTitle(t *testing.T) {
	root := filedBackFixture(t)
	args := fbArgs("源A, 源B", "未决")
	args["title"] = "a/b"
	if got := filedBack(root, args, nil); !strings.Contains(got, "title") {
		t.Fatalf("非法标题应拒绝,得 %s", got)
	}
}

// 正常落盘:模板齐全 + 已存在拒绝覆盖。
func TestFiledBack_WritesSynthesis(t *testing.T) {
	root := filedBackFixture(t)
	got := filedBack(root, fbArgs("源A, 源B", "未决:积压根因尚未实测"), nil)
	if !strings.Contains(got, "wiki/synthesis/Kafka积压治理综合.md") {
		t.Fatalf("落盘应报成功路径,得 %s", got)
	}
	fp := filepath.Join(root, "wiki", "synthesis", "Kafka积压治理综合.md")
	b, err := os.ReadFile(fp)
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	for _, sec := range []string{"## 一句话摘要", "## 问题", "## 分析", "## 证据与张力", "## 未决问题", "## 结论", "## 来源"} {
		if !strings.Contains(text, sec) {
			t.Fatalf("synthesis 模板缺 %s,得:\n%s", sec, text)
		}
	}
	if !strings.Contains(text, "- [[源A]]") || !strings.Contains(text, "- [[源B]]") {
		t.Fatalf("来源应列两个支撑页,得:\n%s", text)
	}
	if !strings.Contains(text, "trigger: query") {
		t.Fatalf("trigger 应为 query,得:\n%s", text)
	}
	// 已存在 → 拒绝覆盖
	if got := filedBack(root, fbArgs("源A, 源B", "未决"), nil); !strings.Contains(got, "已存在") {
		t.Fatalf("同名已存在应拒绝覆盖,得 %s", got)
	}
}
