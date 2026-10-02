"""M04 Agent 运行时:Think-Act-Observe 循环 + 工具执行 + 停止条件。

- 工具结果作为 Observation 回填(messages 追加式,记忆 = Messages)
- 错误三分类的自愈雏形:工具不存在/执行失败 → 转观察文本喂回模型
- 停止条件:MaxSteps + MaxSameAction(防原地打转);MaxTokens/Deadline 见 TODO
- 能力位:capabilities.tools=False 时降级 ReAct(见 loop_react)
"""

from __future__ import annotations

import json
from .providers import Provider, Message, ToolDef


class StopConditionError(RuntimeError):
    pass


class AgentRuntime:
    def __init__(self, provider: Provider, system_prompt: str, tools: list[ToolDef],
                 max_steps: int = 10, max_same_action: int = 3):
        self.provider = provider
        self.system_prompt = system_prompt
        self.tools = tools
        self.tools_by_name = {t.name: t for t in tools}
        self.max_steps = max_steps
        self.max_same_action = max_same_action

    def run(self, user_input: str, state: str | None = None) -> str:
        messages = [Message("system", self.system_prompt)]
        if state:
            messages.append(Message("user", f"[任务状态]\n{state}"))
        messages.append(Message("user", user_input))

        same_action: dict[str, int] = {}

        for step in range(self.max_steps):
            reply = self.provider.chat(messages, tools=self.tools)
            if not reply.tool_calls:
                return reply.content

            for tc in reply.tool_calls:
                tool = self.tools_by_name.get(tc["name"])
                try:
                    args = json.loads(tc["arguments"]) if tc["arguments"] else {}
                except json.JSONDecodeError:
                    args = {}
                if tool is None:
                    obs = f"工具不存在: {tc['name']}。可用: {', '.join(self.tools_by_name)}"
                else:
                    try:
                        obs = tool.func(**args)   # ← 工具结果 = Observation
                    except Exception as e:        # ← 错误自愈:喂回模型,不直接崩
                        obs = f"工具执行失败: {e}。请检查参数或换路径。"
                messages.append(Message("assistant", reply.content, tool_calls=[tc]))
                messages.append(Message("tool", obs, tool_call_id=tc["id"]))

                sig = f"{tc['name']}:{tc['arguments']}"
                same_action[sig] = same_action.get(sig, 0) + 1
                if same_action[sig] > self.max_same_action:
                    raise StopConditionError(
                        f"同一动作重复 {self.max_same_action} 次({sig}),判定原地打转,停止。")

        raise StopConditionError(f"达到最大步数 {self.max_steps},停止。")


def loop_react(provider: Provider, system_prompt: str, tools: dict[str, ToolDef],
               user_input: str, max_steps: int = 10) -> str:
    """ReAct 降级范式:无 function calling 时用纯文本约定(能力位 tools=False)。"""
    import re
    tool_desc = "\n".join(f"- {n}: {t.description}" for n, t in tools.items())
    sys = f"{system_prompt}\n\n可用工具:\n{tool_desc}\n\n每步输出:\nThought: ...\nAction: 工具名(JSON参数)\n然后停止,等 Observation。\n或直接输出 Final Answer: ..."
    messages = [Message("system", sys), Message("user", user_input)]
    for _ in range(max_steps):
        reply = provider.chat(messages, tools=None)
        text = reply.content
        if "Final Answer:" in text:
            return text.split("Final Answer:", 1)[1].strip()
        m = re.search(r"Action:\s*(\w+)\(([^)]*)\)", text)
        if not m:
            return text  # 模型没走工具格式,把它当最终答案
        name, raw_args = m.group(1), m.group(2)
        tool = tools.get(name)
        try:
            args = json.loads(raw_args) if raw_args.strip() else {}
            obs = tool.func(**args) if tool else f"工具不存在: {name}"
        except Exception as e:
            obs = f"工具执行失败: {e}"
        messages.append(Message("assistant", text))
        messages.append(Message("tool", f"Observation: {obs}", tool_call_id=name))
    return "达到最大步数,停止。"
