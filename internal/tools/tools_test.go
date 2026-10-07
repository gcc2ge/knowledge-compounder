package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gcc2ge/knowledge-compounder/internal/retrieval"
)

// TestReadPaged 分页语义:cat -n 行号、续读提示、越界、大文件强制分页、白名单外拒绝。
func TestReadPaged(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "raw"), 0o755); err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	for i := 1; i <= 10; i++ {
		sb.WriteString(strings.Repeat("行", i) + "\n")
	}
	big := strings.Repeat("大文件内容行\n", 9000) // ~130KB,超全读上限
	if err := os.WriteFile(filepath.Join(root, "raw/small.md"), []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "raw/big.md"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}

	// 小文件无参数:原文返回
	if got := readPaged(root, "raw/small.md", 0, 0, nil); got != sb.String() {
		t.Errorf("小文件应原样返回")
	}
	// 分页:offset=2 limit=3 → 第 2-4 行,带行号与续读提示
	got := readPaged(root, "raw/small.md", 2, 3, nil)
	if !strings.Contains(got, "     2\t") || !strings.Contains(got, "     4\t") || strings.Contains(got, "     5\t") {
		t.Errorf("分页应只含第 2-4 行,得:\n%s", got)
	}
	if !strings.Contains(got, "offset=5") {
		t.Errorf("未到末尾应给续读提示,得:\n%s", got)
	}
	// 读到末尾:提示已到文件末尾
	got = readPaged(root, "raw/small.md", 8, 10, nil)
	if !strings.Contains(got, "已到文件末尾") {
		t.Errorf("末页应提示结束,得:\n%s", got)
	}
	// 越界
	if got := readPaged(root, "raw/small.md", 99, 1, nil); !strings.Contains(got, "超出总行数") {
		t.Errorf("越界应报错,得 %s", got)
	}
	// 大文件无参数:返回头部 + 分页指示
	got = readPaged(root, "raw/big.md", 0, 0, nil)
	if !strings.Contains(got, "分页读取") || !strings.Contains(got, "offset=1") {
		t.Errorf("大文件应强制分页指示,得末尾:\n%s", got[len(got)-200:])
	}
	// 大文件分页:行号正确
	got = readPaged(root, "raw/big.md", 2000, 2, nil)
	if !strings.Contains(got, "  2000\t") {
		t.Errorf("分页行号应正确,得:\n%s", got[:80])
	}
	// 白名单外拒绝
	if got := readPaged(root, "go.mod", 0, 0, nil); !strings.Contains(got, "拒绝") {
		t.Errorf("白名单外应拒绝,得 %s", got)
	}
}

// TestHTMLToText 粗粒度转文本:script 剔除、标签剥离、实体解码。
func TestHTMLToText(t *testing.T) {
	h := `<html><head><script>alert("x")</script><style>.a{}</style></head>
<body><h1>标题</h1><p>第一段 &amp; 细节</p><!-- 注释 --></body></html>`
	got := htmlToText(h)
	if strings.Contains(got, "alert") || strings.Contains(got, ".a{}") || strings.Contains(got, "<") {
		t.Errorf("应剔除脚本/样式/标签,得:%s", got)
	}
	if !strings.Contains(got, "标题") || !strings.Contains(got, "第一段 & 细节") {
		t.Errorf("正文与实体解码应保留,得:%s", got)
	}
}

// TestEditFile edit_file 增量补写语义:唯一锚点替换、未找到/不唯一不改、截断拒绝、白名单外拒绝。
func TestEditFile(t *testing.T) {
	root := t.TempDir()
	tools, _, _, _ := Build(root, nil, nil, retrieval.Options{})
	var wf, ef struct {
		name string
		fn   func(map[string]any) string
	}
	for _, td := range tools {
		if td.Name == "write_file" {
			wf.fn = td.Func
		}
		if td.Name == "edit_file" {
			ef.fn = td.Func
		}
	}
	if ef.fn == nil {
		t.Fatal("缺少 edit_file 工具")
	}
	path := "wiki/sources/X.md"
	// 建页(首块)
	if got := wf.fn(map[string]any{"path": path, "content": "行A\n行B\n"}); !strings.Contains(got, "已写入") {
		t.Fatalf("write_file 建页失败: %s", got)
	}
	// 1. 唯一锚点追加:old=尾锚点,new=锚点+新块
	got := ef.fn(map[string]any{"path": path, "old_string": "行B", "new_string": "行B\n行C"})
	if !strings.Contains(got, "已编辑") {
		t.Fatalf("追加应成功,得 %s", got)
	}
	b, _ := os.ReadFile(filepath.Join(root, path))
	if string(b) != "行A\n行B\n行C\n" {
		t.Fatalf("追加后内容错,得 %q", string(b))
	}
	// 2. 锚点找不到 → 不改文件
	got = ef.fn(map[string]any{"path": path, "old_string": "不存在的锚点", "new_string": "X"})
	if !strings.Contains(got, "未找到") {
		t.Fatalf("未找到锚点应报错,得 %s", got)
	}
	b, _ = os.ReadFile(filepath.Join(root, path))
	if string(b) != "行A\n行B\n行C\n" {
		t.Fatalf("锚点未找到时不应改文件,得 %q", string(b))
	}
	// 3. 锚点不唯一 → 不改文件
	got = ef.fn(map[string]any{"path": path, "old_string": "行", "new_string": "X"})
	if !strings.Contains(got, "不唯一") {
		t.Fatalf("多出现锚点应报错,得 %s", got)
	}
	b, _ = os.ReadFile(filepath.Join(root, path))
	if string(b) != "行A\n行B\n行C\n" {
		t.Fatalf("锚点不唯一时不应改文件,得 %q", string(b))
	}
	// 4. 截断标记 → 拒绝,不改文件
	got = ef.fn(map[string]any{"path": path, "old_string": "行C", "new_string": "行C\n行D", "truncated": true})
	if !strings.Contains(got, "拒绝") {
		t.Fatalf("截断应拒绝,得 %s", got)
	}
	b, _ = os.ReadFile(filepath.Join(root, path))
	if string(b) != "行A\n行B\n行C\n" {
		t.Fatalf("截断拒绝时不应改文件,得 %q", string(b))
	}
	// 5. 白名单外拒绝(raw/ 不可变)
	got = ef.fn(map[string]any{"path": "raw/X.md", "old_string": "a", "new_string": "b"})
	if !strings.Contains(got, "拒绝") {
		t.Fatalf("raw/ 应拒绝编辑,得 %s", got)
	}
	// 6. 文件不存在 → 提示先 write_file
	got = ef.fn(map[string]any{"path": "wiki/sources/不存在.md", "old_string": "a", "new_string": "b"})
	if !strings.Contains(got, "不存在") {
		t.Fatalf("文件不存在应提示先建页,得 %s", got)
	}
	// 7. 只传 new_string(弱模型漏字段)→ 形状感知拒绝,给字段顺序
	got = ef.fn(map[string]any{"new_string": "只有新内容"})
	if !strings.Contains(got, "只传了 new_string") || !strings.Contains(got, "path → old_string → new_string") {
		t.Fatalf("漏字段应给顺序指引,得 %s", got)
	}
	// 8. 全缺 → 通用必填提示
	got = ef.fn(map[string]any{})
	if !strings.Contains(got, "必填") {
		t.Fatalf("全缺应提示必填,得 %s", got)
	}
}
