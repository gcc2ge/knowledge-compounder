# 支柱 B:wiki → MCP → coding/trading agents

把编译好的 `wiki/` 暴露成 MCP Server,任何支持 MCP 的客户端(Claude Code、Codex、自定义 agent)都能**按需**读取你的私有知识库——这就是"知识库给 agent 当外脑"。

**纪律来自上下文工程:上下文是工作台不是仓库,按需取。** 不要把 wiki 全塞进 system prompt;agent 需要时才调用工具注入相关页面。

## 快速开始

```bash
cd context/wiki-mcp-server
python3 -m venv .venv && source .venv/bin/activate
pip install -r requirements.txt
python3 server.py                 # 默认 stdio,在项目根目录运行(读 ../wiki)
```

然后在 Claude Code / Codex 的 `.mcp.json` 里接上(见 `../.mcp.json.example`)。

## 工具

| 工具 | 作用 | 实现状态 |
|---|---|---|
| `search_wiki(query, k)` | 关键词检索 wiki(标题+正文加权) | ✅ 骨架(grep 级) |
| `get_page(page)` | 读取一个 wiki 页面 | ✅ |
| `get_related(page)` | 解析页面的 `[[wikilinks]]`,返回关联页 | ✅ |
| `list_recent(n)` | 最近编译的源页 | ✅ |
| `synthesize_for(question)` | 带 A/B/C 证据分层的综合回答 | 🔜 TODO:Agentic RAG |

## TODO(从骨架到可用)

1. **检索升级**:grep 级 → embedding 检索 + rerank(见 `docs/循环设计.md` 的"编译 vs RAG 对照实验")
2. **synthesize_for 落地**:用 compile 循环 + SCHEMA 的 synthesis 模板做"查询即投资"——综合结果 filed back 到 `wiki/synthesis/`
3. **证据分层**:返回片段带 A/B/C 私有度标注
4. **A2A 暴露**:如需给远程 agent 调用,用 ADK launcher 包一层(见战略层的 A2A 讨论)

## 为什么不"全塞进窗口"

见 SCHEMA 质量规则与 `docs/循环设计.md`:长上下文稀释注意力、推高成本;只有"结论性、当前必需"的内容该进上下文,其余按需取回。
