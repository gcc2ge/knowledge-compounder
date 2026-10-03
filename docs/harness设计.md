# Harness 设计:全 Go 自研 agent,任意 LLM 可跑

> 目标:knowledge-compounder **不再通过 Claude Code 执行**——整个项目用 Go 实现自研 agent 运行时,同一套方法论(SCHEMA)在 OpenAI 兼容 / Anthropic 等任意模型下跑。方法论(SCHEMA.md)不变,执行层完全自持。

## 为什么自研(全 Go)

1. **不锁定模型**:方法论的价值在编译纪律,不在某个模型。`KCP_PROVIDER` 一换模型全换。
2. **可嵌入产品**:自研 agent 才能嵌进自己的服务/批量流水线/未来托管与 agent cloud。
3. **控制 harness**:工具暴露、上下文裁剪、停止条件、错误自愈自己定(M04 核心论断:模型外面的工程结构决定系统稳不稳)。
4. **Go 是生产语言**:单二进制、goroutine 并发(并行编译多源)、M11 Eino 可做框架落点、M12 ADK 可做 A2A 对外暴露。

## 五组件映射(M04)

```
Agent = LLM(决策器) + Tools(感知行动) + State(状态) + Loop(循环) + Stop(停止)
  ├─ LLM      → internal/provider:Provider 接口 + Capabilities.Tools 能力位
  ├─ Tools    → internal/tools:全 Go 工具(wiki 状态/检索/读写文件/lint/observe)
  ├─ State    → 任务级输入 + wiki/ 本身作为长记忆(记忆 = Messages 追加式)
  ├─ Loop     → internal/agent.Runtime:Think-Act-Observe 循环
  └─ Stop     → MaxSteps + MaxSameAction(防原地打转);MaxTokens/Deadline 见 TODO
```

## Agent 指令从哪来

`internal/agents/builtin.go` 内置三个角色(compiler/query/qa),指令精炼自 SCHEMA.md。**编译纪律原样进入 system prompt**。

```
internal/agents/builtin.go
  CompilerPrompt  → raw → wiki/sources 源页(论证链完整性/意外发现/连接+意义/矛盾标注/单次提及不建页)
  QueryPrompt     → 检索 + 带引用综合 + 建议 filed back
  QAPrompt        → 断链/孤儿/矛盾/覆盖/格式
```

## Provider 抽象(M02 能力位)

`internal/provider`:

| Provider | 走法 | 能力位 |
|---|---|---|
| `OpenAICompatible` | `/chat/completions` + tools | tools=True(覆盖 OpenAI/DeepSeek/Moonshot/OpenRouter/Ollama/vLLM/Gemini-OpenAI 端点,同一协议) |
| `Anthropic` | `/v1/messages` + tool_use/tool_result | tools=True |

`Capabilities.Tools=false` 时循环降级为文本协议(ReAct,已从 Python 原型验证沉淀):

```
Thought: 对任务的推理
Action: <tool> | <JSON入参>     # 无可用工具时 Action: none
Observation: <工具返回 / 环境反馈>
(循环,最多 MaxSteps 次)
Final: <最终答案>
```

harness 解析 Thought/Action/Observation/Final 四段;`Action: none` 或出现 `Final:` 即停。

## 上下文组装(M09 纪律)

system prompt = 角色指令(含 SCHEMA 摘要,稳定前缀命中 prompt caching);相关页面由 `search_wiki` 动态检索注入,不塞全库——上下文是工作台不是仓库。

## Go 包结构

```
cmd/kcp/main.go       CLI 入口 → internal/cli
internal/
  config/             环境配置(KCP_*)
  provider/           Provider 接口 + OpenAI 兼容 + Anthropic
  agent/              M04 循环 + 停止条件 + 错误自愈(panic 也转 Observation)
  agents/             内置角色 system prompt
  tools/              工具注册(写文件限 wiki/raw/examples 防穿越)
  wiki/               扫描(status)/检索/最小 lint(断链+孤儿)
  cli/                命令分发(status/compile/query/lint/observe/list/role)
```

