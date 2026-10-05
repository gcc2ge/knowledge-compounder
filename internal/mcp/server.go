// Package mcp 支柱 B:MCP stdio server,把编译好的 wiki/ 暴露成工具,喂给 coding/trading agents。
// 纯 stdlib 实现 MCP 协议(JSON-RPC 2.0,stdio 换行分隔)——不引外部 MCP SDK,保持零依赖。
// 检索复用 internal/retrieval 混合检索 + 证据 A/B/C 徽标(Phase 4)。
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gcc2ge/knowledge-compounder/internal/config"
	"github.com/gcc2ge/knowledge-compounder/internal/provider"
	"github.com/gcc2ge/knowledge-compounder/internal/retrieval"
	"github.com/gcc2ge/knowledge-compounder/internal/wiki"
)

const (
	serverName    = "wiki-context"
	serverVersion = "0.6.0"
	protocolVer   = "2024-11-05"
)

// Server MCP 服务器:root 为项目根,opts 为检索选项(embedder/缓存,来自 KCP_* 环境)。
type Server struct {
	root  string
	opts  retrieval.Options
	tools map[string]toolDef
}

type toolDef struct {
	name, desc string
	params     map[string]any
	call       func(map[string]any) string
}

// New 构造并注册工具。
func New(root string, opts retrieval.Options) *Server {
	s := &Server{root: root, opts: opts, tools: map[string]toolDef{}}
	s.register()
	return s
}

func (s *Server) register() {
	s.tools["search_wiki"] = toolDef{
		name: "search_wiki", desc: "混合检索 wiki 页面(词法+语义向量),返回标题+私有度徽标+命中分+摘要",
		params: obj(map[string]any{"query": strProp, "k": intProp}),
		call: func(a map[string]any) string {
			q := strArg(a, "query")
			if q == "" {
				return "参数 query 必填。"
			}
			opts := s.opts
			if k := intArg(a, "k", 5); k > 0 {
				opts.Top = k
			}
			res := wiki.Retrieve(s.root, q, opts)
			if len(res) == 0 {
				return "知识库中未找到与查询匹配的页面。"
			}
			var b strings.Builder
			for _, r := range res {
				fmt.Fprintf(&b, "- [[%s]] %s 命中%.2f: %s\n", r.Label, wiki.EvidenceBadge(s.root, r.Path), r.Score, r.Summary)
			}
			return b.String()
		},
	}
	s.tools["get_page"] = toolDef{
		name: "get_page", desc: "读取一个 wiki 页面(slug 或文件名,如 `Agent核心架构` 或 `Agent核心架构.md`)",
		params: obj(map[string]any{"page": strProp}),
		call: func(a map[string]any) string {
			slug := strings.TrimSuffix(strArg(a, "page"), ".md")
			text, dir, ok := wiki.GetPage(s.root, slug)
			if !ok {
				return fmt.Sprintf("页面 [[%s]] 不存在。", slug)
			}
			return fmt.Sprintf("## %s (来源 %s/)\n\n%s", slug, dir, text)
		},
	}
	s.tools["get_related"] = toolDef{
		name: "get_related", desc: "解析一个页面的 [[wikilinks]],返回它关联了哪些知识节点及其去向(断链单独列出)",
		params: obj(map[string]any{"page": strProp}),
		call: func(a map[string]any) string {
			slug := strings.TrimSuffix(strArg(a, "page"), ".md")
			text, _, ok := wiki.GetPage(s.root, slug)
			if !ok {
				return fmt.Sprintf("页面 [[%s]] 不存在。", slug)
			}
			links := wiki.ParseWikilinks(text)
			var resolved, broken []string
			for _, l := range links {
				if wiki.ResolveLink(s.root, l) {
					resolved = append(resolved, l)
				} else {
					broken = append(broken, l)
				}
			}
			return fmt.Sprintf("[[%s]] 出站链接 %d 条:\n- 可解析: %s\n- 断链: %s",
				slug, len(links), resolved, orNone(broken))
		},
	}
	s.tools["list_recent"] = toolDef{
		name: "list_recent", desc: "列出最近修改的 wiki 页面(按 mtime 倒序)",
		params: obj(map[string]any{"n": intProp}),
		call: func(a map[string]any) string {
			n := intArg(a, "n", 5)
			type mod struct {
				path string
				t    time.Time
			}
			var ms []mod
			for _, p := range wiki.Pages(s.root) {
				fi, err := os.Stat(p)
				if err != nil {
					continue
				}
				ms = append(ms, mod{path: p, t: fi.ModTime()})
			}
			sort.Slice(ms, func(i, j int) bool { return ms[i].t.After(ms[j].t) })
			var b strings.Builder
			for i := 0; i < len(ms) && i < n; i++ {
				fmt.Fprintf(&b, "- [[%s]]\n", strings.TrimSuffix(filepath.Base(ms[i].path), ".md"))
			}
			if b.Len() == 0 {
				return "暂无页面。"
			}
			return strings.TrimSuffix(b.String(), "\n")
		},
	}
	s.tools["synthesize_for"] = toolDef{
		name: "synthesize_for", desc: "Agentic RAG 综合回答:检索相关页面(带私有度徽标)并用 LLM 综合成带引用的答案",
		params: obj(map[string]any{"question": strProp}),
		call: func(a map[string]any) string {
			return s.synthesize(strArg(a, "question"))
		},
	}
}

