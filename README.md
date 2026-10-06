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

## 编译产物:四种页面,一份员工手册

核心动作发生在**写的时候**:五篇文档讲同一个东西,先综合成一个概念页;两篇结论冲突,把冲突留下来;以前出过事故,把根因、修法、不能碰的地方沉淀下来。**Agent 真正干活时,拿到的不是五个碎片,而是一份已经整理过的员工手册。**

四种页面各司其职:

| 页面 | 装什么 | 怎么产生 |
|---|---|---|
| `sources/` 源页 | 每个 raw 文件的完整论证:一句话结论、论证链(代码/SQL/对比表逐字保留)、意外发现、疑点 | 每编译一个 raw 文件,1:1 生成 |
| `concepts/` 概念页 | **跨源综合**:多篇文档讲同一个东西,综合成一页;矛盾显式留在「张力与缺口」;置信度分层(单源=low,多源=high) | 编译时创建/更新,建不建新页由你裁决 |
| `entities/` 实体页 | 人物/工具/项目/组织的关键事实,每条带来源 | 被 2+ 个源提及时才建(单次提及留在源页「术语」节) |
| `synthesis/` 综合页 | 好查询的答案固化成永久分析:问题、证据、张力、结论 | `kcp query` 后 filed back |

举个例子:五篇文档都在讲「熔断器」——

- 编译后不再有五个碎片,而是**一个概念页**:定义、各家观点、适用边界已综合完毕
- 其中两篇结论冲突 → 冲突**不消失**,写进「张力与缺口」,agent 知道这里有争议、该多信哪边看置信度
- 你线上踩过熔断配置的坑 → `observe` 回灌,概念页「我的实践」多一条"根因 X、修法 Y、别碰 Z"
- 下次 agent 接到相关任务,检索命中的是这页手册——**入职培训已经替它做完了**

那 agent 检索时到底看哪种页?**不挑——相关性挑页,类型不挑。** `search_wiki`(MCP)把全部页面放进同一个索引,混合检索按相关性排序;每条结果带私有度徽标(`[A]` 公开 / `[B]` 精选综合 / `[C]` 私有踩坑),agent 按徽标判断该多信哪句,再用 `get_page` 深读。什么问题命中什么页,由问题的形状决定:

| agent 的问题 | 命中 | 为什么 |
|---|---|---|
| 「X 是什么 / 怎么做」 | concept | 跨源综合已完成,信息密度最高 |
| 「论证细节 / 原文那段 SQL」 | source | 论证链完整,代码/表格逐字保留 |
| 「怎么选 / 上次怎么处理的」 | synthesis | 问题-证据-张力-结论的决策文档 |
| 「谁提出的 / 用什么工具」 | entity | 交叉引用的跳板 |

**类型不是给 agent 选的菜单,是编译时埋好的分工**:写的时候决定知识放哪层、什么置信度;读的时候 agent 只管相关性排序 + 徽标——结构红利自动兑现。

复利就长在这三种时刻上:

1. **编译时**——每个新源交叉引用 5-15 个已有页:知识不是堆积,是连网
2. **查询时**——好答案 filed back 成 synthesis:每次提问都是对库的投资,不是消耗
3. **失败时**——踩坑 observe 回灌:同一个坑只亏一次,教训直接进手册

第一天 agent 拿到的是手册 v1;第一百天拿到的是被你的问题、你的事故、你的复盘喂过的手册 v100。

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

## 也在给人用:研究工作台,不是收藏夹

这套系统不止给 agent 当外脑——**你自己就是第一个用户**。传统云笔记(有道云/印象笔记/Notion 这类)是仓库:收藏、分类、按关键词搜原文,知识躺着不动,AI 功能止于"帮你总结这篇"。kcp 把研究过程本身变成工作台:

| | 传统云笔记 | kcp |
|---|---|---|
| 笔记之间的关系 | 无——文件夹是柜子,笔记各自独立 | 交叉引用网络,每页至少 2 个出站链接 |
| AI 的角色 | 一次性摘要,用完即弃 | **编译**:综合、连网、标注矛盾、回灌 |
| 两篇文章冲突 | 并排躺着,你不知道 | 显式写进「张力与缺口」,置信度分层 |
| 找东西 | 关键词匹配,找到的是原文 | `kcp query` 跨页综合出带引用的答案 |
| 收藏的后果 | 越收越多,从不回头看(收藏夹吃灰) | 不综合进不了库——**读过不等于拥有,编译过才连网** |
| 数据 | 锁在他们的云里 | 纯文本 + git,属于你,可导出带走 |

