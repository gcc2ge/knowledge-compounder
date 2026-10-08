# 支柱 B:wiki → MCP → coding/trading agents

把编译好的 `wiki/` 暴露成 MCP Server,任何支持 MCP 的客户端(Claude Code、Codex、自定义 agent)都能**按需**读取你的私有知识库——这就是"知识库给 agent 当外脑"。

**纪律来自上下文工程:上下文是工作台不是仓库,按需取。** 不要把 wiki 全塞进 system prompt;agent 需要时才调用工具注入相关页面。

## 快速开始

全 Go、零依赖(纯 stdlib 实现 MCP JSON-RPC 协议,不引 mcp-go SDK)。

```bash
cd knowledge-compounder
go build -o kcp ./cmd/kcp
./kcp mcp        # MCP stdio server,在项目根目录运行(自动定位 SCHEMA.md 所在根)
```

然后在 Claude Code / Codex 的 `.mcp.json` 里接上(见 `../.mcp.json.example`):

```json
{ "mcpServers": { "wiki-context": {
  "command": "/绝对/路径/knowledge-compounder/kcp",
  "args": ["mcp"], "env": { "KCP_EMBED_MODEL": "...", "KCP_API_KEY": "..." } } } }
```

## 工具

| 工具 | 作用 | 状态 |
|---|---|---|
| `search_wiki(query, k)` | 混合检索(词法+语义向量,Phase 4),返回标题+**私有度徽标** `[A]/[B]/[C]`+命中分+摘要 | ✅ |
| `get_page(page)` | 读取一个 wiki 页面 | ✅ |
| `get_related(page)` | 解析页面的 `[[wikilinks]]`,返回可解析/断链去向 | ✅ |
| `list_recent(n)` | 最近修改的 wiki 页 | ✅ |
| `synthesize_for(question)` | Agentic RAG 综合回答(检索+LLM 综合,带来源与徽标引用);需配 `KCP_API_KEY` | ✅ |

## 环境变量

检索工具零 LLM 可跑;`synthesize_for` 需要 LLM:

- `KCP_PROVIDER` / `KCP_MODEL` / `KCP_API_KEY` / `KCP_BASE_URL` —— synthesize_for 的 LLM
- `KCP_EMBED_MODEL` / `KCP_EMBED_BASE_URL` / `KCP_EMBED_API_KEY` —— 语义检索;留空用离线字符哈希嵌入
- `KCP_RETRIEVE_K` —— 默认检索条数(默认 5)

## 为什么不"全塞进窗口"

见 SCHEMA 质量规则与 `docs/02-运行原理.md`:长上下文稀释注意力、推高成本;只有"结论性、当前必需"的内容该进上下文,其余按需取回。
