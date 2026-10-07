# Wiki Index

## 概要
- 总页面数:58
- 源文件数:13
- 最后更新:2026-10-07

## Sources(按文件名排序)
| 文件 | 源 | 核心收获 | 日期 | 标签 |
|------|-----|------|------|------|
| [[M01 Go 语言 AI 开发基础]] | M01 Go 语言 AI 开发基础.md | AI Agent 开发落到代码层,依赖一套可复用的 Go 工程能力:标准 `internal/cmd` 分层、goroutine+channel 建模 LLM 流式输出、context 贯穿超时/取消/请求级元数据、小接口 + 泛型抽象 P… | 2026-10-07 | go,ai-agent,llm-api,concurrency,context,http-client |
| [[M02 LLM 全平台接入]] | M02 LLM 全平台接入.md | LLM 调用层是 Agent 系统一切能力的地基:用统一 `llm.Provider` 接口(Chat/ChatStream/Capabilities)把协议差异收敛到 Provider 内部——OpenAI 兼容协议家族(OpenAI/D… | 2026-10-07 | go,ai-agent,llm-api,provider,sse,json-schema,router,token-cost |
| [[M03 Prompt 与上下文工程基础]] | M03 Prompt 与上下文工程基础.md | 与模型对话的本质不是「发一段文字」而是「发送一个消息列表」:用 System/User/Assistant/Tool 四类消息角色组织对话,用清晰指令 + few-shot 示例 + 结构化输出约束模型行为,并建立「上下文不是仓库而是工作台… | 2026-10-07 | prompt,context-engineering,token-budget,prompt-caching,message-roles,few-shot,ai-agent,go |
| [[M04 Agent 核心架构]] | M04 Agent 核心架构.md | Agent 是「LLM 决策器 + Tools 感知行动接口 + State + Loop 自主循环 + Stop 停止条件」五件套缺一不可的程序结构:模型在循环里持续决定下一步(是否调工具、如何处理结果、何时结束),代码只负责提供工具、状… | 2026-10-07 | agent,react,function-calling,state-machine,stop-condition,plan-and-execute,event-stream,persistence,go,ai-agent |
| [[M05 Agent 设计模式]] | M05 Agent 设计模式.md | 在 M04 的单个 Agent 循环之上,把多次 LLM 调用、多个 Agent 或多个工具按结构组织起来,可以换取更高的可靠性、质量或速度——这些组织结构沉淀为五种核心模式:Prompt Chaining(固定几步线性串联)、Routin… | 2026-10-07 | agent,design-patterns,prompt-chaining,routing,parallelization,evaluator-optimizer,orchestrator-workers,three-agent,go,ai-agent |
| [[M06 工具系统、MCP 与 Skills]] | M06 工具系统、MCP 与 Skills.md | 工具系统是 Agent 的「耳目手脚」:用泛型 `TypedTool[T]` 把「定义工具」简化为「写一个类型化 Go 函数」,用中立抽象 + provider 边界映射补齐 Function Calling 三家协议差异,用路径围栏/NL… | 2026-10-07 | 工具系统,MCP,Function Calling,Skills,类型安全工具,工具安全,Go |
| [[M07 记忆系统与 Agentic RAG]] | M07 记忆系统与 Agentic RAG.md | 让 Agent 用上私有知识 = 建立「四种记忆」框架(工作/情节/语义/程序),再用语义记忆的主线实现整套 RAG 技术栈(切分→向量化→存储→混合检索+rerank→Agentic RAG);生产基线是「BM25+向量双路 RRF 融合… | 2026-10-07 | RAG,记忆,Embedding,向量数据库,Agentic RAG,Go |
| [[M08 多智能体系统]] | M08 多智能体系统.md | 多智能体系统(Multi-Agent System)是多个 Agent 作为**独立参与者并发运行、通过消息协作**的系统——与 M05 单进程内的函数编排有本质区别:每个 Agent 拥有自己的角色、上下文和决策循环(独立性),可通过 g… | 2026-10-07 | multi-agent,message-bus,supervisor,orchestrator,pipeline,debate,swarm,go,ai-agent |
| [[M09 Context Engineering（上下文工程）]] | M09 Context Engineering（上下文工程）.md | 上下文是有限的注意力预算(≠ token 物理空间),上下文膨胀有四大来源(对话历史/工具结果/工具定义/检索片段),治理总纲是「让常驻上下文只保留此刻必需的、结论性信息,其余压缩、外置、按需取回」——通过 Token 预算门控、历史压缩(… | 2026-10-07 | context-engineering,token-budget,compaction,tool-exposure,prompt-caching,agent-harness |
| [[M10 可观测性、评估与安全]] | M10 可观测性、评估与安全.md | Agent 的调试与改进依赖三件套:用 OpenTelemetry GenAI 语义约定把 Agent 循环埋成 trace(可观测,还原「发生了什么」);用三层评估架构(确定性检查 → 轻量判官 → 完整判官)加 Agent-as-a-J… | 2026-10-07 | observability,evaluation,security,opentelemetry,prompt-injection,rate-limiting,judge,go,ai-agent |
| [[M11 Eino 框架]] | M11 Eino 框架.md | Eino 是 CloudWeGo 推出的 Go LLM 应用开发框架,以「组件层(原子能力)→ 编排层(Chain/Graph/Workflow/Flow)→ Agent 层(ADK)→ DevOps 层(横切治理)」四层单向依赖组织应用:… | 2026-10-07 | eino,go,agent框架,编排,adk,rag,milvus,devops |
| [[M12 Google ADK for Go 与 A2A 协议]] | M12 Google ADK for Go 与 A2A 协议.md | 当 Agent 协作跨越团队、语言和机器(物流 Agent 由别的团队维护、风控 Agent 用 Java 写、数据分析 Agent 部署在独立集群)时,进程内 Graph/Compose 无法直接连接远程 Agent,需要 A2A(Age… | 2026-10-07 | A2A,ADK-Go,agent-communication,课程笔记,MCP |
| [[go-channel-三铁律]] | go-channel-三铁律.md | Go channel 的 goroutine 泄漏源于"发送阻塞而接收方已退出";三铁律——谁创建谁关闭、发送时监听取消、错误与结果同通道——配合 `defer close`,系统性地消除泄漏。 | 2025-01-01 | go,concurrency,channel,goroutine-leak |

## Concepts
| 文件 | 描述 | 置信度 | 源数量 |
|------|------|--------|--------|
| [[A2A]] | A2A(Agent2Agent)是开放、厂商中立的 Agent 互操作协议,让不同框架、语言和供应商构建的 Agent 发现彼此、交换消息并管理协作任务。Google 于 2025 年 4 月公开,2025 年 6 月移交 Linux Fo… | high | 4 |
| [[Agent 五要素]] | Agent 五要素 = **LLM(决策器)+ Tools(感知与行动接口)+ State(状态)+ Loop(自主循环)+ Stop(停止条件)**,是 M04 给出的 Agent 形式化定义——「五部分缺一不可,缺任一部分就退化成不同形… | high | 6 |
| [[AgentCard]] | AgentCard 是 A2A(Agent2Agent)协议的发现与身份声明文档:一个 JSON 文档,由 Remote Agent 在标准路径 `GET /.well-known/agent-card.json` 暴露,声明其身份、接口、… | medium | 2 |
| [[AgentEvent]] |  |  | 0 |
| [[Checkpoint]] | Checkpoint(检查点)是 Agent 执行状态在某个时刻的持久化快照:把「运行位置、状态变量、待处理动作」序列化到外部存储,中断后可从该位置恢复继续执行——是把 Agent 从「内存中的一次性运行」升级为「可跨请求、跨进程、可审计的… | medium | 3 |
| [[Eino ADK]] | Eino ADK 是字节 CloudWeGo Eino 框架中的 Agent 层(Agent Development Kit 子模块),把「外层 Agent 选择能力,内层 Graph/Workflow 可靠执行」的落地范式封装成可复用组件… | medium | 2 |
| [[Function Calling]] | Function Calling(也称 Tool Use)是模型厂商在 API 层面提供的结构化工具调用能力:请求里传入工具定义和参数 Schema,模型在专门的结构化字段里返回工具调用(`tool_calls`),代码直接读取并执行,再把… | high | 6 |
| [[Langfuse]] |  |  | 0 |
| [[Lost in the Middle]] | Lost in the Middle 是长上下文问题:当输入上下文中部存在相关信息时,模型倾向于**忽略中间位置的信息**,而更关注开头和结尾的内容(即「两头好、中间差」)。它是单 Agent 上下文容量有限的典型症状之一——M08 原文:… | medium | 3 |
| [[MCP]] | MCP 是一个开放协议,把「让模型使用外部工具」从每个平台私有 API 变成统一的标准接口,是 AI 世界的「USB 接口」:外部工具、数据源和业务系统各自实现一次 MCP Server,任意 MCP Client(Agent、IDE、CL… | medium | 2 |
| [[Plan-and-Execute]] |  |  | 0 |
| [[Prompt Caching]] | 提示词缓存(Prompt Caching):把上下文中每次调用都差不多的大块内容(System 提示词、工具定义、结构化输出 Schema、few-shot 示例、稳定资料摘要)作为重复前缀,让这些重复前缀在后续请求中更快、更便宜地被处理。… | medium | 3 |
| [[Provider 接口]] | LLM 供应商的统一抽象:上层业务只依赖「能不能 Chat、能不能流式、支持哪些能力」,不关心某家把 system 放哪、请求头怎么拼、响应是什么结构。接口刻意保持小——先有 `Chat`/`ChatStream` 两个方法开局,工具调用、… | medium | 2 |
| [[RAG]] | 检索增强生成(Retrieval-Augmented Generation,RAG):把模型从「只靠参数知识回答」扩展到「看着资料、调用工具回答」——先检索相关知识片段,再作为上下文的一部分喂给模型,让它基于资料作答而不是凭空生成。 | high | 4 |
| [[RRF]] |  |  | 0 |
| [[ReAct]] | ReAct(Reasoning + Acting)是让语言模型在推理与行动之间交替的工具调用范式:模型输出由 `Thought`(推理)、`Action`(选择工具与参数)、`Observation`(观察工具结果)构成的文本序列,循环往复… | medium | 2 |
| [[Reflection]] | Reflection(反思模式,也称 Evaluator-Optimizer)解决「一次生成能用,但质量不够好」的问题:先生成内容,再评估,再根据评估反馈修改——生成→评估→(不达标则带反馈重生成)的循环,至多 `maxRounds` 轮。… | high | 2 |
| [[Rerank]] |  |  | 0 |
| [[SSE]] | Server-Sent Events(SSE)——LLM 流式输出的传输协议:HTTP 响应以 `text/event-stream` 形式逐行推送 `data: {...}` 事件,以空行分隔,OpenAI 风格通常用 `data: [D… | medium | 2 |
| [[StreamChunk]] | 流式输出的最小单元——LLM 流式响应里一个增量块的内容与错误状态的统一载体。约定 `Content` 与 `Err` 互斥:`Err != nil` 时这是最后一个有意义的块。消费者用 `for range` 读取,channel 关闭后… | medium | 2 |
| [[Swarm]] | Swarm 是多智能体系统中**去中心化的控制权转交**模式:系统没有固定主管,每个 Agent 处理当前请求后自行决定——给出最终答案,还是通过 **handoff(控制权转交)** 把请求转交给另一个 Agent。每个 Agent 只负… | medium | 2 |
| [[Token 预算]] | Token 预算(Token Budget):给一次模型调用里上下文的每一部分划定 token 上限——`Budget{Total, SystemPrompt, Tools, History, Retrieved}`——把「上下文是有限注意… | high | 6 |
| [[context 取消]] | Go context 的取消语义:通过 `WithCancel`/`WithTimeout`/`WithDeadline` 从父 ctx 派生子 ctx,父 ctx 取消/超时时子 ctx 立即收到取消信号(`ctx.Done()` 关闭、… | medium | 3 |
| [[llm.Message]] | llm.Message 是《AI Agent 开发实战》课程统一请求结构中的消息类型:每条消息有 `Role` 和 `Content` 两个字段,`Role` 是 `system | user | assistant | tool` 四类取… | medium | 3 |
| [[schema.Generate]] | `schema.Generate(v any) *Schema`:用反射(Go reflect)从任意结构体自动生成 JSON Schema——遍历字段、按 kind 映射(string→"string", struct→"object")… | high | 4 |
| [[上下文隔离]] | 上下文隔离(Context Isolation)是让每个 Agent 只在**自己的上下文窗口**内工作、不把其他 Agent 的消息/工具结果/中间输出灌进自己上下文的工程约束——多智能体系统控制上下文膨胀的核心手段。M08 原文把它表述… | medium | 3 |
| [[动态工具暴露]] | 动态工具暴露(Dynamic Tool Exposure)是治理工具定义膨胀的手段:运行期只把与当前问题相关的工具提供给模型,而不是把所有工具定义一次性全塞进上下文。每个工具的名称、描述和参数 Schema 都会占用上下文——一个大 MCP… | high | 4 |
| [[历史压缩]] | 历史压缩(Compaction)是长会话治理的核心手段:当对话历史逼近上下文预算时,把较早内容高保真地总结为一条摘要,用「摘要 + 最近几轮原文」构造精简上下文,使任务继续运行。对话历史是 Agent 最持续的膨胀源——每轮都会追加模型输出… | high | 6 |
| [[多 Agent]] | 多 Agent 系统是多个独立 Agent 协作完成任务的程序结构:每个 Agent 拥有独立上下文(甚至独立工具集与职责),通过某种拓扑(如 Pipeline、Swarm/Handoff、Orchestrator+子代理、Debate/C… | high | 6 |
| [[子 Agent]] | 「把一段复杂任务隔离给另一个 Agent 做」的能力单元——与工具(原子动作)、MCP(连接协议)、Skills(流程配方)并列的第四种能力边界(M06 6.8)。子 Agent 拥有独立上下文(甚至独立工具集与职责),处理一大块带独立上下… | high | 4 |
| [[工作流]] | 工作流是确定性编排:LLM 调用和工具按预先写定的代码路径执行,每一步走向哪里由代码决定,模型只负责完成某个步骤中的内容。与智能体(Agent)相对的核心判据只有一个问题——**下一步做什么,是谁决定的**:路径写死在代码里就是工作流,模型… | medium | 3 |
| [[工具三要素]] | 工具三要素(工具定义三要素)= **Name(工具名)+ Description(功能描述)+ Parameters(参数 JSON Schema)**,是 Agent 系统中把「模型能调用的工具」描述给模型的最小信息集。模型依靠这三要素理… | high | 5 |
| [[注意力预算]] | 注意力预算(Attention Budget)是模型**有效消化能力**的上限——与 Token 预算(上下文窗口的物理空间)相对。Token 只是空间,注意力才是有效空间;大模型上下文的有效空间远小于物理空间,这是「上下文窗口可以扩,但注… | medium | 3 |
| [[混合检索]] |  |  | 0 |
| [[纵深防御]] | 纵深防御(Defense in Depth)是安全控制的分层策略:不依赖单一防御机制,而是在输入、模型、工具、输出、运行时等相互独立的层上建立控制,使单层失效时仍有其他边界限制损失。对 Prompt Injection 类攻击,不可能靠单一… | medium | 2 |
| [[结构化笔记]] | 结构化笔记(structured note-taking)是 Agent 的外部「工作笔记」:不要求把所有状态保留在对话历史中,而是把当前目标、已确认事实和待办事项写入外部结构化记录;新一轮对话或 Compaction 完成后,通过读取笔记… | medium | 3 |
| [[记忆系统]] | 记忆系统(Memory System):Agent 区别于一次性问答的根本能力——LLM 本身完全无状态,每次 API 调用孤立,但 Agent 任务根本上是有状态的(对话历史/用户偏好/过去交互/业务知识);记忆系统就是在这层「模型无状态… | high | 4 |
| [[限流与配额]] | 限流(rate limiting)与配额(quota)是 Agent 上线前的基础成本与滥用控制能力:限流限制每个用户/租户的请求频率(令牌桶),配额限制每个用户/租户的累计 token 消耗。二者互补——仅限制请求频率无法覆盖「低频但超长… | medium | 3 |

## Entities
| 文件 | 类型 | 描述 |
|------|------|------|
| [[ADK-Go]] | tool | ADK-Go(Google ADK for Go)是 Google 官方维护的开源 Go Agent 开发框架,code-first、模块化,用于构建、评估和部署 Agent;支持 Gemini,也允许接入其他模型。官方提供 Google … |
| [[Eino]] | tool | Eino 是字节跳动 CloudWeGo 社区推出的 Go LLM 应用开发框架,以「组件层(原子能力)→ 编排层(Chain/Graph/Workflow/Flow)→ Agent 层(ADK)→ DevOps 层(横切治理)」四层单向依… |
| [[Milvus]] | tool | Milvus 是 Zilliz 开源的云原生向量数据库(Go/C++ 实现,字节跳动系),支持十亿级向量规模、HNSW/IVF/DiskANN 等 ANN 索引,并原生提供 **dense + sparse 混合检索(Hybrid Sear… |
| [[OpenTelemetry]] | project | OpenTelemetry(OTel)是 CNCF 旗下的开源可观测项目,提供 Traces、Metrics 和 Logs 三类主要信号,通过 OTLP(OpenTelemetry Protocol)协议把应用埋点传输到各类后端。对 Age… |
| [[RAGAS]] | tool | RAGAS 是面向 RAG 与 Agent 评估的开源 Python 库(不是 trace 存储平台):读取问题、回答、检索片段和参考答案,计算四类常见 RAG 指标——Faithfulness(忠实度)、Answer Relevancy(… |
| [[llmrouter]] | project | 《AI Agent 开发实战》课程 M02 章的配套练习产出的多模型路由问答工具:在 minicall(单 Provider、非流式)之上加统一 Provider 接口、Claude 适配器、路由网关(多模型 + 故障转移/降级)、反射生成… |
| [[minicall]] | project | 《AI Agent 开发实战》课程 M01 章的配套练习产出的命令行问答工具:读环境变量 `LLM_BASE_URL`/`LLM_API_KEY`/`LLM_MODEL`,从命令行参数拿问题,非流式调用 `/chat/completions… |

## Synthesis
| 文件 | 问题 | 日期 |
|------|------|------|
