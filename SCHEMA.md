# SCHEMA — 知识编译纪律

> 这是 Knowledge Compounder 的"编译契约"——规定 raw 如何变成 wiki。`wiki/` 层完全由本文件驱动;compiler agent 按此编译,qa agent 按此验收,coordinator 按此编排。
>
> 心智:知识**只编译一次,然后持续维护**。`raw/` = 源代码(不可变),`wiki/` = 编译产物,lint = 测试,query = 运行时。

## 三层架构

```
Layer 3: SCHEMA(本文件)      ← 指导编译行为
Layer 2: WIKI(wiki/ 目录)     ← 编译自
Layer 1: RAW SOURCES(raw/)    ← 不可变,事实来源(source of truth)
```

- `raw/` 不可变。永不修改 raw 中的任何文件。`raw/assets/` 存本地图片附件,`raw/books/` 存转换入库的书籍 md。
- `wiki/` 是编译产物,自由创建/修改/删除。子目录:`sources/`(每 raw 源一个摘要页,1:1)、`concepts/`(跨源综合)、`entities/`(人物/工具/项目/组织)、`synthesis/`(比较分析综合文档)、`health/`(lint 健康报告)、`notes/`(低置信度工作笔记)。
- 页面计数与未编译源统一用 `wiki_status` 工具的机器口径。

## 三个核心操作

1. **Ingest(编译)**:读 raw 全文 → 判断 origin → 编译进 wiki(sources/concepts/entities)→ 检查与已有页面的**矛盾**(有则显式标注并反映到「张力与缺口」)→ **佐证回流**:新源佐证既有概念页时用 `update_evidence` 追加源、机器重算 confidence——既有页每次编译都物理变强(正向复利,不只写新源页「连接」)→ 加 `[[wikilinks]]` → 返回变更清单。
2. **Query(查询 + 知识复利)**:定位相关页 → 读取 → 合成带引用答案 → **跨页综合答案用 `filed_back` 落盘 `wiki/synthesis/`**(query-as-contribution 的机械执行:≥2 支撑源、未决问题非空、同名不覆盖)。每次好提问都应让 wiki 更丰富。
3. **Lint(健康检查)**:断链 / 孤儿 / 矛盾 / 过时(`review_after` 优先)/ 覆盖(未编译 raw)/ 格式 / 知识网络(单源概念、单向链接、slug 冲突)/ **私有 edge 检查**(「意外发现」节无联想信号词则标弱——私有价值是复利载体)/ **疑似同义概念页**(命名漂移检测)。健康报告落盘 `wiki/health/` 并与上次比较(趋势行)。

## Origin 分流(决定概念页内容路由)

- `origin: external` —— raw/ 中他人撰写的资料(文章/论文/转载)→ 进概念页「外部观点」
- `origin: self` —— 自己的实践产物(wiki/synthesis、wiki/notes、自己的设计文档)→ 进概念页「我的实践」

概念页 frontmatter `sources` 只列 external 源摘要;self 来源通过正文「我的实践」的 `[[wikilinks]]` 引用体现,不挤占数组。

## 页面格式

### 源摘要页(`wiki/sources/`)

```markdown
---
source_files: [raw/文件1.md, raw/文件2.md]
origin: external|self
compiled: YYYY-MM-DD
type: source
tags: [tag1, tag2]
---
# 源标题

## 一句话结论        ← 不读 raw 也能带走结论
## 论证链            ← 按作者论证顺序,每步附关键引用
## 关键细节          ← 数字/参数/阈值/API 字段/协议细节,不放论证
## 作者立场与定位    ← 作者视角、假设、适用边界、局限性
## 意外发现          ← 与预期不符的 + "这让我联想到什么/在用户场景意味着什么"
## 疑点              ← 未验证主张。不能留空(支撑充分则写"未发现重大疑点")
## 术语              ← 单次提及的概念/实体在此落地,不单独建页
## 连接              ← [[wikilinks]] + 必写关联意义
## 引用
```

