# Knowledge Compounder — 知识编译复利引擎

> **多数 AI 笔记工具做摘要——一次性的、用完即弃。这套系统做的是编译:**
> 每次摄入 → 交叉引用 5-15 个已有页;每次查询 → 带引用的综合并 filed back;每次失败 → 作为观察编译回灌。每轮循环,wiki 更丰富,决策更好——**你的知识像复利一样增长。**

一个开源的 **AI 时代云笔记** 内核:AI 帮你把碎片信息编译成结构化、交叉链接、带矛盾标注与置信度的私有知识库——这份资产属于你、可导出、跨场景复用,既给人看,也给 coding / 交易等场景 agents 当外脑。

## 它解决什么

你喂进 `raw/` 的文章、论文、剪藏、亏损案例,不再是"一堆文档 + RAG 检索"——而是被**编译**成知识资产:

- **论证链完整性**:每个 SQL / 代码块 / 对比表 / 关键示例逐字保留,不缩写
- **意外发现与私有连接**:每页必须写"这让我想到什么 / 这在你的场景意味着什么"
- **矛盾显式标注 + 置信度分层**:单源=low、多源=high,模型知道该多信哪句
- **查询即投资**:每次好查询 filed back 成 synthesis 页,下一次更强
- **失败回灌**:observe 闭环把踩坑编译回库,下次 agent 直接带坑的知识工作

## 与 RAG 外挂知识库的本质区别

> **RAG 解决"怎么取知识"(读取机制,取完不增值);编译复利解决"知识怎么变成资产并复利增值"(编译 + 查询投资 + 失败回灌)。一个是外挂,一个是本体。**

| | RAG 外挂知识库 | 知识编译复利 |
|---|---|---|
| 存什么 | 原文碎片(平) | 思考结果:论证链/意外发现/连接+意义/矛盾/置信度 |
| 查询性质 | 消耗,答案用完即弃 | 投资,filed back 让库更密 |
| 随使用变化 | 不变 | 复利增长 |
| 从错误学习 | 不会 | 会(observe 回灌) |

详见 `docs/循环设计.md`。

## 快速上手

```bash
# 1. 克隆模板,进入项目
git clone https://github.com/<you>/knowledge-compounder.git && cd knowledge-compounder

# 2. 装一个笔记源(raw/ 支持 .md/.txt/.pdf/.epub/.docx)
#    或者用 observe 捕获一条观察
python3 scripts/observe.py -s strategy -T "发现:费率异常" -c "描述"

# 3. 用 Claude Code 打开项目(它自带 coordinator + compiler + qa agents)
#    告诉 coordinator:编译 raw/ 里的新文件
#    → compiler 编译进 wiki/,qa lint,index/log 自动维护

# 4. 看状态
python3 scripts/wiki-status.py

# 5. 把知识库喂给其他 agent 当外脑
#    → context/ 目录的 MCP server 把 wiki 暴露成 MCP 工具
#    → 在 Claude Code / Codex 的 .mcp.json 里接上(见 context/.mcp.json.example)
```

## 自研 harness:任意 LLM 可跑

方法论不绑定模型。除了用 Claude Code 作为执行引擎,项目自带**自研 harness**(`harness/`):同一套 SCHEMA + agent 指令,在 OpenAI 兼容(DeepSeek/Ollama/OpenRouter/…)、Anthropic 等任意模型下跑。

```bash
export KCP_PROVIDER=openai-compatible  # 或 anthropic
export KCP_MODEL=deepseek-chat
export KCP_API_KEY=sk-xxx
python -m harness compile raw/foo.md    # 等价"Claude 编译",但走自己的运行时
python -m harness status
```

设计见 `docs/harness设计.md`(M04 五组件映射 + M02 Provider 能力位 + M09 上下文纪律)。

## 项目结构

```
knowledge-compounder/
├── SCHEMA.md            ← 编译纪律:页面格式约定(本项目的心智)
├── harness/             ← 自研运行时:任意 LLM 可跑(providers/loop/tools/cli)
├── .claude/
│   ├── agents/          ← coordinator/compiler/qa/batch-compiler/converter(同一指令,双引擎复用)
│   └── skills/          ← observe 捕获技能
├── scripts/             ← 状态/lint/更新/PDF转换/隐私导出/观察
├── templates/           ← source/concept/entity/synthesis 页面模板
├── examples/            ← 合成演示:raw + 编译产物,展示纪律
├── context/             ← 支柱B:wiki 暴露为 MCP server,喂给 coding/trading agents
└── docs/ 上手.md  循环设计.md  隐私模型.md  harness设计.md
```

## 两个支柱 + 一条闭环

```
支柱 A: compiler(写)   raw → 编译进 wiki(批量、自主 agent,SCHEMA 驱动)
支柱 B: context(读)    wiki → MCP server → coding/trading/自定义 agents 当外脑
闭环:   observe        踩坑/失败/新模式 → raw/observations → 编译回灌 → wiki 更密
```

## 隐私模型

- 知识库**属于你**:`wiki/` 可导出、可带走(git 天然版本历史)
- 页面级排除(`public: false`)与段落级标记(`<!-- PRIVATE -->`)双机制
- `scripts/export-public.sh` 一键导出公开版,自动替换薪资/股权/本地路径等敏感字段
- **开源的是方法论,不是你的知识**:你的 `wiki/` 与 `raw/` 不进入本项目(见 `.gitignore`)

## License

MIT(如你计划在其上卖托管服务,考虑换 AGPL/BSL 防白嫖——见 `docs/循环设计.md` 的讨论)。

---

*由 [知识库编译方法论产品化] 的决策路径落地:edge 在私有连接不在自动化;编译流水线自动化,判断留给人;开源方法论,私有 edge 留私库。*
