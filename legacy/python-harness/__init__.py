"""Knowledge Compounder 自研 harness —— 任意 LLM 下跑知识编译复利。

包结构:
  config.py    环境配置(provider/model/key)
  providers.py Provider 抽象 + 能力位(OpenAI 兼容/Anthropic)
  loop.py      M04 Agent 循环(Think-Act-Observe + 工具 + 停止条件)
  tools.py     把 scripts/ 注册成可调用工具
  agents.py    加载 .claude/agents/*.md → system prompt
  cli.py       python -m harness <role> "..." / status / lint / compile / query
"""
