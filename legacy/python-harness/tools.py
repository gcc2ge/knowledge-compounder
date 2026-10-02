"""工具注册:把 scripts/ 与文件读写暴露成可调用函数(M06 工具系统)。

注意:run_script 只允许白名单 scripts/ 内脚本,防任意命令。
"""

from __future__ import annotations

import json
import re
import subprocess
from pathlib import Path

from .providers import ToolDef

ROOT = Path(__file__).resolve().parent.parent
WIKI = ROOT / "wiki"
SUBDIRS = ("sources", "concepts", "entities", "synthesis")
ALLOWED_SCRIPTS = {
    "wiki-status": ["python3", "scripts/wiki-status.py", "--json"],
    "wiki-lint": ["python3", "scripts/wiki-lint.py"],
    "observe": ["python3", "scripts/observe.py"],
    "export-public": ["bash", "scripts/export-public.sh"],
}


def _run(cmd: list[str], timeout: int = 300) -> str:
    try:
        r = subprocess.run(cmd, cwd=ROOT, capture_output=True, text=True, timeout=timeout)
        return (r.stdout or "") + (r.stderr[-500:] if r.stderr else "")
    except subprocess.TimeoutExpired:
        return f"[超时 {timeout}s]"


def wiki_status() -> str:
    return _run(ALLOWED_SCRIPTS["wiki-status"])


def run_lint() -> str:
    return _run(ALLOWED_SCRIPTS["wiki-lint"])


def run_script(name: str, args: str = "") -> str:
    """安全执行白名单脚本:name ∈ wiki-status|wiki-lint|observe|export-public,args 追加。"""
    base = ALLOWED_SCRIPTS.get(name)
    if not base:
        return f"不允许的脚本: {name}(白名单: {list(ALLOWED_SCRIPTS)})"
    import shlex
    return _run(base + shlex.split(args))


def _pages() -> list[Path]:
    return [p for d in SUBDIRS for p in (WIKI / d).glob("*.md")]


def search_wiki(query: str, k: int = 5) -> str:
    terms = [t for t in re.split(r"\s+", query.lower()) if t]
    scored = []
    for p in _pages():
        text = p.read_text(encoding="utf-8", errors="ignore")
        score = sum(3 if t in p.stem.lower() else 0 for t in terms)
        score += sum(text.lower().count(t) for t in terms)
        if score:
            m = re.search(r"## (?:一句话结论|定义|概述|问题)\s*\n([^\n]+)", text)
            scored.append((score, p, m.group(1).strip() if m else ""))
    scored.sort(key=lambda x: -x[0])
    if not scored:
        return "未找到匹配页面。"
    return "\n".join(f"- [[{p.stem}]] ({s}): {summ}" for s, p, summ in scored[:k])


def get_page(page: str) -> str:
    slug = page.removesuffix(".md")
    for d in SUBDIRS:
        p = WIKI / d / f"{slug}.md"
        if p.exists():
            return f"## {slug} ({d}/)\n\n{p.read_text(encoding='utf-8', errors='ignore')}"
    return f"[[{slug}]] 不存在。"


def read_file(path: str) -> str:
    """读取项目内文件(相对项目根)。"""
    fp = (ROOT / path).resolve()
    if not str(fp).startswith(str(ROOT)):
        return "拒绝:路径超出项目根。"
    try:
        return fp.read_text(encoding="utf-8", errors="ignore")
    except OSError as e:
        return f"读取失败: {e}"


def write_file(path: str, content: str) -> str:
    """写文件(相对项目根)。只允许写 wiki/ 或 raw/ 内。"""
    fp = (ROOT / path).resolve()
    if not str(fp).startswith(str(ROOT)):
        return "拒绝:路径超出项目根。"
    rel = fp.relative_to(ROOT)
    if not (rel.parts[0] in ("wiki", "raw", "examples")):
        return f"拒绝:只允许写 wiki/ raw/ examples/,收到 {rel}。"
    try:
        fp.parent.mkdir(parents=True, exist_ok=True)
        fp.write_text(content, encoding="utf-8")
        return f"已写入 {rel}"
    except OSError as e:
        return f"写入失败: {e}"


def build_tools() -> list[ToolDef]:
    return [
        ToolDef("wiki_status", "查看知识库状态:页面计数、未编译 raw", {"type": "object", "properties": {}}, wiki_status),
        ToolDef("run_lint", "运行 lint 健康检查(断链/孤儿/矛盾/格式)", {"type": "object", "properties": {}}, run_lint),
        ToolDef("search_wiki", "在知识库中检索相关页面", {"type": "object", "properties": {"query": {"type": "string"}, "k": {"type": "integer"}}}, search_wiki),
        ToolDef("get_page", "读取一个 wiki 页面", {"type": "object", "properties": {"page": {"type": "string"}}}, get_page),
        ToolDef("read_file", "读取项目内文件", {"type": "object", "properties": {"path": {"type": "string"}}}, read_file),
        ToolDef("write_file", "写入 wiki/raw/examples 内的文件(编译写页面用)", {"type": "object", "properties": {"path": {"type": "string"}, "content": {"type": "string"}}}, write_file),
        ToolDef("run_script", "执行白名单脚本", {"type": "object", "properties": {"name": {"type": "string"}, "args": {"type": "string"}}}, run_script),
    ]