// ---- synthesize_for:检索 + LLM 综合 ----

const synthPrompt = `你是知识库综合助手(Agentic RAG)。用户的问题:
%s

以下是检索到的相关知识(标注私有度:[A]公开 / [B]精选综合 / [C]私有):
%s

要求:
- 综合回答,不要罗列碎片
- 每条关键证据标注来源([[页名]])与私有度徽标
- 资料间矛盾时两边都呈现并标注
- 控制在 500 字内,直接输出答案`

func (s *Server) synthesize(question string) string {
	if question == "" {
		return "参数 question 必填。"
	}
	cfg := config.Load()
	if cfg.APIKey == "" && cfg.BaseURL == "" {
		return "未配置 KCP_API_KEY / KCP_BASE_URL,synthesize_for 需要 LLM;其余检索工具可用。"
	}
	p := newProvider(cfg)
	res := retrieval.Search(s.wikiDocs(), question, s.opts)
	if len(res) == 0 {
		return "知识库中未检索到相关内容。"
	}
	var ctxText strings.Builder
	for _, r := range res {
		fmt.Fprintf(&ctxText, "【%s】%s 命中%.2f\n", r.Label, wiki.EvidenceBadge(s.root, r.Path), r.Score)
		text := wiki.Read(r.Path)
		if len(text) > 2000 {
			text = text[:2000]
		}
		ctxText.WriteString(text)
		ctxText.WriteString("\n\n")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	msg, err := p.Chat(ctx, []provider.Message{
		{Role: "system", Content: "你是知识库综合助手,基于给定资料作答,标注来源与私有度徽标。"},
		{Role: "user", Content: fmt.Sprintf(synthPrompt, question, ctxText.String())},
	}, nil, nil)
	if err != nil {
		return "综合失败: " + err.Error()
	}
	return strings.TrimSpace(msg.Content)
}

func (s *Server) wikiDocs() []retrieval.Doc {
	var docs []retrieval.Doc
	for _, p := range wiki.Pages(s.root) {
		text := wiki.Read(p)
		if text == "" {
			continue
		}
		docs = append(docs, retrieval.Doc{Path: p, Label: filepath.Base(p), Text: text})
	}
	return docs
}

// ---- MCP 协议(JSON-RPC 2.0,stdio 换行分隔) ----

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Serve 在 in/out 上跑 stdio 循环,直到输入关闭。out 必须保持纯净(只有 JSON-RPC)。
func (s *Server) Serve(in io.Reader, out io.Writer) error {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 1<<20), 8<<20)
	enc := json.NewEncoder(out)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var req request
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			continue // 畸形帧忽略
		}
		resp := s.handle(req)
		if resp == nil {
			continue // notification,不回包
		}
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}
	return sc.Err()
}

func (s *Server) handle(req request) *response {
	rpc := func(id json.RawMessage, result any) *response {
		return &response{JSONRPC: "2.0", ID: id, Result: result}
	}
	fail := func(id json.RawMessage, code int, msg string) *response {
		return &response{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}}
	}
	// notification:无 id 或 null id,不回包
	if len(req.ID) == 0 || string(req.ID) == "null" {
		return nil
	}
	switch req.Method {
	case "initialize":
		return rpc(req.ID, map[string]any{
			"protocolVersion": protocolVer,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": serverName, "version": serverVersion},
		})
	case "ping":
		return rpc(req.ID, map[string]any{})
	case "tools/list":
		var list []map[string]any
		for _, t := range s.tools {
			list = append(list, map[string]any{
				"name": t.name, "description": t.desc, "inputSchema": t.params,
			})
		}
		return rpc(req.ID, map[string]any{"tools": list})
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		_ = json.Unmarshal(req.Params, &p)
		t, ok := s.tools[p.Name]
		if !ok {
			return fail(req.ID, -32602, "未知工具: "+p.Name)
		}
		text := t.call(p.Arguments)
		return rpc(req.ID, map[string]any{
			"content": []map[string]any{{"type": "text", "text": text}},
		})
	default:
		return fail(req.ID, -32601, "Method not found: "+req.Method)
	}
}

// ---- 参数辅助 ----

func obj(props map[string]any) map[string]any {
	return map[string]any{"type": "object", "properties": props}
}

var (
	strProp = map[string]any{"type": "string"}
	intProp = map[string]any{"type": "integer"}
)

func strArg(a map[string]any, k string) string {
	if v, ok := a[k].(string); ok {
		return v
	}
	return ""
}

func intArg(a map[string]any, k string, def int) int {
	switch v := a[k].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil {
			return n
		}
	}
	return def
}

func orNone(items []string) string {
	if len(items) == 0 {
		return "无"
	}
	return strings.Join(items, ", ")
}

func newProvider(cfg config.Config) provider.Provider {
	if cfg.Provider == "anthropic" {
		return provider.NewAnthropic(cfg.Model, cfg.APIKey, cfg.BaseURL)
	}
	return provider.NewOpenAICompatible(cfg.Model, cfg.APIKey, cfg.BaseURL)
}
