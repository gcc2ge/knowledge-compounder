"""Agent 指令加载:解析 .claude/agents/*.md → (name, system_prompt)。

frontmatter(name/description) 用于注册;正文直接作为 system prompt——
同一份编译纪律指令,从 Claude Code 换成任意 LLM,方法论不变。
"""

from __future__ import annotations

import re
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
AGENTS_DIR = ROOT / ".claude" / "agents"


def parse_agent_md(path: Path) -> dict:
    text = path.read_text(encoding="utf-8", errors="ignore")
    frontmatter: dict = {}
    body = text
    if text.startswith("---"):
        m = re.match(r"^---\n(.*?)\n---\n(.*)$", text, re.S)
        if m:
            for line in m.group(1).splitlines():
                if ":" in line:
                    k, v = line.split(":", 1)
                    frontmatter[k.strip()] = v.strip().strip('"')
            body = m.group(2)
    return {"name": frontmatter.get("name", path.stem),
            "description": frontmatter.get("description", ""),
            "system_prompt": body}


def list_agents() -> list[str]:
    return sorted(p.stem for p in AGENTS_DIR.glob("*.md"))


def load_agent(role: str) -> dict:
    path = AGENTS_DIR / f"{role}.md"
    if not path.exists():
        raise FileNotFoundError(f"agent 不存在: {role}(可用: {list_agents()})")
    return parse_agent_md(path)
