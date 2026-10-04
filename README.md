# Knowledge Compounder — 知识编译复利引擎

> **多数 AI 笔记工具做摘要——一次性的、用完即弃。这套系统做的是编译:**
> 每次摄入 → 交叉引用 5-15 个已有页;每次查询 → 带引用的综合并 filed back;每次失败 → 作为观察编译回灌。每轮循环,wiki 更丰富,决策更好——**你的知识像复利一样增长。**

一个开源的 **AI 时代云笔记** 内核:AI 帮你把碎片信息编译成结构化、交叉链接、带矛盾标注与置信度的私有知识库——这份资产属于你、可导出、跨场景复用,既给人看,也给 coding / 交易等场景 agents 当外脑。

**血缘**:方向源自 [Karpathy 的「LLM Wiki / Second Brain」](https://gist.github.com/karpathy/442a6bf555914893e9891c11519de94f)——让 LLM 持续维护一个结构化 Wiki,知识「编译一次、持续维护」,而不是每次查询重新检索。本项目把这条路线工程化:纯 Go 单二进制、任意 LLM 可跑、零第三方依赖,把编译纪律与复利闭环(查询回灌/失败回灌)做成可验收的 CLI 工作流。

**文档导航**
- `docs/运行原理.md` — 系统怎么运转(raw → 编译 → 复利闭环 → 喂给 agent)
- `docs/场景应用.md` — 编程 / 交易 / 个人学习 / 办公四个场景怎么用
- `docs/与RAG的区别.md` — 为什么这不是「又一个 RAG 笔记工具」
- `docs/循环设计.md` — 知识复利的运转机制与对照实验
- `docs/上手.md` — 逐步入门

## 它解决什么

你喂进 `raw/` 的文章、论文、剪藏、亏损案例,不再是"一堆文档 + RAG 检索"——而是被**编译**成知识资产:

- **论证链完整性**:每个 SQL / 代码块 / 对比表 / 关键示例逐字保留,不缩写
- **意外发现与私有连接**:每页必须写"这让我想到什么 / 这在你的场景意味着什么"
- **矛盾显式标注 + 置信度分层**:单源=low、多源=high,模型知道该多信哪句
- **查询即投资**:每次好查询 filed back 成 synthesis 页,下一次更强
- **失败回灌**:observe 闭环把踩坑编译回库,下次 agent 直接带坑的知识工作

## 与 RAG 的本质区别:外挂 vs 本体

> **RAG 解决"怎么取知识"(读侧/机制/外挂,取完不增值);编译复利解决"知识怎么变成资产并复利增值"(写侧/资产/本体,越用越富)。一个是外挂,一个是本体。**

**大多数 AI 笔记工具做摘要——一次性的、用完即弃。这套系统做的是编译**。用向量库检索原文碎片是 RAG——它的对象是信息平面;kcp 检索的是**编译后的思考结果**(论证链/意外发现/连接+意义/矛盾标注/置信度),它是立体的知识资产。

| | RAG 外挂知识库 | 知识编译复利 |
|---|---|---|
| 检索的对象 | 原文碎片(平) | 思考结果:论证链/意外发现/连接+意义/矛盾/置信度 |
| 矛盾处理 | 无,各取一半喂模型 | 写时显式标注,反映到「张力与缺口」 |
| 置信度 | 无,所有片段同等可信 | 单源=low、多源=high |
| 查询性质 | 消耗,答案用完即弃 | 投资,filed back 让库更密 |
| 随使用变化 | 不变 | 复利增长 |
| 从错误学习 | 不会 | 会(observe 回灌) |

四条本质差别(详见 `docs/与RAG的区别.md`):
1. **知识复利 vs 原地踏步**——RAG 语料是死的,上一轮洞见不沉淀;kcp 每次好查询 filed back,越用越富
2. **跨源综合 vs 原子 chunk**——RAG 给你几块可能矛盾的片段让模型现场仲裁;kcp 写时就完成综合与矛盾标注
3. **写时付费 vs 读时付费**——RAG 索引便宜但每问一次付一次检索+推理钱;kcp 编译贵但查询读到挤干水分的页面
4. **错误模式不同**——RAG 错了是本次错;kcp 编译错误会固化,靠 lint + review_after 兜底

kcp 的检索层**本身是 RAG 的生产基线**(BM25+向量 0.55/0.45 融合),但检索的是编译资产——**增值来自编译,不来自检索**。`kcp eval` 可复现对照实验(实测编译复利 vs 原文 RAG:+1.1 均分,综合/迁移类问题差距最大)。

详见 `docs/循环设计.md` 与 `docs/与RAG的区别.md`。

## 快速上手

> 要求:Go 1.24+。纯 Go、零第三方依赖,`go build` 一个二进制搞定。

```bash
# 1. 克隆模板,进入项目,编译自研 agent
git clone https://github.com/<you>/knowledge-compounder.git && cd knowledge-compounder
go build -o kcp ./cmd/kcp

# 2. 先验证安装(以下全部零 LLM,不需要任何 API key)
./kcp status                    # 知识库状态 + 未编译 raw(内置 demo:1 源已编译)
./kcp lint                      # 健康检查(断链/孤儿/格式)
./kcp search "go channel 泄漏"  # 混合检索诊断(词法+向量,带 [A]/[B]/[C] 私有度徽标)

# 3. 配置模型(任意 LLM:DeepSeek/Ollama/智谱 GLM/OpenRouter…)
export KCP_PROVIDER=openai-compatible   # 或 anthropic
export KCP_MODEL=deepseek-chat          # 换成你的模型(本地 ollama 可留空 key)
export KCP_API_KEY=sk-xxx
export KCP_BASE_URL=https://api.deepseek.com/v1   # ⚠ 必须与你选的模型一致!
#   DeepSeek → https://api.deepseek.com/v1
#   Ollama   → http://localhost:11434/v1
#   智谱 GLM → https://open.bigmodel.cn/api/paas/v4
#   OpenRouter→ https://openrouter.ai/api/v1

# 4. 装一个笔记源,然后编译(raw/ 直接放 .md/.txt;PDF 先 scripts/pdf2md.sh,epub/docx 先转 md)
./kcp compile raw/你的文件.md # compiler agent 按 SCHEMA 编译(SSE 流式,策展点暂停问人)

# 5. 查询并 filed back(查询即投资)
./kcp query "知识库里对 X 有什么结论?"
./kcp search "X 相关关键词"

# 6. 对照实验:编译复利 vs RAG 外挂(Agent-as-a-Judge 打分)
./kcp eval                    # 评估种子见 eval/seeds/(按你的知识库改写),报告落 eval/reports/

# 7. 把知识库喂给其他 agent 当外脑
#    → ./kcp mcp 把 wiki 暴露成 MCP server(支柱 B,纯 Go 零依赖)
#    → 在 Claude Code / Codex 的 .mcp.json 里接上(见 context/.mcp.json.example)
```

## 自研 agent:全 Go,任意 LLM 可跑

**不再依赖 Claude Code 执行**——整个项目用 Go 实现自己的 agent 运行时(`cmd/kcp` + `internal/`),同一套方法论(SCHEMA)在 OpenAI 兼容(DeepSeek/Ollama/OpenRouter/…)、Anthropic 等任意模型下跑。方法论与执行引擎彻底解耦。

```bash
go build -o kcp ./cmd/kcp
export KCP_PROVIDER=openai-compatible  # 或 anthropic
export KCP_MODEL=deepseek-chat
export KCP_API_KEY=sk-xxx
export KCP_BASE_URL=https://api.deepseek.com/v1   # 与模型对应,见上方快速上手
./kcp status                 # 知识库状态(零 LLM 依赖)
./kcp compile examples/raw-demo.md   # 单源编译(compiler agent)
./kcp query "知识库里对 X 有哪些结论?"
./kcp lint                   # 健康检查
./kcp observe -s strategy -T "发现" -c "描述"
```

Go 包结构对应 M04 五组件:

```
cmd/kcp                CLI 入口
internal/provider     M02 Provider 抽象 + 能力位(OpenAI 兼容/Anthropic)
internal/agent        M04 循环:Think-Act-Observe + 停止条件 + 错误自愈
internal/agents       内置角色 system prompt(compiler/query/qa)
internal/tools        M06 工具注册(状态/检索/读写文件/lint),全 Go 实现
internal/wiki         知识库操作(扫描/检索/lint),替代原 Python scripts
internal/cli          命令分发
```

`scripts/` 仅保留 pdf2md.sh(T1/T2 PDF 管道)与 export-public.sh(隐私导出)——两者依赖外部工具,保留 shell;Python 工具链已全部移植到 Go。设计见 `docs/harness设计.md`。

## 项目结构

```
knowledge-compounder/
├── SCHEMA.md            ← 编译纪律:页面格式约定(本项目的心智)
├── cmd/kcp + internal/  ← 自研 Go agent:provider/agent/agents/tools/wiki/cli
├── scripts/             ← 保留 shell:pdf2md.sh / export-public.sh(Python 已移植到 Go)
├── templates/           ← source/concept/entity/synthesis 页面模板
├── examples/            ← 合成演示:raw + 编译产物,展示纪律
├── context/             ← 支柱B:kcp mcp 把 wiki 暴露成 MCP server,喂给 coding/trading agents
└── docs/ 上手.md  运行原理.md  与RAG的区别.md  场景应用.md  循环设计.md  隐私模型.md  harness设计.md
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

MIT——自由使用、修改、商用,保留版权声明。见 [LICENSE](LICENSE)。

---

*开源方法论,私有 edge 留私库——编译流水线自动化,判断留给人。*
