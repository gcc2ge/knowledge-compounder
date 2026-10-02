---
name: observe
description: 观察捕获系统 — 将策略执行中的异常发现标准化写入 wiki 并编译为持久知识
---

# Observe Skill

## 概述

`observe` 是一个用户 CLI 工具（`scripts/observe.py`），用于快速记录策略执行中的异常发现，自动写入 `raw/observations/`，后续由 compiler 编译进 wiki。

用户已经配置了 shell 别名：`observe`、`obs-list`、`obs-compile`。

## 何时使用

在 Claude Code 会话中，当用户提到以下内容时，建议使用 observe：

- "我发现了 XX 很奇怪" / "有个异常" / "有个新模式"
- "这个创建者的参数组合不太对"
- "今天套利价差变了"
- 任何亏损/失败案例的讨论
- "亏了" / "又 rug 了"

**不要替用户运行 observe**（这是用户的 CLI 工具）。而是指出"这个值得用 observe 记下来"，并帮助用户组织观察内容。

## 观察格式

一条好的观察包含：

```
标题: [策略] 发现 [具体现象]
内容:
  - 发生了什么（链、地址、参数、时间）
  - 我的假设（这可能意味着什么）
  - 需要验证什么（下一步行动项）
```

## 观察类型

| 类型 | 说明 |
|------|------|
| anomaly | 异常——和预期不符 |
| pattern | 模式——重复出现的特征 |
| signal | 信号——可能有信息含量 |
| hypothesis | 假设——待验证的猜想 |
| failure | 失败——亏损或错误 |
| edge | 优势——发现的可利用特征 |

## 闭环流程

```
用户发现异常 → 建议 observe 捕获 → raw/observations/
     ↑                                      ↓
     └── 查 wiki ← compiler 编译 ← obs-list 筛选
```

## 编译时

当 `raw/observations/` 中有文件时，compiler 应将其视为 raw 源文件，编译为 `wiki/sources/` 中的页面，并更新相关概念页。

观察文件的前置 `compiled: false` 标记在编译完成后由用户手动运行 `obs-compile` 更新，或由 coordinator 运行 `python3 scripts/observe.py mark-compiled <path>` 更新。

## 相关概念

详见 [[wiki/concepts/观察驱动闭环.md]] 和 [[OBSERVE-CHEATSHEET.md]]。