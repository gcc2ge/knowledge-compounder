# Harness:自研运行时,任意 LLM 可跑

让 knowledge-compounder 不依赖 Claude Code——**方法论(SCHEMA/agents/scripts)不变,执行引擎换成自己的**。

## 配置(环境变量)

```bash
export KCP_PROVIDER=openai-compatible   # openai-compatible | anthropic
export KCP_MODEL=deepseek-chat
export KCP_API_KEY=sk-xxx
export KCP_BASE_URL=                    # 默认 OpenAI 端点;Ollama 用 http://localhost:11434/v1
# 常用预设见 harness/config.py(openai/deepseek/anthropic/ollama/gemini-openai)
```

## 用法

```bash
# 跑一个 agent 角色(compiler/qa/coordinator/batch-compiler/converter)
python -m harness compiler "编译 raw/xxx.md,按 SCHEMA 生成 source 页"

python -m harness status          # 等价旧 Claude Code SessionStart hook
python -m harness compile raw/foo.md
python -m harness query "知识库里对 X 有哪些结论?"
python -m harness lint
python -m harness observe -s strategy -T "标题" -c "内容"
python -m harness list             # 可用 agent 角色
```

## 结构

```
harness/
├── config.py     环境配置(provider/model/key/停止参数)
├── providers.py  Provider 抽象 + 能力位(OpenAI 兼容/Anthropic)
├── loop.py       M04 Agent 循环:Think-Act-Observe + 工具 + 停止条件
├── tools.py      把 scripts/ 注册成工具(search/read/write/lint/observe)
├── agents.py     加载 .claude/agents/*.md → system prompt
└── cli.py        命令行入口
```

## 原理(对应 M04/M02/M06/M09)

- **Provider 抽象**(M02):OpenAI 兼容协议覆盖大部分模型;Anthropic 走原生 tools;能力位决定 function calling 还是 ReAct 降级。
- **循环**(M04):工具结果作为 Observation 回填 messages;错误喂回模型自愈;MaxSteps/MaxSameAction 防打转。
- **工具**(M06):白名单脚本 + 文件读写 + 检索,写文件只允许 wiki/raw/examples 内。
- **上下文**(M09):system prompt = SCHEMA 摘要 + 角色指令(稳定前缀),相关页面动态检索注入,不塞全库。

## TODO(骨架 → 生产)

- [ ] 流式输出(SSE,见 M02 两层流式栈)
- [ ] 停止条件补 MaxTokens/Deadline/MaxHeal
- [ ] search_wiki 升级 embedding + rerank(Agentic RAG)
- [ ] compile 流程加"策展决策点"(建概念页?矛盾标注?)暂停要人裁决
- [ ] 任务状态 checkpoint(可中断/可恢复/可审计)
- [ ] 评估集(Agent-as-a-Judge)验证编译质量
