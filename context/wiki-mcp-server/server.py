"""wiki-mcp-server:把编译好的 wiki/ 暴露成 MCP 工具。

骨架版本:
- 检索是 grep 级(标题+正文关键词加权),够演示;完整 Agentic RAG 见 TODO。
- 运行于项目根目录(server 用 ../wiki 定位知识库)。

TODO:
- search_wiki 升级为 embedding 检索 + rerank
- synthesize_for 落地(compile 循环 + synthesis 模板,filed back)
"""

from __future__ import annotations

import json
import re
import sys
from pathlib import Path

from mcp.server.fastmcp import FastMCP

PROJECT_ROOT = Path(__file__).resolve().parent.parent
WIKI_ROOT = PROJECT_ROOT / "wiki"
SUBDIRS = ("sources", "concepts", "entities", "synthesis")

mcp = FastMCP("wiki-context")


def _all_pages() -> list[Path]:
    return [p for d in SUBDIRS for p in (WIKI_ROOT / d).glob("*.md")]


def _slug(p: Path) -> str:
    return p.stem


def _read(p: Path) -> str:
    return p.read_text(encoding="utf-8", errors="ignore")


@mcp.tool()
def search_wiki(query: str, k: int = 5) -> str:
    """在知识库中检索与 query 最相关的页面,返回标题+摘要+命中理由。

    骨架用关键词命中打分:标题命中权重 3,正文命中权重 1。
    TODO:替换为 embedding + rerank。
    """
    terms = [t for t in re.split(r"\s+", query.lower()) if t]
    results = []
    for p in _all_pages():
        text = _read(p)
        title = _slug(p)
        score = 0
        for t in terms:
            if t in title.lower():
                score += 3
            score += text.lower().count(t)
        if score:
            # 取一句话结论/定义节首行作为摘要
            summary = ""
            m = re.search(r"## (?:一句话结论|定义|概述|问题)\s*\n([^\n]+)", text)
            if m:
                summary = m.group(1).strip()
            results.append((score, title, summary))
    results.sort(key=lambda x: -x[0])
    top = results[:k]
    if not top:
        return "知识库中未找到与查询匹配的页面。"
    out = []
    for score, title, summary in top:
        out.append(f"- [[{title}]] (命中 {score}): {summary or '(无摘要)'}")
    return "\n".join(out)


@mcp.tool()
def get_page(page: str) -> str:
    """读取知识库中的一个页面(传 slug 或文件名,如 `Agent核心架构` 或 `Agent核心架构.md`)。"""
    slug = page.removesuffix(".md")
    for d in SUBDIRS:
        p = WIKI_ROOT / d / f"{slug}.md"
        if p.exists():
            return f"## {slug} (来源 {d}/)\n\n{_read(p)}"
    return f"页面 [[{slug}]] 不存在。"


@mcp.tool()
def get_related(page: str) -> str:
    """解析一个页面的 [[wikilinks]],返回它关联了哪些知识节点及其在库中的去向。"""
    slug = page.removesuffix(".md")
    src = None
    for d in SUBDIRS:
        p = WIKI_ROOT / d / f"{slug}.md"
        if p.exists():
            src = p
            break
    if src is None:
        return f"页面 [[{slug}]] 不存在。"
    links = re.findall(r"\[\[([^\]|]+)(?:\|[^\]]+)?\]\]", _read(src))
    resolved, broken = [], []
    for l in links:
        hit = any((WIKI_ROOT / d / f"{l}.md").exists() for d in SUBDIRS)
        (resolved if hit else broken).append(l)
    return f"[[{slug}]] 出站链接 {len(links)} 条:\n- 可解析: {resolved}\n- 断链: {broken or '无'}"


@mcp.tool()
def list_recent(n: int = 5) -> str:
    """列出最近编译的源页(按文件名中 YYYY-MM-DD 前缀或修改时间)。"""
    pages = _all_pages()
    pages.sort(key=lambda p: p.stat().st_mtime, reverse=True)
    return "\n".join(f"- [[{_slug(p)}]]" for p in pages[:n])


if __name__ == "__main__":
    mcp.run(transport="stdio")