## 工具清单(全 Go 实现)

| 工具 | 实现 | 说明 |
|---|---|---|
| `wiki_status` | internal/wiki.Scan | 计数 + 未编译 raw |
| `search_wiki` | internal/wiki.SearchPages | 标题命中×3 + 正文计数;TODO embedding+rerank |
| `get_page` | internal/wiki.GetPage | 读页面 |
| `read_file` | 白名单目录 | wiki/raw/examples/docs/... |
| `write_file` | 限 wiki/raw/examples | 防路径穿越 |
| `run_lint` | internal/wiki.Lint | 断链 + 孤儿 |

## CLI

```
kcp status                   知识库状态(零 LLM 依赖)
kcp compile <raw文件>        单源编译(compiler agent 按 SCHEMA)
kcp query "<问题>"           检索 + 综合 + 建议 filed back
kcp lint                     健康检查
kcp observe -s <策略> -T <标题> -c <内容>   捕获观察
kcp <role> "<输入>"          直接跑角色
```

## 与旧版的关系

- `.claude/` 与 `legacy/python-harness/` → 已删除(指令已提炼进 `internal/agents` + SCHEMA.md;ReAct 要点见上)
- `scripts/*.py` → 待移植到 Go(Phase 2:完整 lint/export-public/pdf2md/update/registry/relink)
- `context/wiki-mcp-server/`(Python MCP)→ Phase 5 移植 Go

## 开发路线(Phase)

> 骨架已成(Phase 0)。全部 TODO 按序推进,每阶段以可验收产物收尾。

| Phase | 内容 | 状态 |
|---|---|---|
| 0 | 全 Go 骨架:provider 抽象 + agent 循环 + 6 工具 + status/lint/observe CLI | ✅ `a7cc774` |
| 1 | 真实 LLM 端到端实测:compile/query 跑通,验证工具调用回填与停止条件,修循环 bug(DeepSeek/Ollama) | ✅ 智谱 GLM 实测通过(2026-10-03) |
| 2 | scripts/*.py 移植 Go:完整 lint(矛盾/格式/覆盖)、update(add-source/touch/add-link)、check-sources-shrink、index 重建、source-skeleton。pdf2md/export-public 保留 shell(依赖外部工具) | ✅ 分批完成(2026-10-03) |
| 3 | 生产化 harness:SSE 流式(M02 两层流式栈)、MaxTokens/Deadline/MaxHeal、checkpoint 持久化(M04 Store)、策展决策点暂停要人 | ✅ 完成(2026-10-03) |
| 4 | 检索与质量:grep → embedding + rerank(Agentic RAG)、证据 A/B/C 分层、评估集(Agent-as-a-Judge)、RAG-vs-编译对照实验 | ⬜ |
| 5 | context/wiki-mcp-server Python → Go(支柱 B 完整 MCP server,喂 coding/trading agents) | ⬜ |
| 6 | A2A 暴露(M12 ADK launcher)给 agent cloud + License 策略定稿(AGPL/BSL vs MIT) | ⬜ |

## 边界与 TODO

> 各项已在「开发路线」中按 Phase 归位,此处保留细节。

- **SSE 流式**(Phase 3):骨架非流式;生产补 SSE(M02 两层流式栈)
- **停止条件**(Phase 1+3):MaxSteps/MaxSameAction 已就位;补 MaxTokens/Deadline/MaxHeal
- **检索升级**(Phase 4):grep → embedding + rerank(Agentic RAG);证据 A/B/C 分层
- **人工把关点**(Phase 3):compiler 在"建概念页?"策展决策处暂停要用户裁决("人策展>自动"进协议)
- **状态 checkpoint**(Phase 3):可中断/可恢复/可审计(M04 Store 持久化)
- **评估集**(Phase 4):Agent-as-a-Judge 验证编译质量
- **A2A 暴露**(Phase 6):M12 ADK launcher 包一层,开放给远程 agent
