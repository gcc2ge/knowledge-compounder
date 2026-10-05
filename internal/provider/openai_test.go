package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newOpenAI(srv *httptest.Server) *OpenAICompatible {
	p := NewOpenAICompatible("test-model", "key", srv.URL)
	p.Client = srv.Client()
	return p
}

func chatHandler(t *testing.T, wantReasoning bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 断言请求体是标准 chat 格式(不传 tools 等)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["model"] != "test-model" {
			t.Errorf("model 应为 test-model,得 %v", body["model"])
		}
		w.Header().Set("Content-Type", "application/json")
		var msg string
		if wantReasoning {
			msg = `{"content":"","reasoning_content":"这是推理过程得出的结论"}`
		} else {
			msg = `{"content":"正常回答"}`
		}
		w.Write([]byte(`{"choices":[{"message":` + msg + `}],"usage":{"total_tokens":5}}`))
	}
}

// 推理模型只回 reasoning_content 且无工具调用 → 兜底成最终文本。
func TestChatReasoningFallback(t *testing.T) {
	srv := httptest.NewServer(chatHandler(t, true))
	defer srv.Close()
	out, err := newOpenAI(srv).Chat(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Content != "这是推理过程得出的结论" {
		t.Fatalf("空 content + reasoning 应兜底为 reasoning 文本,得 %q", out.Content)
	}
	if out.Reasoning != "这是推理过程得出的结论" {
		t.Fatalf("Reasoning 字段应保留,得 %q", out.Reasoning)
	}
}

// 正常响应 content 非空 → 不走兜底。
func TestChatNoFallbackOnContent(t *testing.T) {
	srv := httptest.NewServer(chatHandler(t, false))
	defer srv.Close()
	out, err := newOpenAI(srv).Chat(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Content != "正常回答" {
		t.Fatalf("正常 content 不应被替换,得 %q", out.Content)
	}
}

// Vision 请求体必须携带 image_url data URL,响应取 content。
func TestVisionSendsImageURL(t *testing.T) {
	img := filepath.Join(t.TempDir(), "diagram.png")
	os.WriteFile(img, []byte{0x89, 0x50, 0x4e, 0x47}, 0o644) // 任意字节,只验证 MIME/传输

	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, r.ContentLength)
		r.Body.Read(b)
		gotBody = string(b)
		w.Write([]byte(`{"choices":[{"message":{"content":"图中 A 节点指向 B 节点"}}]}`))
	}))
	defer srv.Close()

	desc, err := newOpenAI(srv).Vision(context.Background(), img, "描述这张图")
	if err != nil {
		t.Fatal(err)
	}
	if desc != "图中 A 节点指向 B 节点" {
		t.Fatalf("Vision 应返回 content,得 %q", desc)
	}
	if !strings.Contains(gotBody, `"type":"image_url"`) || !strings.Contains(gotBody, "data:image/png;base64,") {
		t.Fatalf("请求体缺少 image_url data URL: %s", gotBody)
	}
	if !strings.Contains(gotBody, "描述这张图") {
		t.Fatalf("请求体应带 prompt 文本: %s", gotBody)
	}
}

// 非图片扩展名 → Vision 报错。
func TestVisionRejectsBadExt(t *testing.T) {
	pdf := filepath.Join(t.TempDir(), "doc.pdf")
	os.WriteFile(pdf, []byte("dummy"), 0o644)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"choices":[{"message":{"content":"x"}}]}`))
	}))
	defer srv.Close()
	_, err := newOpenAI(srv).Vision(context.Background(), pdf, "描述")
	if err == nil || !strings.Contains(err.Error(), "扩展名") {
		t.Fatalf("非图片扩展名应报错,得 %v", err)
	}
}

func TestImageMIME(t *testing.T) {
	cases := map[string]string{
		"a.png": "image/png", "a.JPG": "image/jpeg", "a.webp": "image/webp", "a.gif": "image/gif",
	}
	for p, want := range cases {
		if got := imageMIME(p); got != want {
			t.Errorf("imageMIME(%s)=%q want %q", p, got, want)
		}
	}
	if imageMIME("a.txt") != "" {
		t.Error("未知扩展名应返回空")
	}
}
