# Harness 设计:任意 LLM 下跑知识编译复利

> 目标:把 knowledge-compounder 从"Claude Code 模板"升格为**自研 harness**——同一套方法论,在 OpenAI / Anthropic / Gemini / DeepSeek / 本地 Ollama 等任意模型下都能跑。方法论(SCHEMA)不变,只是执行层从 Claude Code 换成我们自己的运行时。

## 为什么自研

1. **不锁定模型**:方法论的价值在编译纪律,不在某个模型。换模型要零成本。
2. **可嵌入产品**:Claude Code 是交互式 CLI(给人用);自研 harness 才能嵌进自己的服务/批量流水线/未来卖托管。
3. **控制 harness**:工具暴露、上下文裁剪、停止条件、错误自愈都由自己定(M04 的核心论断:模型外面的工程结构决定系统稳不稳)。

## 五组件映射(M04)

```
Agent = LLM(决策器) + Tools(感知行动) + State(状态) + Loop(循环) + Stop(停止)
  ├─ LLM      → harness/providers.py:Provider 抽象 + 能力位(支持工具→function calling,否则降级 ReAct)
  ├─ Tools    → harness/tools.py:把 scripts/*.py + 文件读写 + 检索注册成可调用函数
  ├─ State    → 任务级 JSON(checkpoint)+ wiki/ 本身作为长记忆(记忆 = Messages 追加式)
  ├─ Loop     → harness/loop.py:Think-Act-Observe 循环
  └─ Stop     → MaxSteps / MaxSameAction(防原地打转)/ MaxHeal(自愈上限)
```

## Agent 指令从哪来

`.claude/agents/*.md` 本来就是指令文本——harness 解析 frontmatter(`name`/`description`)注册角色,正文直接当 system prompt。**同一份方法论指令,换了执行引擎。**

```python
# harness/agents.py 伪代码
def load_agent(role):
    frontmatter, body = parse_md(f".claude/agents/{role}.md")
    return Agent(name=frontmatter["name"],
                 system_prompt=body,          # ← 编译纪律原样进入
                 suggested_tools=frontmatter.get("tools", []))
```

## Provider 抽象(M02 能力位)

| Provider | 走法 | 能力位 |
|---|---|---|
| OpenAI 兼容 | `/chat/completions` + tools | tools=True(覆盖 OpenAI/DeepSeek/Moonshot/OpenRouter/Ollama/vLLM,同一协议) |
| Anthropic | `/v1/messages` + tools(tool_use/tool_result) | tools=True |
| Gemini | 走 Google 的 OpenAI 兼容端点 | tools=True(复用 OpenAICompatible) |

`Capabilities.tools=False` 时循环降级 ReAct(纯文本 `Thought/Action/Action Input` + stop sequence)。

## 上下文组装(M09 纪律)

每次调用 = **稳定前缀 + 动态后缀**:

```
system:
  [SCHEMA 摘要]        ← 稳定,命中 prompt caching
  [当前角色指令正文]     ← 稳定
  [相关 wiki 页动态注入] ← 动态:harness 按任务先检索,只放当前必需的
user: 用户输入 / 工具结果
```

不把整个 wiki 塞进 system——按需检索(M09:上下文是工作台不是仓库)。

## 工具清单(骨架)

| 工具 | 实现 | 来源 |
|---|---|---|
| `wiki_status` | 跑 `scripts/wiki-status.py --json` | 脚本 |
| `run_lint` | 跑 `scripts/wiki-lint.py` | 脚本 |
| `search_wiki` | 复用 context/wiki-mcp-server 的 grep 检索 | context |
| `get_page` / `get_related` | 读 wiki 页 + 解析 wikilinks | 同上 |
| `read_file` / `write_file` | 编译 agent 写页面 | 内置 |
| `run_script` | 安全执行 scripts/ 内白名单脚本 | 内置 |
| `observe` | 跑 `scripts/observe.py` | 脚本 |

## CLI

```
python -m harness <role> "<用户输入>"     # 跑一个 agent(compiler/qa/coordinator...)
python -m harness status                   # 等价旧 SessionStart hook
python -m harness compile <raw文件>        # 单源编译(compiler agent + 人工把关点)
python -m harness query "问题"             # 查询 + 建议 filed back
python -m harness lint
python -m harness observe -s <strategy> -c "内容"
```

## 边界与 TODO

- **流式输出**:骨架用非流式,生产补 SSE(见 M02 两层流式栈)
- **停止条件**:骨架有 MaxSteps/MaxSameAction;补 MaxTokens/Deadline/MaxHeal
- **检索升级**:grep → embedding + rerank(Agentic RAG)
- **人工把关点**:compiler 在"建概念页?"等策展决策处暂停要用户裁决(把"人策展>自动"从 hook 变成协议)
- **A2A 暴露**:将来开放给远程 agent 调用时,用 ADK launcher 包一层
