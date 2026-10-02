"""CLI 入口:任意 LLM 下跑知识编译复利。

用法:
  python -m harness <role> "<用户输入>"    # 跑一个 agent(compiler/qa/coordinator...)
  python -m harness status                  # 等价旧 Claude Code SessionStart hook
  python -m harness compile <raw文件>      # 单源编译(compiler agent)
  python -m harness query "问题"           # 查询 + 综合
  python -m harness lint                    # 健康检查
  python -m harness observe -s <strategy> -c "内容"
  python -m harness list                    # 列出可用 agent 角色
"""

from __future__ import annotations

import sys

from . import config
from .agents import list_agents, load_agent
from .loop import AgentRuntime, loop_react
from .providers import make_provider
from .tools import build_tools


def run_role(role: str, user_input: str, state: str | None = None) -> str:
    agent = load_agent(role)
    provider = make_provider()
    tools = build_tools()
    if provider.capabilities.tools:
        rt = AgentRuntime(provider, agent["system_prompt"], tools,
                          max_steps=config.MAX_STEPS, max_same_action=config.MAX_SAME_ACTION)
        return rt.run(user_input, state=state)
    return loop_react(provider, agent["system_prompt"], {t.name: t for t in tools}, user_input)


def cmd_status() -> str:
    from .tools import wiki_status
    return wiki_status()


def cmd_compile(raw_file: str) -> str:
    content = None
    try:
        with open(raw_file, encoding="utf-8") as f:
            content = f.read()
    except OSError as e:
        return f"读取失败: {e}"
    prompt = f"编译以下 raw 源,按 SCHEMA 生成 source 页(论证链保留全部代码/表/示例),并检查交叉引用:\n\n{content[:20000]}"
    return run_role("compiler", prompt, state=f"待编译文件: {raw_file}")


def cmd_query(question: str) -> str:
    return run_role("coordinator", question)


def cmd_lint() -> str:
    from .tools import run_lint
    return run_lint()


def cmd_observe(args: list[str]) -> str:
    from .tools import run_script
    return run_script("observe", " ".join(args))


def main(argv: list[str] | None = None) -> int:
    argv = argv if argv is not None else sys.argv[1:]
    if not argv:
        print("用法见 harness/README.md。可用 agent:", ", ".join(list_agents()))
        return 1
    cmd = argv[0]
    if cmd == "list":
        print("\n".join(list_agents())); return 0
    if cmd == "status":
        print(cmd_status()); return 0
    if cmd == "lint":
        print(cmd_lint()); return 0
    if cmd == "observe":
        print(cmd_observe(argv[1:])); return 0
    if cmd == "compile":
        if len(argv) < 2:
            print("用法: python -m harness compile <raw文件>"); return 1
        print(cmd_compile(argv[1])); return 0
    if cmd == "query":
        if len(argv) < 2:
            print("用法: python -m harness query <问题>"); return 1
        print(cmd_query(" ".join(argv[1:]))); return 0
    # 兜底:把第一个参数当 role
    if cmd in list_agents():
        print(run_role(cmd, " ".join(argv[1:]) if len(argv) > 1 else ""))
        return 0
    print(f"未知命令/agent: {cmd}(可用 agent: {', '.join(list_agents())})")
    return 1


if __name__ == "__main__":
    sys.exit(main())