> 设计原则:source 页是**自包含的知识单元**——读者不看 raw 也能理解完整论证,包括所有 SQL、代码、表格、示例和运行结果。编译后逐条核验:每个 SQL 块 / 代码块 / 对比表 / 关键示例是否都在论证链对应位置出现;缺则补。

### 概念页(`wiki/concepts/`)

```markdown
---
type: concept
created: YYYY-MM-DD
updated: YYYY-MM-DD
sources: [source1, source2]
confidence: high|medium|low
---
# 概念名
## 定义
## 关键方面
## 我的实践        ← self 来源;暂无写"待 synthesis 回灌"
## 外部观点        ← external 来源,多源综合并标注来源
## 张力与缺口      ← 实践 vs 外部观点的矛盾、未验证点
## 例子            ← 1-3 个具体例子
## 相关            ← wikilinks
```

### 实体页(`wiki/entities/`)

```markdown
---
type: entity
entity_type: person|tool|project|organization
created: YYYY-MM-DD
updated: YYYY-MM-DD
---
# 实体名
## 概述
## 关键事实   ← 每条带来源 [[source]]
## 相关
```

> **创建阈值**:实体页只在被 **2+ 个源提及**时创建;单次提及留 source 页「术语」节。

### 综合文档(`wiki/synthesis/`)

```markdown
---
type: synthesis
trigger: query|lint|manual
created: YYYY-MM-DD
question: "触发这个分析的问题"
---
# 分析标题
## 一句话摘要
## 问题
## 分析
## 证据与张力   ← 反例/佐证/具体例子,附 [[wikilink]];每条关键证据标私有度 A=公开 / B=精选综合 / C=私有数据
## 未决问题     ← 不能留空
## 结论
## 来源
```

### Index(`wiki/index.md`)与 Log(`log.md`)

- index:总览(页面计数)+ Sources(日期倒序表)+ Concepts + Entities + Synthesis 表;由 coordinator 串行维护。
- log:append-only,每条 `## [YYYY-MM-DD HH:MM] 操作 | 主题`,含操作/描述/涉及页面。coordinator 串行追加,compiler 不并发写。

## 质量规则(不可妥协)

1. **不做全文摘要**——提取关键信号,不是压缩原文
2. **标注来源**——每个事实声明可追溯到具体 raw 文件
3. **标注矛盾**——新旧源冲突时两边都保留并显式标注
4. **标注置信度**——单源 = low,多源验证 = high
5. **Wikilinks 为先**——每页至少 2 个出站链接
6. **人策展 > 自动更新**——创建新概念页前先问用户是否值得
7. **意外优先**——每个源摘要必须有「意外发现」
8. **origin 必填**——lint 校验
9. **单次提及不建页**——只被一个源提及的概念/实体进该 source 的「术语」节
10. **每次编译都是私有化机会**——每个 source 在「意外发现」「连接」中至少写一行你自己的分析。"纯搬运不产生 edge;你写的连接和批判是私有价值所在。"

## 语言约定

默认中文,英文仅用于已有共识的专有名词。中文文件名用简短表意词(不用拼音);外国人名/英文品牌保留英文 slug(如 `andrej-karpathy.md`、`claude-code.md`)。标签优先中文,纯英文专有术语保留。

## 隐私分层

- 页面级:`public: false` 整页不进公开版
- 段落级:`<!-- PRIVATE --> ... <!-- /PRIVATE -->` 导出时剥离
- 替换规则(导出脚本自动执行):具体薪资 → [薪资信息]、股权比例 → [股权信息]、本地文件路径 → [本地路径]
- 导出:`bash scripts/export-public.sh` → 输出到 wiki-public/

## 协作纪律

- 共享文件 `wiki/index.md`、`log.md`、健康报告由 coordinator 串行维护;compiler/batch-compiler 并发写各自页面但不碰共享文件。
- 每次 ingest/batch/update 的 Git 闭环:raw 入库 → 编译 → index → log → `wiki_status` → lint 落盘 → 用户确认后 commit。
