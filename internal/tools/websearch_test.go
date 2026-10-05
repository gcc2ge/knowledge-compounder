package tools

import "testing"

// DDG 结果页 HTML 片段(真实结构:result__a 标题跳转链接 + result__snippet 摘要)。
const ddgHTML = `<html><body>
<div class="result results_links results_links_deep web-result">
  <h2 class="result__title">
    <a rel="nofollow" class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fgo">Go <b>Concurrency</b> Guide</a>
  </h2>
  <a class="result__snippet" href="//duckduckgo.com/l/?uddg=...">A practical guide to <b>goroutines</b> and channels.</a>
</div>
<div class="result results_links results_links_deep web-result">
  <a rel="nofollow" class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fgolang.org%2Fdoc">Golang Docs</a>
  <a class="result__snippet" href="//duckduckgo.com/l/?uddg=...">Official documentation for the Go language.</a>
</div>
</body></html>`

// 解析出标题/真实 URL/摘要;HTML 高亮 <b> 被剥离。
func TestParseDDGResults(t *testing.T) {
	rs := parseDDGResults(ddgHTML)
	if len(rs) != 2 {
		t.Fatalf("应解析出 2 条,得 %d: %#v", len(rs), rs)
	}
	if rs[0].title != "Go Concurrency Guide" {
		t.Fatalf("标题应剥掉 <b> 高亮,得 %q", rs[0].title)
	}
	if rs[0].url != "https://example.com/go" {
		t.Fatalf("uddg 跳转应解出真实 URL,得 %q", rs[0].url)
	}
	if rs[0].snippet == "" {
		t.Fatal("摘要不应为空")
	}
}

// 空 HTML → 空结果(不 panic)。
func TestParseDDGResultsEmpty(t *testing.T) {
	if rs := parseDDGResults("<html></html>"); len(rs) != 0 {
		t.Fatalf("空页应返回 0 条,得 %#v", rs)
	}
}

func TestDecodeDDGURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fa", "https://example.com/a"},
		{"//example.com/x", "https://example.com/x"},
		{"/doc", "https://duckduckgo.com/doc"},
		{"https://plain.example", "https://plain.example"},
	}
	for _, c := range cases {
		if got := decodeDDGURL(c.in); got != c.want {
			t.Errorf("decodeDDGURL(%q)=%q want %q", c.in, got, c.want)
		}
	}
}
