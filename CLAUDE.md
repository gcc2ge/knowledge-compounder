# knowledge-compounder

知识编译复利引擎:把 raw 原文编译成可复利的 wiki 知识资产(Go 实现,CLI `kcp` + MCP server)。

## 代码质量工作流

- 提交前:对工作区 diff 跑 `/code-review`(medium),修复确认的正确性问题后才 commit。
- 需要清理刚写的改动时:跑 `/simplify`(或对 code-review 的清理项用 `--fix`)。
- 简化/审查只动未提交的 diff;commit 后没有 diff 可审,先跑再交。
