package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	if got := readPaged(root, "raw/small.md", 0, 0); got != sb.String() {
		t.Errorf("小文件应原样返回")
	}
	// 分页:offset=2 limit=3 → 第 2-4 行,带行号与续读提示
	got := readPaged(root, "raw/small.md", 2, 3)
	if !strings.Contains(got, "     2\t") || !strings.Contains(got, "     4\t") || strings.Contains(got, "     5\t") {
		t.Errorf("分页应只含第 2-4 行,得:\n%s", got)
	}
	if !strings.Contains(got, "offset=5") {
		t.Errorf("未到末尾应给续读提示,得:\n%s", got)
	}
	// 读到末尾:提示已到文件末尾
	got = readPaged(root, "raw/small.md", 8, 10)
	if !strings.Contains(got, "已到文件末尾") {
		t.Errorf("末页应提示结束,得:\n%s", got)
	}
	// 越界
	if got := readPaged(root, "raw/small.md", 99, 1); !strings.Contains(got, "超出总行数") {
		t.Errorf("越界应报错,得 %s", got)
	}
	// 大文件无参数:返回头部 + 分页指示
	got = readPaged(root, "raw/big.md", 0, 0)
	if !strings.Contains(got, "分页读取") || !strings.Contains(got, "offset=1") {
		t.Errorf("大文件应强制分页指示,得末尾:\n%s", got[len(got)-200:])
	}
	// 大文件分页:行号正确
	got = readPaged(root, "raw/big.md", 2000, 2)
	if !strings.Contains(got, "  2000\t") {
		t.Errorf("分页行号应正确,得:\n%s", got[:80])
	}
	// 白名单外拒绝
	if got := readPaged(root, "go.mod", 0, 0); !strings.Contains(got, "拒绝") {
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
