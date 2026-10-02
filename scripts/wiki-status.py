#!/usr/bin/env python3
"""统一输出 Wiki 状态，供 SessionStart、QA 和 coordinator 使用。

口径：
- 页面计数来自 wiki/ 实际文件；health/ 排除，index.md 单独排除
- raw 覆盖只统计没有对应 source 页引用的 Markdown/TXT
- 已有同 stem Markdown 的 PDF 是转换中间产物，不重复计为未编译源
"""
from __future__ import annotations

import argparse
import json
import re
from pathlib import Path

ARXIV_RE = re.compile(r"\d{4}\.\d{4,5}")

BASE = Path(__file__).resolve().parent.parent
WIKI = BASE / "wiki"
RAW = BASE / "raw"


def source_raw_paths() -> set[str]:
    paths: set[str] = set()
    for page in (WIKI / "sources").glob("*.md"):
        text = page.read_text(encoding="utf-8", errors="ignore")
        lines = text.splitlines()
        in_fm = False
        for i, line in enumerate(lines):
            if line.strip() == "---":
                in_fm = not in_fm
                continue
            if not in_fm:
                continue
            # Match YAML array entry: "- raw/path" possibly with continuation
            m = re.match(r"\s*-\s+(raw/.+)", line)
            if m:
                path = m.group(1)
                # Collect multi-line continuation (indented lines without "- " prefix)
                j = i + 1
                while j < len(lines):
                    nxt = lines[j]
                    if nxt.strip() and nxt[0] in " \t" and not re.match(r"\s*-\s", nxt):
                        # YAML multi-line folds to space; ensure sep matches original
                        sep = "" if path.endswith(" ") else " "
                        path += sep + nxt.lstrip()
                        j += 1
                    else:
                        break
                paths.add(path)
    return paths


def raw_candidates() -> list[str]:
    files = []
    for p in RAW.rglob("*"):
        if p.is_dir() or "assets" in p.parts or "resume-arsenal" in p.parts:
            continue
        if p.suffix.lower() not in {".md", ".txt", ".pdf"}:
            continue
        files.append(str(p.relative_to(BASE)))
    return sorted(files)


def uncompiled() -> list[str]:
    compiled = source_raw_paths()
    candidates = raw_candidates()
    compiled_stems = {Path(x).stem for x in compiled}
    compiled_ids = {m.group(0) for x in compiled for m in [ARXIV_RE.search(x)] if m}
    result = []
    for path in candidates:
        if path in compiled:
            continue
        # PDF 与已经入库的同名 Markdown 是同一来源的转换中间产物。
        if Path(path).suffix.lower() == ".pdf":
            stem = Path(path).stem
            pdf_id = ARXIV_RE.search(stem)
            if (stem in compiled_stems or
                any(stem in c or c in stem for c in compiled_stems) or
                (pdf_id and pdf_id.group(0) in compiled_ids)):
                continue
        result.append(path)
    return result


def status() -> dict:
    counts = {"sources": 0, "concepts": 0, "entities": 0, "synthesis": 0, "notes": 0}
    for p in WIKI.rglob("*.md"):
        if "health" in p.parts or p == WIKI / "index.md":
            continue
        rel = p.relative_to(WIKI)
        if rel.parts[0] == "sources": counts["sources"] += 1
        elif rel.parts[0] == "concepts": counts["concepts"] += 1
        elif rel.parts[0] == "entities": counts["entities"] += 1
        elif rel.parts[0] == "synthesis": counts["synthesis"] += 1
        elif rel.parts[0] == "notes": counts["notes"] += 1
    counts["total_pages"] = sum(counts.values())
    counts["uncompiled"] = len(uncompiled())
    counts["uncompiled_files"] = uncompiled()
    return counts


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--json", action="store_true")
    args = parser.parse_args()
    data = status()
    if args.json:
        print(json.dumps(data, ensure_ascii=False, indent=2))
    else:
        print(f"Wiki Status: {data['total_pages']} pages | "
              f"{data['sources']} sources / {data['concepts']} concepts / "
              f"{data['entities']} entities / {data['synthesis']} synthesis / "
              f"{data['notes']} notes | uncompiled raw: {data['uncompiled']}")
        for path in data["uncompiled_files"]:
            print(f"- {path}")