研究者的三个时刻,各有一个对应机制:

1. **读的时候**——编译逼你综合:新文章必须连接 5-15 个已有页、写出"这让我想到什么"。纯搬运产生不了 edge,你的联想才是私有价值
2. **想的时候**——`kcp query "知识库里对 X 有什么结论?"`:它替你拼出多篇来源的综合答案,带引用、带置信度;好问题的答案 filed back 成 synthesis,**研究过程本身产出资产**
3. **发现张力的时候**——新读的东西和已有认知冲突?冲突不消失,进「张力与缺口」。研究恰恰是追踪矛盾的过程;传统笔记里矛盾只是两篇并排躺着的文章

一句话:云笔记让你**存得更整齐**,kcp 让你**想得更深入**——存的是原文,长的是你自己的知识网络。

## 快速上手

> 要求:Go 1.24+。纯 Go、零第三方依赖,`go build` 一个二进制搞定。

```bash
# 1. 克隆模板,进入项目,编译自研 agent
git clone https://github.com/gcc2ge/knowledge-compounder.git && cd knowledge-compounder
go build -o kcp ./cmd/kcp

# 2. 先验证安装(以下全部零 LLM,不需要任何 API key)
./kcp status                    # 知识库状态 + 未编译 raw(内置 demo:1 源已编译)
./kcp lint                      # 健康检查(断链/孤儿/格式)
./kcp search "go channel 泄漏"  # 混合检索诊断(词法+向量,带 [A]/[B]/[C] 私有度徽标)

# 3. 配置模型(任意 LLM:DeepSeek/Ollama/智谱 GLM/OpenRouter…)
export KCP_PROVIDER=openai-compatible   # 或 anthropic
export KCP_MODEL=deepseek-chat          # 换成你的模型(本地 ollama 可留空 key)
export KCP_API_KEY=sk-xxx
export KCP_BASE_URL=https://api.deepseek.com/v1   # ⚠ 必须与你选的模型一致,且必须带 /v1
#  ⚠ kcp 在 BaseURL 后直接拼 /chat/completions——BaseURL 必须带提供商的 API 版本路径
#  (通常 /v1),否则请求打到站点首页/根路由,返回 HTML 而非 JSON,compile 静默空转
#  (实测:BaseURL 漏 /v1 时模型全程未参与、源页零产出,靠 exit code 已拦为失败码)。
#   DeepSeek → https://api.deepseek.com/v1
#   Ollama   → http://localhost:11434/v1
#   智谱 GLM → https://open.bigmodel.cn/api/paas/v4
#   OpenRouter→ https://openrouter.ai/api/v1
#   其他服务商(含中转):以该服务商文档为准,拼到 /chat/completions 能返回 JSON 即对。

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
export KCP_BASE_URL=https://api.deepseek.com/v1   # 与模型对应,必须带 /v1(见上方快速上手)
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
internal/tools        M06 工具注册(状态/检索/mentions/反链/读写文件/lint),全 Go 实现
internal/index        内存倒排索引+快照:检索/计数/图查询零全库扫(716 页实测单次检索 307ms→6.6ms)
internal/wiki         知识库操作(扫描/检索/lint),替代原 Python scripts
internal/mcp          支柱 B:MCP stdio server(纯标准库实现协议,零 SDK 依赖)
internal/cli          命令分发
```

工具层遵循一条纪律:**能确定性解决的绝不交给概率**。「术语被几个源提及」这类建页判据由 `wiki_mentions` 直接计数返回结论(0/1/≥2 三档话术),LLM 不做模糊搜索去数文件;语义判断才留给模型。索引层用标准库实现(倒排 + gob 字典编码快照 + mtime 增量同步),第三方依赖数保持为零;`KCP_INDEX=off` 一键降级回全扫路径。

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
