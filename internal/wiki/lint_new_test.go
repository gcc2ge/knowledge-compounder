package wiki

import (
	"path/filepath"
	"strings"
	"testing"
)

// 意外发现节只有原文复述、无联想信号 → 标弱(私有 edge 缺失的提示)。
func TestSurpriseWeak_FiresOnRestatement(t *testing.T) {
	root := t.TempDir()
	writeFixtureFile(t, filepath.Join(root, "wiki", "sources", "源A.md"),
		"---\ntype: source\norigin: external\n---\n# 源A\n\n## 一句话结论\nok\n\n## 意外发现\n原文说背压阈值是 0.8,与我预期不符。\n\n## 疑点\n未发现重大疑点\n")
	res := Lint(root)
	if len(res.SurpriseWeak) != 1 {
		t.Fatalf("纯复述意外发现应标弱,得 %v", res.SurpriseWeak)
	}
}

// 意外发现含联想信号词(这意味着/场景)→ 不标弱。
func TestSurpriseWeak_SkipWhenInsight(t *testing.T) {
	root := t.TempDir()
	writeFixtureFile(t, filepath.Join(root, "wiki", "sources", "源A.md"),
		"---\ntype: source\norigin: external\n---\n# 源A\n\n## 一句话结论\nok\n\n## 意外发现\n原文说背压阈值是 0.8,这意味着在用户的 Kafka 场景里该阈值要按分区数重算。\n\n## 疑点\n未发现重大疑点\n")
	res := Lint(root)
	if len(res.SurpriseWeak) != 0 {
		t.Fatalf("含联想不应标弱,得 %v", res.SurpriseWeak)
	}
}

// 缺「意外发现」节 → 走既有 Format 检查(缺节),不算 SurpriseWeak(不双重报告)。
func TestSurpriseWeak_NotDoubleReportMissing(t *testing.T) {
	root := t.TempDir()
	writeFixtureFile(t, filepath.Join(root, "wiki", "sources", "源A.md"),
		"---\ntype: source\norigin: external\n---\n# 源A\n\n## 一句话结论\nok\n\n## 疑点\n未发现重大疑点\n")
	res := Lint(root)
	if len(res.SurpriseWeak) != 0 {
		t.Fatalf("缺节应走 Format 而非 SurpriseWeak,得 %v", res.SurpriseWeak)
	}
	if len(res.Format) != 1 || !strings.Contains(res.Format[0], "意外发现") {
		t.Fatalf("缺节应进 Format,得 %v", res.Format)
	}
}

// 两个高相似概念页 → 报疑似同义对。
func TestNearDuplicateConcepts_Fires(t *testing.T) {
	root := t.TempDir()
	body := "# 背压\n\n## 定义\n背压是数据流中生产者快于消费者的反压信号,队列填满时触发。\n\n## 外部观点\n多方验证了该机制在消息系统的必要性。\n"
	writeFixtureFile(t, filepath.Join(root, "wiki", "concepts", "背压.md"), "---\ntype: concept\n---\n"+body)
	writeFixtureFile(t, filepath.Join(root, "wiki", "concepts", "背压机制.md"), "---\ntype: concept\n---\n"+body)
	res := Lint(root)
	if len(res.NearDup) == 0 {
		t.Fatalf("高相似概念页应报疑似同义,得 %v", res.NearDup)
	}
}

// 两个完全不同概念页 → 不误报。
func TestNearDuplicateConcepts_NoFalsePositive(t *testing.T) {
	root := t.TempDir()
	writeFixtureFile(t, filepath.Join(root, "wiki", "concepts", "背压.md"),
		"---\ntype: concept\n---\n# 背压\n\n## 定义\n队列满时对生产者的反压信号。\n")
	writeFixtureFile(t, filepath.Join(root, "wiki", "concepts", "分布式事务.md"),
		"---\ntype: concept\n---\n# 分布式事务\n\n## 定义\n跨服务原子提交,两阶段提交与最终一致性。\n")
	res := Lint(root)
	if len(res.NearDup) != 0 {
		t.Fatalf("无关概念页不应误报,得 %v", res.NearDup)
	}
}
